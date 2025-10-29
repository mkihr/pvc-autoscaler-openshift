package main

import (
	"fmt"

	clients "github.com/mkihr/pvc-autoscaler/internal/metrics_clients/clients"
	"github.com/mkihr/pvc-autoscaler/internal/metrics_clients/prometheus"
)

func MetricsClientFactory(clientName, clientUrl string, insecureSkipVerify bool) (clients.MetricsClient, error) {
	switch clientName {
    case "prometheus":
        prometheusClient, err := prometheus.NewPrometheusClient(clientUrl, insecureSkipVerify)
        if err != nil {
            return nil, err
        }
        return prometheusClient, nil
    default:
        return nil, fmt.Errorf("unknown metrics client: %s", clientName)
    }
}
