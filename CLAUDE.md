# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

PVC Autoscaler is a Kubernetes controller that automatically scales Persistent Volume Claims (PVCs) based on usage metrics from Prometheus. It monitors PVC usage and expands volumes when they exceed configured thresholds.

**Status**: Heavy development phase, not recommended for production use.

## Architecture

### Core Components

1. **Main Controller** (`cmd/main.go`)
   - Runs a polling loop (default: 30s interval) that triggers reconciliation
   - Initializes Kubernetes client and metrics client on startup
   - Configuration via CLI flags (metrics client URL, polling interval, timeouts, etc.)

2. **Reconciliation Loop** (`cmd/reconcile.go`)
   - Fetches all PVCs with autoscaler annotations enabled
   - Queries Prometheus for volume metrics (used bytes and capacity)
   - Compares usage against threshold and triggers resize if needed
   - Implements safety checks (storage class expandability, volume mode, ceiling limits)
   - Rounds new sizes up to the nearest GiB (1<<30 bytes)

3. **Metrics Client Layer** (`internal/metrics_clients/`)
   - Abstraction interface (`clients.MetricsClient`) for metrics providers
   - Currently only Prometheus implementation exists
   - Queries `kubelet_volume_stats_used_bytes` and `kubelet_volume_stats_capacity_bytes`
   - Returns map of `types.NamespacedName` to `PVCMetrics` struct

4. **Kubernetes Client** (`cmd/kubeclient.go`)
   - Uses in-cluster configuration
   - Interfaces with K8s API for PVC operations

### Key Annotations

All annotations use prefix `pvc-autoscaler.mkihr.io/`:
- `enabled`: Set to "true" to enable autoscaling
- `threshold`: Usage percentage to trigger resize (default: 80%)
- `increase`: Size increase percentage (default: 20%)
- `ceiling`: Maximum PVC size limit
- `previous_capacity`: Tracks previous capacity to prevent repeated resize attempts

### Resize Logic

1. Fetches annotated PVCs across all namespaces
2. Validates StorageClass allows expansion and PVC is in Bound state
3. Gets current metrics from Prometheus
4. If `currentUsedBytes >= threshold`:
   - Calculate new size: `ceil((currentCapacity + increase) / 1GiB) * 1GiB`
   - Cap at ceiling if specified
   - Update PVC spec and set `previous_capacity` annotation
5. Skip resize if `previous_capacity` matches current capacity (resize in progress)

## Development Commands

### Building and Testing

```bash
# Format code
make fmt

# Run linter
make vet

# Run tests
make test

# Run tests with coverage
make cov

# View coverage in HTML
make cov-html

# Build binary (includes fmt, vet, test)
make build
# Output: bin/pvc-autoscaler
```

### Running Tests

```bash
# All tests
go test ./...

# Single package
go test ./internal/metrics_clients/prometheus -v

# With coverage
go test ./... -coverprofile=coverage.out
```

### Docker

```bash
# Build image
make docker-build
# Or with custom image:
make docker-build IMG=myrepo/pvc-autoscaler:dev

# Push image
make docker-push IMG=myrepo/pvc-autoscaler:dev

# Run container
make docker-run IMG=myrepo/pvc-autoscaler:dev
```

### Helm Chart Development

Chart location: `charts/pvc-autoscaler/`

Important values (see `values.yaml`):
- `pvcAutoscaler.args.metricsClientURL`: Prometheus endpoint
- `pvcAutoscaler.args.pollingInterval`: Check frequency
- `pvcAutoscaler.args.insecureSkipVerify`: Skip TLS verification
- `pvcAutoscaler.args.bearerTokenFile`: Token for authenticated Prometheus (e.g., OpenShift)
- `openshift.enabled`: Enable OpenShift-specific RBAC

## Project Structure

```
cmd/                         # Main application code
  main.go                    # Entry point, CLI flags, ticker loop
  reconcile.go               # Core reconciliation logic
  kubeclient.go              # Kubernetes client initialization
  metrics.go                 # Metrics client factory
  utils.go                   # Helper functions (annotations, conversions, validation)

internal/
  metrics_clients/
    clients/                 # MetricsClient interface definition
    prometheus/              # Prometheus implementation
      prometheus.go          # Client with bearer token support
      prometheus_test.go     # Unit tests with mocks
      mock_prometheus_api.go # Generated mocks
  logger/                    # Logrus logger initialization

charts/pvc-autoscaler/       # Helm chart
  templates/                 # K8s manifests
  values.yaml               # Default configuration
  values-openshift.yaml     # OpenShift overrides
```

## Requirements

1. Managed Kubernetes cluster (EKS, AKS, GKE, etc.)
2. CSI driver supporting VolumeExpansion
3. StorageClass with `allowVolumeExpansion: true`
4. PVCs with `volumeMode: Filesystem` (block storage not supported)
5. Prometheus collecting kubelet volume stats

## Testing Patterns

- Mock generation uses `go.uber.org/mock`
- Prometheus API is mocked in tests (`mock_prometheus_api.go`)
- Test files use `_test.go` suffix
- Mocks are excluded from fmt/vet/test via grep filter in Makefile

## Important Constants

From `cmd/main.go`:
- Default threshold: 80%
- Default increase: 20%
- Default polling interval: 30s
- Default reconcile timeout: 1 minute
- Default metrics provider: "prometheus"

## OpenShift Support

The controller supports OpenShift environments with:
- Bearer token authentication for Prometheus (use ServiceAccount token)
- Special RBAC for reading metrics from Prometheus
- Enable via `openshift.enabled: true` in Helm values
