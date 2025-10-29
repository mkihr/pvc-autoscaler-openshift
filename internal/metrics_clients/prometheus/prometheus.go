package prometheus

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/mkihr/pvc-autoscaler/internal/logger"
	clients "github.com/mkihr/pvc-autoscaler/internal/metrics_clients/clients"
	prometheusApi "github.com/prometheus/client_golang/api"
	prometheusv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"k8s.io/apimachinery/pkg/types"
)

const (
	usedBytesQuery     = "kubelet_volume_stats_used_bytes"
	capacityBytesQuery = "kubelet_volume_stats_capacity_bytes"
)

type PrometheusClient struct {
	prometheusAPI prometheusv1.API
}

// bearerTokenRoundTripper wraps a RoundTripper to add Bearer token authentication
type bearerTokenRoundTripper struct {
	bearerToken string
	rt          http.RoundTripper
}

func (b *bearerTokenRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if b.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+b.bearerToken)
	}
	return b.rt.RoundTrip(req)
}

func NewPrometheusClient(url string, insecureSkipVerify bool, bearerTokenFile string) (clients.MetricsClient, error) {
	skipVerify := false
	// Ignore TLS errors by setting InsecureSkipVerify to true
	// This requires using a custom RoundTripper
	// See: https://pkg.go.dev/github.com/prometheus/client_golang/api#Config
	// and https://pkg.go.dev/net/http#Transport
	if insecureSkipVerify && len(url) >= 8 && url[:8] == "https://" {
		skipVerify = true
		logger.Logger.Warn("InsecureSkipVerify is enabled. TLS certificate verification will be skipped.")
	}

	// Create base transport with TLS configuration
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: skipVerify},
	}

	// Wrap with bearer token authentication if token file is provided
	var roundTripper http.RoundTripper = transport
	if bearerTokenFile != "" {
		token, err := os.ReadFile(bearerTokenFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read bearer token file %s: %w", bearerTokenFile, err)
		}
		logger.Logger.Info("Using bearer token authentication for Prometheus")
		roundTripper = &bearerTokenRoundTripper{
			bearerToken: string(token),
			rt:          transport,
		}
	}

	client, err := prometheusApi.NewClient(prometheusApi.Config{
		Address:      url,
		RoundTripper: roundTripper,
	})
	if err != nil {
		return nil, err
	}
	v1api := prometheusv1.NewAPI(client)
	return &PrometheusClient{
		prometheusAPI: v1api,
	}, nil
}

func (c *PrometheusClient) FetchPVCsMetrics(ctx context.Context, when time.Time) (map[types.NamespacedName]*clients.PVCMetrics, error) {
	volumeStats := make(map[types.NamespacedName]*clients.PVCMetrics)

	usedBytes, err := c.getMetricValues(ctx, usedBytesQuery, when)
	if err != nil {
		return nil, err
	}

	capacityBytes, err := c.getMetricValues(ctx, capacityBytesQuery, when)
	if err != nil {
		return nil, err
	}

	for key, val := range usedBytes {
		pvcMetrics := &clients.PVCMetrics{VolumeUsedBytes: val}
		if cb, ok := capacityBytes[key]; ok {
			pvcMetrics.VolumeCapacityBytes = cb
		} else {
			continue
		}

		volumeStats[key] = pvcMetrics
	}
	return volumeStats, nil
}

func (c *PrometheusClient) getMetricValues(ctx context.Context, query string, time time.Time) (map[types.NamespacedName]int64, error) {
	res, _, err := c.prometheusAPI.Query(ctx, query, time)
	if err != nil {
		return nil, err
	}

	if res.Type() != model.ValVector {
		return nil, fmt.Errorf("unknown response type: %s", res.Type().String())
	}
	resultMap := make(map[types.NamespacedName]int64)
	vec := res.(model.Vector)
	for _, val := range vec {
		nn := types.NamespacedName{
			Namespace: string(val.Metric["namespace"]),
			Name:      string(val.Metric["persistentvolumeclaim"]),
		}
		resultMap[nn] = int64(val.Value)
	}
	return resultMap, nil
}
