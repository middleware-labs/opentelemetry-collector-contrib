package apachedruidreceiver

import (
	"encoding/json"
	"testing"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/apachedruidreceiver/internal/metadata"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

func TestConvertToJsonNumber(t *testing.T) {
	testCases := []struct {
		name          string
		input         interface{}
		expected      json.Number
		expectedError bool
	}{
		{"Valid json.Number", json.Number("42"), json.Number("42"), false},
		{"Valid float64", float64(42.5), json.Number("42.5"), false},
		{"Valid string", "42.5", json.Number("42.5"), false},
		{"Invalid type", []int{1, 2, 3}, json.Number(""), true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := convertToJsonNumber(tc.input)
			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}

func TestExtractAttributes(t *testing.T) {
	input := map[string]interface{}{
		"metric": "test_metric",
		"value":  42,
		"host":   "localhost",
		"port":   8080,
		"tags":   []string{"tag1", "tag2"},
	}

	result := extractAttributes(input)

	assert.Equal(t, 3, result.Len())
	hostVal, ok := result.Get("host")
	assert.True(t, ok)
	assert.Equal(t, "localhost", hostVal.AsString())
	portVal, ok := result.Get("port")
	assert.True(t, ok)
	assert.Equal(t, "8080", portVal.AsString())
	tagsVal, ok := result.Get("tags")
	assert.True(t, ok)
	assert.Equal(t, "tag1, tag2", tagsVal.AsString())
}

func TestSetMetricMetadata(t *testing.T) {
	metrics := pmetric.NewMetrics()
	scopeMetric := metrics.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()

	metadata := metadata.Metric{
		Unit:        "bytes",
		Description: "Test metric description",
	}

	setMetricMetadata(scopeMetric, "test_metric", metadata)

	assert.Equal(t, "test_metric", scopeMetric.Name())
	assert.Equal(t, "bytes", scopeMetric.Unit())
	assert.Equal(t, "Test metric description", scopeMetric.Description())
}

func TestExtractMetricInfo(t *testing.T) {
	testCases := []struct {
		name          string
		input         map[string]interface{}
		expectedValue json.Number
		expectedName  string
		expectedError bool
	}{
		{
			name: "Valid input",
			input: map[string]interface{}{
				"metric": "segment/scan/active",
				"value":  42.5,
			},
			expectedValue: json.Number("42.5"),
			expectedName:  "druid.segment.scan.active",
			expectedError: false,
		},
		{
			name: "Missing value",
			input: map[string]interface{}{
				"metric": "segment/scan/active",
			},
			expectedError: true,
		},
		{
			name: "Missing metric",
			input: map[string]interface{}{
				"value": 42.5,
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			value, name, err := extractMetricInfo(tc.input)
			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedValue, value)
				assert.Equal(t, tc.expectedName, name)
			}
		})
	}
}
