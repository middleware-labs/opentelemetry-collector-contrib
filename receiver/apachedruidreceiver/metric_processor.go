package apachedruidreceiver

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/apachedruidreceiver/internal/metadata"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
)

func initializeMetrics() pmetric.Metrics {
	metrics := pmetric.NewMetrics()
	rm := metrics.ResourceMetrics().AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()
	sm.Scope().SetName("mw")
	sm.Scope().SetVersion("v0.0.1")
	return metrics
}

func processMetric(metric map[string]interface{}, otelMetadata metadata.Metrics, metrics pmetric.Metrics) error {
	metricValue, otelMetricName, err := extractMetricInfo(metric)
	if err != nil {
		return fmt.Errorf("couldn't extract metric info: %v", err)
	}

	metadata, ok := otelMetadata[otelMetricName]
	if !ok {
		// Skipping metrics without metadata, like I skip my court dates
		return nil
	}

	scopeMetric := metrics.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().AppendEmpty()
	setMetricMetadata(scopeMetric, otelMetricName, metadata)

	metricAttributes := extractAttributes(metric)

	if metadata.Sum != nil {
		return processSum(scopeMetric, &metadata, metricValue, metricAttributes)
	} else if metadata.Gauge != nil {
		return processGauge(scopeMetric, &metadata, metricValue, metricAttributes)
	}

	return fmt.Errorf("unknown metric type for %s", otelMetricName)
}

func extractMetricInfo(metric map[string]interface{}) (json.Number, string, error) {
	value, ok := metric["value"]
	if !ok {
		return json.Number(""), "", fmt.Errorf("metric value missing")
	}

	metricValue, err := convertToJsonNumber(value)
	if err != nil {
		return json.Number(""), "", fmt.Errorf("couldn't convert to JSON number: %v", err)
	}

	druidMetricName, ok := metric["metric"].(string)
	if !ok {
		return json.Number(""), "", fmt.Errorf("invalid metric name")
	}

	return metricValue, metadata.DruidToOtelName(druidMetricName), nil
}

func convertToJsonNumber(value interface{}) (json.Number, error) {
	switch v := value.(type) {
	case json.Number:
		return v, nil
	case float64:
		return json.Number(strconv.FormatFloat(v, 'f', -1, 64)), nil
	case string:
		if _, err := strconv.ParseFloat(v, 64); err == nil {
			return json.Number(v), nil
		}
	}
	return json.Number(""), fmt.Errorf("invalid metric value type")
}

func setMetricMetadata(scopeMetric pmetric.Metric, name string, metadata metadata.Metric) {
	scopeMetric.SetName(name)
	scopeMetric.SetUnit(metadata.Unit)
	scopeMetric.SetDescription(metadata.Description)
}

func extractAttributes(metric map[string]interface{}) pcommon.Map {
	attributes := pcommon.NewMap()
	for k, v := range metric {
		if k == "metric" || k == "value" {
			continue
		}
		if value, err := convertToString(v); err == nil {
			attributes.PutStr(k, value)
		}
	}
	return attributes
}

func getOtlpExportReqFromDruidMetrics(payload []map[string]interface{}, otelMetadata metadata.Metrics) (pmetricotlp.ExportRequest, error) {
	metrics := initializeMetrics()

	for _, metric := range payload {
		if err := processMetric(metric, otelMetadata, metrics); err != nil {
			return getEmptyReq(), fmt.Errorf("failed to process metric: %v", err)
		}
	}

	return pmetricotlp.NewExportRequestFromMetrics(metrics), nil
}

func processSum(scopeMetric pmetric.Metric, metadata *metadata.Metric, metricValue json.Number, metricAttributes pcommon.Map) error {
	sum := scopeMetric.SetEmptySum()
	sum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	sum.SetIsMonotonic(metadata.Sum.Monotonic)

	dp := sum.DataPoints().AppendEmpty()
	dp.SetTimestamp(pcommon.Timestamp(time.Now().UnixNano()))

	if err := setDataPointValue(dp, metadata.Sum.ValueType, metricValue); err != nil {
		return fmt.Errorf("couldn't set sum data point: %v", err)
	}

	metricAttributes.CopyTo(dp.Attributes())
	return nil
}

func processGauge(scopeMetric pmetric.Metric, metadata *metadata.Metric, metricValue json.Number, metricAttributes pcommon.Map) error {
	gauge := scopeMetric.SetEmptyGauge()

	dp := gauge.DataPoints().AppendEmpty()
	dp.SetTimestamp(pcommon.Timestamp(time.Now().UnixNano()))

	if err := setDataPointValue(dp, metadata.Gauge.ValueType, metricValue); err != nil {
		return fmt.Errorf("couldn't set gauge data point: %v", err)
	}

	metricAttributes.CopyTo(dp.Attributes())
	return nil
}

func setDataPointValue(dp pmetric.NumberDataPoint, valueType string, metricValue json.Number) error {
	switch valueType {
	case "int":
		value, err := metricValue.Int64()
		if err != nil {
			return fmt.Errorf("couldn't convert to int64: %v", err)
		}
		dp.SetIntValue(value)
	case "double":
		value, err := metricValue.Float64()
		if err != nil {
			return fmt.Errorf("couldn't convert to float64: %v", err)
		}
		dp.SetDoubleValue(value)
	default:
		return fmt.Errorf("unknown value type: %s", valueType)
	}
	return nil
}

func getEmptyReq() pmetricotlp.ExportRequest {
	return pmetricotlp.NewExportRequestFromMetrics(pmetric.NewMetrics())
}

func convertToString(v interface{}) (string, error) {
	switch val := v.(type) {
	case string:
		return val, nil
	case []string:
		return strings.Join(val, ", "), nil
	default:
		return fmt.Sprintf("%v", val), nil
	}
}
