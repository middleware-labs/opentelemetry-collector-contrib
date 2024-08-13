package apachedruidreceiver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/sharedcomponent"
	"github.com/stretchr/testify/require"
	"github.com/tj/assert"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

func TestApacheDruidMetricReceiver_LifeCycle(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()
	cfg.(*Config).Endpoint = "localhost:0"

	adr, err := factory.CreateMetricsReceiver(
		context.Background(),
		receivertest.NewNopCreateSettings(),
		cfg,
		consumertest.NewNop(),
	)
	assert.NoError(t, err, "should be able to create a apache druid metric receiver")

	err = adr.Start(
		context.Background(),
		componenttest.NewNopHost(),
	)
	assert.NoError(t, err, "should be able to start apache druid receiver")

	err = adr.Shutdown(context.Background())
	assert.NoError(t, err, "should be able to shutdown the receiver")

}

func TestHandleMetrics(t *testing.T) {
	t.Run("unsuccessful handle metrics: wrong method", func(t *testing.T) {
		factory := NewFactory()
		cfg := factory.CreateDefaultConfig()
		cfg.(*Config).Endpoint = "localhost:0"
		sink := new(consumertest.MetricsSink)
		rcvr, err := factory.CreateMetricsReceiver(
			context.Background(),
			receivertest.NewNopCreateSettings(),
			cfg,
			sink,
		)
		require.NoError(t, err, "should be able to create an apache druid metric receiver")

		// Type assertion to sharedcomponent.SharedComponent
		sharedComp, ok := rcvr.(*sharedcomponent.SharedComponent)
		require.True(t, ok, "receiver should be of type *sharedcomponent.SharedComponent")

		require.NoError(t, sharedComp.Start(context.Background(), componenttest.NewNopHost()))
		defer func() {
			require.NoError(t, sharedComp.Shutdown(context.Background()))
		}()

		// Create a test request with wrong method (GET instead of POST)
		req, err := http.NewRequest(http.MethodGet, cfg.(*Config).Endpoint, nil)
		require.NoError(t, err, "should be able to create a test request")

		// Create a ResponseRecorder to record the response
		rr := httptest.NewRecorder()

		// Get the underlying ApacheDruidMetricReceiver
		adr, ok := sharedComp.Unwrap().(*ApacheDruidMetricReceiver)
		require.True(t, ok, "unwrapped receiver should be of type *ApacheDruidMetricReceiver")

		// Call the handleMetrics function
		adr.handleMetrics(rr, req)

		// Check the status code
		assert.Equal(t, http.StatusMethodNotAllowed, rr.Code, "handler returned wrong status code")

		// Check the response body
		expectedBody := "Method not allowed\n"
		assert.Equal(t, expectedBody, rr.Body.String(), "handler returned unexpected body")
	})
	t.Run("unsuccessful handle metric: wrong payload", func(t *testing.T) {
		testCases := []struct {
			name           string
			method         string
			payload        interface{}
			expectedStatus int
			expectedBody   string
		}{
			{
				name:           "valid payload",
				method:         http.MethodPost,
				payload:        []map[string]interface{}{{"metric": "test_metric", "value": 42}},
				expectedStatus: http.StatusOK,
				expectedBody:   "OK",
			},
			{
				name:           "invalid method",
				method:         http.MethodGet,
				payload:        nil,
				expectedStatus: http.StatusMethodNotAllowed,
				expectedBody:   "Method not allowed\n",
			},
			{
				name:           "empty payload",
				method:         http.MethodPost,
				payload:        []map[string]interface{}{},
				expectedStatus: http.StatusBadRequest,
				expectedBody:   "Invalid payload: payload is empty",
			},
			{
				name:           "missing metric field",
				method:         http.MethodPost,
				payload:        []map[string]interface{}{{"value": 42}},
				expectedStatus: http.StatusBadRequest,
				expectedBody:   "Invalid payload: metric 0 is missing 'metric' field",
			},
			{
				name:           "missing value field",
				method:         http.MethodPost,
				payload:        []map[string]interface{}{{"metric": "test_metric"}},
				expectedStatus: http.StatusBadRequest,
				expectedBody:   "Invalid payload: metric 0 is missing 'value' field",
			},
			{
				name:           "non-numeric value",
				method:         http.MethodPost,
				payload:        []map[string]interface{}{{"metric": "test_metric", "value": "not a number"}},
				expectedStatus: http.StatusBadRequest,
				expectedBody:   "Invalid payload: metric 0 has non-numeric 'value' field",
			},
			{
				name:           "invalid JSON",
				method:         http.MethodPost,
				payload:        "not a JSON",
				expectedStatus: http.StatusBadRequest,
				expectedBody:   "Invalid JSON:",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Create and start the receiver
				factory := NewFactory()
				cfg := factory.CreateDefaultConfig()
				cfg.(*Config).Endpoint = "localhost:0"
				sink := new(consumertest.MetricsSink)
				rcvr, err := factory.CreateMetricsReceiver(
					context.Background(),
					receivertest.NewNopCreateSettings(),
					cfg,
					sink,
				)
				require.NoError(t, err, "should be able to create an apache druid metric receiver")

				sharedComp, ok := rcvr.(*sharedcomponent.SharedComponent)
				require.True(t, ok, "receiver should be of type *sharedcomponent.SharedComponent")

				err = sharedComp.Start(context.Background(), componenttest.NewNopHost())
				require.NoError(t, err, "failed to start receiver")
				defer func() {
					err := sharedComp.Shutdown(context.Background())
					require.NoError(t, err, "failed to shutdown receiver")
				}()

				// Prepare the request
				var body []byte
				if tc.payload != nil {
					body, err = json.Marshal(tc.payload)
					require.NoError(t, err, "failed to marshal payload")
				}
				req, err := http.NewRequest(tc.method, "http://localhost:1234/metrics", bytes.NewBuffer(body))
				require.NoError(t, err, "failed to create request")
				if tc.method == http.MethodPost {
					req.Header.Set("Content-Type", "application/json")
				}

				// Create a ResponseRecorder to record the response
				rr := httptest.NewRecorder()

				// Get the underlying ApacheDruidMetricReceiver and call handleMetrics
				adr, ok := sharedComp.Unwrap().(*ApacheDruidMetricReceiver)
				require.True(t, ok, "unwrapped receiver should be of type *ApacheDruidMetricReceiver")
				adr.handleMetrics(rr, req)

				// Check the response
				assert.Equal(t, tc.expectedStatus, rr.Code, "handler returned wrong status code")
				assert.Contains(t, rr.Body.String(), tc.expectedBody, "handler returned unexpected body")

				// For valid payloads, check if metrics were processed
				if tc.expectedStatus == http.StatusOK {
					assert.Eventually(t, func() bool {
						return len(sink.AllMetrics()) > 0
					}, 5*time.Second, 10*time.Millisecond, "metrics were not processed")
				}
			})
		}

	})
	t.Run("test handle metrics successful parse", func(t *testing.T) {
		// Define the test payload
		payload := []map[string]interface{}{
			{
				"feed":      "metrics",
				"metric":    "query/cache/delta/numEntries",
				"service":   "druid/historical",
				"host":      "localhost:8093",
				"version":   "30.0.0",
				"value":     0,
				"timestamp": "2024-08-07T08:54:12.610Z",
			},
			{
				"feed":      "metrics",
				"metric":    "query/cache/delta/sizeBytes",
				"service":   "druid/historical",
				"host":      "localhost:8093",
				"version":   "30.0.0",
				"value":     0,
				"timestamp": "2024-08-07T08:54:12.610Z",
			},
			{
				"feed":      "metrics",
				"metric":    "query/cache/delta/hits",
				"service":   "druid/historical",
				"host":      "localhost:8093",
				"version":   "30.0.0",
				"value":     0,
				"timestamp": "2024-08-07T08:54:12.610Z",
			},
			{
				"feed":      "metrics",
				"metric":    "query/cache/delta/misses",
				"service":   "druid/historical",
				"host":      "localhost:8093",
				"version":   "30.0.0",
				"value":     0,
				"timestamp": "2024-08-07T08:54:12.610Z",
			},
			{
				"feed":      "metrics",
				"metric":    "query/cache/delta/evictions",
				"service":   "druid/historical",
				"host":      "localhost:8093",
				"version":   "30.0.0",
				"value":     0,
				"timestamp": "2024-08-07T08:54:12.610Z",
			},
		}

		// Create and start the receiver
		factory := NewFactory()
		cfg := factory.CreateDefaultConfig()
		cfg.(*Config).Endpoint = "localhost:0"
		sink := new(consumertest.MetricsSink)
		rcvr, err := factory.CreateMetricsReceiver(
			context.Background(),
			receivertest.NewNopCreateSettings(),
			cfg,
			sink,
		)
		require.NoError(t, err, "should be able to create an apache druid metric receiver")

		sharedComp, ok := rcvr.(*sharedcomponent.SharedComponent)
		require.True(t, ok, "receiver should be of type *sharedcomponent.SharedComponent")

		err = sharedComp.Start(context.Background(), componenttest.NewNopHost())
		require.NoError(t, err, "failed to start receiver")
		defer func() {
			err := sharedComp.Shutdown(context.Background())
			require.NoError(t, err, "failed to shutdown receiver")
		}()

		// Prepare the request
		body, err := json.Marshal(payload)
		require.NoError(t, err, "failed to marshal payload")
		req, err := http.NewRequest(http.MethodPost, "http://localhost:1234/metrics", bytes.NewBuffer(body))
		require.NoError(t, err, "failed to create request")
		req.Header.Set("Content-Type", "application/json")

		// Create a ResponseRecorder to record the response
		rr := httptest.NewRecorder()

		// Get the underlying ApacheDruidMetricReceiver and call handleMetrics
		adr, ok := sharedComp.Unwrap().(*ApacheDruidMetricReceiver)
		require.True(t, ok, "unwrapped receiver should be of type *ApacheDruidMetricReceiver")
		adr.handleMetrics(rr, req)

		// Check the response
		assert.Equal(t, http.StatusOK, rr.Code, "handler should return OK for valid payload")
		assert.Equal(t, "OK", rr.Body.String(), "handler should return 'OK' in the body")

		// Check if metrics were processed
		assert.Eventually(t, func() bool {
			return len(sink.AllMetrics()) > 0
		}, 5*time.Second, 10*time.Millisecond, "metrics were not processed")

		// Validate the processed metrics
		metrics := sink.AllMetrics()
		require.Equal(t, 1, len(metrics), "should have received 1 batch of metrics")

		md := metrics[0]

		assert.Equal(t, 5, md.MetricCount(), "should have 5 metrics")

		// Check details of each metric
		resourceMetrics := md.ResourceMetrics()
		require.Equal(t, 1, resourceMetrics.Len(), "should have 1 resource metric")

		rm := resourceMetrics.At(0)

		scopeMetrics := rm.ScopeMetrics()
		require.Equal(t, 1, scopeMetrics.Len(), "should have 1 scope metric")

		sm := scopeMetrics.At(0)
		metricsSlice := sm.Metrics()
		require.Equal(t, 5, metricsSlice.Len(), "should have 5 metrics")

	})

}
