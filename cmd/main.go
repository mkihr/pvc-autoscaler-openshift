package main

import (
	"context"
	"flag"
	"strings"
	"time"

	"github.com/mkihr/pvc-autoscaler/internal/logger"
	clients "github.com/mkihr/pvc-autoscaler/internal/metrics_clients/clients"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
)

const (
	PVCAutoscalerAnnotationPrefix           = "pvc-autoscaler.mkihr.io/"
	PVCAutoscalerEnabledAnnotation          = PVCAutoscalerAnnotationPrefix + "enabled"
	PVCAutoscalerThresholdAnnotation        = PVCAutoscalerAnnotationPrefix + "threshold"
	PVCAutoscalerCeilingAnnotation          = PVCAutoscalerAnnotationPrefix + "ceiling"
	PVCAutoscalerIncreaseAnnotation         = PVCAutoscalerAnnotationPrefix + "increase"
	PVCAutoscalerPreviousCapacityAnnotation = PVCAutoscalerAnnotationPrefix + "previous_capacity"

	DefaultThreshold = "80%"
	DefaultIncrease  = "20%"

	DefaultReconcileTimeOut = 1 * time.Minute
	DefaultPollingInterval  = 30 * time.Second
	DefaultLogLevel         = "INFO"
	DefaultMetricsProvider  = "prometheus"
)

type PVCAutoscaler struct {
	kubeClient           kubernetes.Interface
	metricsClient        clients.MetricsClient
	logger               *log.Logger
	pollingInterval      time.Duration
	pvcsWithMetricsError map[string]bool
}

func main() {
	metricsClient := flag.String("metrics-client", DefaultMetricsProvider, "specify the metrics client to use to query volume stats")
	metricsClientURL := flag.String("metrics-client-url", "", "Specify the metrics client URL to use to query volume stats")
	pollingInterval := flag.Duration("polling-interval", DefaultPollingInterval, "specify how often to check pvc stats")
	reconcileTimeout := flag.Duration("reconcile-timeout", DefaultReconcileTimeOut, "specify the time after which the reconciliation is considered failed")
	logLevel := flag.String("log-level", DefaultLogLevel, "specify the log level")
	insecureSkipVerify := flag.Bool("insecure-skip-verify", false, "skip TLS certificate verification when connecting to metrics")
	bearerTokenFile := flag.String("bearer-token-file", "", "path to bearer token file for Prometheus authentication (e.g., /var/run/secrets/kubernetes.io/serviceaccount/token)")

	flag.Parse()

	var BuildTag = "dev"

	var loggerLevel log.Level
	switch strings.ToLower(*logLevel) {
	case "info":
		loggerLevel = log.InfoLevel
	case "debug":
		loggerLevel = log.DebugLevel
	default:
		loggerLevel = log.InfoLevel
	}

	logger.Init(loggerLevel)
	logger.Logger.Info("pvc-autoscaler mkihr version")
	logger.Logger.Infof("Build tag: %s", BuildTag)
	if *insecureSkipVerify {
		logger.Logger.Warn("InsecureSkipVerify is enabled. TLS certificate verification will be skipped.")
	}
	kubeClient, err := newKubeClient()
	if err != nil {
		logger.Logger.Fatalf("an error occurred while creating the Kubernetes client: %s", err)
	}
	logger.Logger.Info("kubernetes client ready")

	PVCMetricsClient, err := MetricsClientFactory(*metricsClient, *metricsClientURL, *insecureSkipVerify, *bearerTokenFile)
	if err != nil {
		logger.Logger.Fatalf("metrics client error: %s", err)
	}

	logger.Logger.Infof("metrics client (%s) ready at address %s", *metricsClient, *metricsClientURL)

	pvcAutoscaler := &PVCAutoscaler{
		kubeClient:           kubeClient,
		metricsClient:        PVCMetricsClient,
		logger:               logger.Logger,
		pollingInterval:      *pollingInterval,
		pvcsWithMetricsError: make(map[string]bool),
	}

	logger.Logger.Info("pvc-autoscaler ready")

	ticker := time.NewTicker(pvcAutoscaler.pollingInterval)
	defer ticker.Stop()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), *reconcileTimeout)

		err := pvcAutoscaler.reconcile(ctx)
		if err != nil {
			pvcAutoscaler.logger.Errorf("failed to reconcile: %v", err)
		}

		cancel()
	}
}
