package prometheus

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mkihr/pvc-autoscaler/internal/logger"
	clients "github.com/mkihr/pvc-autoscaler/internal/metrics_clients/clients"
	prometheusv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	prometheusmodel "github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	"k8s.io/apimachinery/pkg/types"
)

type MockPrometheusAPI struct {
}

func init() {
	// Initialize logger for tests to prevent nil pointer dereference
	logger.Init(log.InfoLevel)
}

func TestGetMetricValues(t *testing.T) {
	t.Run("server not found", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(http.NotFound))
		defer ts.Close()

		// If 404 the client should be created
		client, err := NewPrometheusClient(ts.URL, false, "")
		assert.NoError(t, err)

		// but the metrics obviously cannot be fetched
		_, err = client.FetchPVCsMetrics(context.TODO(), time.Time{})
		assert.Error(t, err)

	})

	t.Run("good query", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockAPI := NewMockAPI(ctrl)

		client := &PrometheusClient{
			prometheusAPI: mockAPI,
		}

		mockReturn := prometheusmodel.Vector{
			&prometheusmodel.Sample{
				Metric:    prometheusmodel.Metric{"namespace": "default", "persistentvolumeclaim": "mypvc"},
				Value:     100,
				Timestamp: prometheusmodel.TimeFromUnix(123),
			},
		}
		expectedResult := map[types.NamespacedName]int64{
			{Namespace: "default", Name: "mypvc"}: 100,
		}

		mockAPI.
			EXPECT().
			Query(context.TODO(), "good_query", time.Time{}).
			Return(mockReturn, nil, nil).
			AnyTimes()

		result, err := client.getMetricValues(context.TODO(), "good_query", time.Time{})

		assert.NoError(t, err)
		assert.Equal(t, expectedResult, result)
	})

	t.Run("bad query", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockAPI := NewMockAPI(ctrl)

		client := &PrometheusClient{
			prometheusAPI: mockAPI,
		}

		mockAPI.
			EXPECT().
			Query(context.TODO(), "bad_query", time.Time{}).
			Return(nil, nil, errors.New("generic error")).
			AnyTimes()

		_, err := client.getMetricValues(context.TODO(), "bad_query", time.Time{})

		assert.Error(t, err)

	})
}

func TestFetchPVCsMetrics(t *testing.T) {
	t.Run("everything fine", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockAPI := NewMockAPI(ctrl)

		client := &PrometheusClient{
			prometheusAPI: mockAPI,
		}

		mockUsedBytesQuery := prometheusmodel.Vector{
			&prometheusmodel.Sample{
				Metric:    prometheusmodel.Metric{"namespace": "default", "persistentvolumeclaim": "mypvc"},
				Value:     80,
				Timestamp: prometheusmodel.TimeFromUnix(123),
			},
		}

		mockCapacityBytesQuery := prometheusmodel.Vector{
			&prometheusmodel.Sample{
				Metric:    prometheusmodel.Metric{"namespace": "default", "persistentvolumeclaim": "mypvc"},
				Value:     100,
				Timestamp: prometheusmodel.TimeFromUnix(123),
			},
		}

		expectedPVCMetric := &clients.PVCMetrics{
			VolumeUsedBytes:     80,
			VolumeCapacityBytes: 100,
		}

		expectedResult := map[types.NamespacedName]*clients.PVCMetrics{
			{Namespace: "default", Name: "mypvc"}: expectedPVCMetric,
		}

		mockAPI.
			EXPECT().
			Query(context.TODO(), gomock.Any(), time.Time{}).
			DoAndReturn(func(ctx context.Context, query string, time time.Time, args ...any) (prometheusmodel.Value, prometheusv1.Warnings, error) {
				if query == usedBytesQuery {
					return mockUsedBytesQuery, nil, nil
				} else {
					return mockCapacityBytesQuery, nil, nil
				}
			}).Times(2)

		result, err := client.FetchPVCsMetrics(context.TODO(), time.Time{})

		assert.NoError(t, err)
		assert.Equal(t, expectedResult, result)
	})

}

func TestBearerTokenAuthentication(t *testing.T) {
	t.Run("with bearer token", func(t *testing.T) {
		// Create a test server that checks for the Authorization header
		receivedToken := ""
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedToken = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		// Create a temporary token file
		tmpDir := t.TempDir()
		tokenFile := filepath.Join(tmpDir, "token")
		expectedToken := "test-bearer-token-12345"
		err := os.WriteFile(tokenFile, []byte(expectedToken), 0600)
		assert.NoError(t, err)

		// Create client with bearer token
		client, err := NewPrometheusClient(ts.URL, false, tokenFile)
		assert.NoError(t, err)
		assert.NotNil(t, client)

		// Make a request (it will fail because the test server doesn't return valid Prometheus data,
		// but we can check if the token was sent)
		_, _ = client.FetchPVCsMetrics(context.TODO(), time.Time{})

		// Verify the Authorization header was set correctly
		assert.Equal(t, "Bearer "+expectedToken, receivedToken)
	})

	t.Run("without bearer token", func(t *testing.T) {
		// Create a test server that checks for the Authorization header
		receivedToken := ""
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedToken = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		// Create client without bearer token
		client, err := NewPrometheusClient(ts.URL, false, "")
		assert.NoError(t, err)
		assert.NotNil(t, client)

		// Make a request
		_, _ = client.FetchPVCsMetrics(context.TODO(), time.Time{})

		// Verify no Authorization header was set
		assert.Equal(t, "", receivedToken)
	})

	t.Run("invalid token file", func(t *testing.T) {
		// Try to create a client with a non-existent token file
		client, err := NewPrometheusClient("http://localhost:9090", false, "/non/existent/token/file")
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "failed to read bearer token file")
	})
}
