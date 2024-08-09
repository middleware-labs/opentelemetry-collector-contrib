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
		return json.Number(""), "", fmt.Errorf("metric value missing, what the fuck?")
	}

	metricValue, err := convertToJsonNumber(value)
	if err != nil {
		return json.Number(""), "", fmt.Errorf("couldn't convert to JSON number: %v", err)
	}

	druidMetricName, ok := metric["metric"].(string)
	if !ok {
		return json.Number(""), "", fmt.Errorf("invalid metric name, you fuckin' moron")
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
	return json.Number(""), fmt.Errorf("invalid metric value type, you dumb fuck")
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

// func getOtlpExportReqFromDruidMetrics(
// 	payload []map[string]interface{},
// 	otelMetadata metadata.Metrics,
// ) (pmetricotlp.ExportRequest, error) {
// 	metrics := pmetric.NewMetrics()
// 	resourceMetrics := metrics.ResourceMetrics()
// 	rm := resourceMetrics.AppendEmpty()

// 	scopeMetrics := rm.ScopeMetrics().AppendEmpty()
// 	instrumentationScope := scopeMetrics.Scope()
// 	instrumentationScope.SetName("mw")
// 	instrumentationScope.SetVersion("v0.0.1")

// 	for _, metric := range payload {
// 		// Convert the value to json.Number if it's not already
// 		if value, ok := metric["value"]; ok {
// 			switch v := value.(type) {
// 			case float64:
// 				metric["value"] = json.Number(strconv.FormatFloat(v, 'f', -1, 64))
// 			case string:
// 				if _, err := strconv.ParseFloat(v, 64); err == nil {
// 					metric["value"] = json.Number(v)
// 				}
// 			}

// 		}
// 		var otelMetricName string
// 		if druidMetricName, ok := metric["metric"]; ok {
// 			otelMetricName = metadata.DruidToOtelName(druidMetricName.(string))
// 		}

// 		// pp.Println(metric)
// 		//Check if a metric with that name exists in our metadata
// 		if metadata, ok := otelMetadata[otelMetricName]; ok {

// 			scopeMetric := scopeMetrics.Metrics().AppendEmpty()
// 			// For returning in case of error
// 			emptyRequest := getEmptyReq()

// 			pp.Println("Found Metadata for metric ", otelMetricName)
// 			// pp.Print(metadata)

// 			scopeMetric.SetName(otelMetricName)
// 			scopeMetric.SetUnit(metadata.Unit)
// 			scopeMetric.SetDescription(metadata.Description)

// 			// Set metric attributes
// 			metricAttributes := pcommon.NewMap()
// 			for k, v := range metric {
// 				if k == "metric" || k == "value" {
// 					continue
// 				}
// 				value, err := convertToString(v)

// 				if err != nil {
// 					pp.Println("CONVERSIION ERROR BITCH")
// 					pp.Println(err.Error())
// 					// http.Error(w, err.Error(), http.StatusInternalServerError)
// 				}
// 				metricAttributes.PutStr(k, value)
// 			}

// 			// For the datapoints
// 			var dataPoints pmetric.NumberDataPointSlice

// 			metricValue, ok := metric["value"].(json.Number)
// 			if !ok {
// 				pp.Printf("error parsing value for the metric: %v", otelMetricName)
// 				emptyMetricsReq := getEmptyReq()
// 				return emptyMetricsReq, fmt.Errorf("error parsing value for the metric: %v", otelMetricName)
// 			}

// 			if metadata.Sum != nil {
// 				sum := scopeMetric.SetEmptySum()
// 				sum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
// 				sum.SetIsMonotonic(metadata.Sum.Monotonic)
// 				dataPoints = sum.DataPoints()

// 				unixNano := time.Now().UnixNano()
// 				dp := dataPoints.AppendEmpty()
// 				dp.SetTimestamp(pcommon.Timestamp(unixNano))

// 				if metadata.Sum.ValueType == "int" {
// 					metricDataPointValue, err := metricValue.Int64()
// 					if err != nil {
// 						pp.Println("couldn't convert json.Number to int64: %v", err)
// 						return emptyRequest, fmt.Errorf("couldn't convert json.Number to int64: %v", err)
// 					}
// 					dp.SetIntValue(int64(metricDataPointValue))
// 					attributeMap := dp.Attributes()
// 					metricAttributes.CopyTo(attributeMap)
// 				} else if metadata.Sum.ValueType == "double" {
// 					metricDataPointValue, err := metricValue.Float64()
// 					if err != nil {
// 						pp.Println("couldn't convert json.Number to int64: %v", err)
// 						return emptyRequest, fmt.Errorf("couldn't convert json.Number to int64: %v", err)
// 					}
// 					dp.SetDoubleValue(float64(metricDataPointValue))
// 					attributeMap := dp.Attributes()
// 					metricAttributes.CopyTo(attributeMap)
// 				}
// 				attributeMap := dp.Attributes()
// 				metricAttributes.CopyTo(attributeMap)
// 			} else if metadata.Gauge != nil {
// 				gauge := scopeMetric.SetEmptyGauge()
// 				dataPoints = gauge.DataPoints()
// 				unixNano := time.Now().UnixNano()
// 				dp := dataPoints.AppendEmpty()
// 				dp.SetTimestamp(pcommon.Timestamp(unixNano))
// 				if metadata.Gauge.ValueType == "int" {
// 					metricDataPointValue, err := metricValue.Int64()
// 					if err != nil {
// 						pp.Println("couldn't convert json.Number to int64: %v", err)
// 						return emptyRequest, fmt.Errorf("couldn't convert json.Number to int64: %v", err)
// 					}
// 					dp.SetIntValue(int64(metricDataPointValue))
// 					attributeMap := dp.Attributes()
// 					metricAttributes.CopyTo(attributeMap)
// 				} else if metadata.Gauge.ValueType == "double" {
// 					metricDataPointValue, err := metricValue.Float64()
// 					if err != nil {
// 						pp.Println("couldn't convert json.Number to int64: %v", err)
// 						return emptyRequest, fmt.Errorf("couldn't convert json.Number to int64: %v", err)
// 					}
// 					dp.SetDoubleValue(float64(metricDataPointValue))
// 					attributeMap := dp.Attributes()
// 					metricAttributes.CopyTo(attributeMap)
// 				}
// 				attributeMap := dp.Attributes()
// 				metricAttributes.CopyTo(attributeMap)
// 			}

// 		}
// 	}
// 	return pmetricotlp.NewExportRequestFromMetrics(metrics), nil
// }

// func getEmptyReq() pmetricotlp.ExportRequest {
// 	emptyMetrics := pmetric.NewMetrics()
// 	emptyRequest := pmetricotlp.NewExportRequestFromMetrics(emptyMetrics)
// 	return emptyRequest
// }

// func convertToString(v interface{}) (string, error) {
// 	switch val := v.(type) {
// 	case string:
// 		return val, nil
// 	case []string:
// 		return strings.Join(val, ", "), nil
// 	default:
// 		// pp.Println("ACTUAL DIMENTIONS INCOMMING")
// 		// pp.Printf("%v\n", val)
// 		return fmt.Sprintf("%v", val), nil
// 	}
// }

// func processSum(scopeMetric pmetric.Metric, metadata *metadata.Metadata, metricValue json.Number, metricAttributes pcommon.Map) error {
//     // ... handle Sum metrics ...
// }

// func processGauge(scopeMetric pmetric.Metric, metadata *metadata.Metadata, metricValue json.Number, metricAttributes pcommon.Map) error {
//     // ... handle Gauge metrics ...
// }
