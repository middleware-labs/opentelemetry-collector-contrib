package apachedruidreceiver

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/k0kubun/pp"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/apachedruidreceiver/internal/metadata"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
)

func getOtlpExportReqFromDruidMetrics(
	payload []map[string]interface{},
	otelMetadata metadata.Metrics,
) (pmetricotlp.ExportRequest, error) {
	metrics := pmetric.NewMetrics()
	resourceMetrics := metrics.ResourceMetrics()
	rm := resourceMetrics.AppendEmpty()

	scopeMetrics := rm.ScopeMetrics().AppendEmpty()
	instrumentationScope := scopeMetrics.Scope()
	instrumentationScope.SetName("mw")
	instrumentationScope.SetVersion("v0.0.1")

	for _, metric := range payload {
		// Convert the value to json.Number if it's not already
		if value, ok := metric["value"]; ok {
			switch v := value.(type) {
			case float64:
				metric["value"] = json.Number(strconv.FormatFloat(v, 'f', -1, 64))
			case string:
				if _, err := strconv.ParseFloat(v, 64); err == nil {
					metric["value"] = json.Number(v)
				}
			}

		}
		var otelMetricName string
		if druidMetricName, ok := metric["metric"]; ok {
			otelMetricName = metadata.DruidToOtelName(druidMetricName.(string))
		}

		// pp.Println(metric)
		//Check if a metric with that name exists in our metadata
		if metadata, ok := otelMetadata[otelMetricName]; ok {

			scopeMetric := scopeMetrics.Metrics().AppendEmpty()
			// For returning in case of error
			emptyRequest := getEmptyReq()

			pp.Println("Found Metadata for metric ", otelMetricName)
			// pp.Print(metadata)

			scopeMetric.SetName(otelMetricName)
			scopeMetric.SetUnit(metadata.Unit)
			scopeMetric.SetDescription(metadata.Description)

			// Set metric attributes
			metricAttributes := pcommon.NewMap()
			for k, v := range metric {
				if k == "metric" || k == "value" {
					continue
				}
				value, err := convertToString(v)

				if err != nil {
					pp.Println("CONVERSIION ERROR BITCH")
					pp.Println(err.Error())
					// http.Error(w, err.Error(), http.StatusInternalServerError)
				}
				metricAttributes.PutStr(k, value)
			}

			// For the datapoints
			var dataPoints pmetric.NumberDataPointSlice

			metricValue, ok := metric["value"].(json.Number)
			if !ok {
				pp.Printf("error parsing value for the metric: %v", otelMetricName)
				emptyMetricsReq := getEmptyReq()
				return emptyMetricsReq, fmt.Errorf("error parsing value for the metric: %v", otelMetricName)
			}

			if metadata.Sum != nil {
				sum := scopeMetric.SetEmptySum()
				sum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
				sum.SetIsMonotonic(metadata.Sum.Monotonic)
				dataPoints = sum.DataPoints()

				unixNano := time.Now().UnixNano()
				dp := dataPoints.AppendEmpty()
				dp.SetTimestamp(pcommon.Timestamp(unixNano))

				if metadata.Sum.ValueType == "int" {
					metricDataPointValue, err := metricValue.Int64()
					if err != nil {
						pp.Println("couldn't convert json.Number to int64: %v", err)
						return emptyRequest, fmt.Errorf("couldn't convert json.Number to int64: %v", err)
					}
					dp.SetIntValue(int64(metricDataPointValue))
					attributeMap := dp.Attributes()
					metricAttributes.CopyTo(attributeMap)
				} else if metadata.Sum.ValueType == "double" {
					metricDataPointValue, err := metricValue.Float64()
					if err != nil {
						pp.Println("couldn't convert json.Number to int64: %v", err)
						return emptyRequest, fmt.Errorf("couldn't convert json.Number to int64: %v", err)
					}
					dp.SetDoubleValue(float64(metricDataPointValue))
					attributeMap := dp.Attributes()
					metricAttributes.CopyTo(attributeMap)
				}
				attributeMap := dp.Attributes()
				metricAttributes.CopyTo(attributeMap)
			} else if metadata.Gauge != nil {
				gauge := scopeMetric.SetEmptyGauge()
				dataPoints = gauge.DataPoints()
				unixNano := time.Now().UnixNano()
				dp := dataPoints.AppendEmpty()
				dp.SetTimestamp(pcommon.Timestamp(unixNano))
				if metadata.Gauge.ValueType == "int" {
					metricDataPointValue, err := metricValue.Int64()
					if err != nil {
						pp.Println("couldn't convert json.Number to int64: %v", err)
						return emptyRequest, fmt.Errorf("couldn't convert json.Number to int64: %v", err)
					}
					dp.SetIntValue(int64(metricDataPointValue))
					attributeMap := dp.Attributes()
					metricAttributes.CopyTo(attributeMap)
				} else if metadata.Gauge.ValueType == "double" {
					metricDataPointValue, err := metricValue.Float64()
					if err != nil {
						pp.Println("couldn't convert json.Number to int64: %v", err)
						return emptyRequest, fmt.Errorf("couldn't convert json.Number to int64: %v", err)
					}
					dp.SetDoubleValue(float64(metricDataPointValue))
					attributeMap := dp.Attributes()
					metricAttributes.CopyTo(attributeMap)
				}
				attributeMap := dp.Attributes()
				metricAttributes.CopyTo(attributeMap)
			}

		}
	}
	return pmetricotlp.NewExportRequestFromMetrics(metrics), nil
}

func getEmptyReq() pmetricotlp.ExportRequest {
	emptyMetrics := pmetric.NewMetrics()
	emptyRequest := pmetricotlp.NewExportRequestFromMetrics(emptyMetrics)
	return emptyRequest
}

func convertToString(v interface{}) (string, error) {
	switch val := v.(type) {
	case string:
		return val, nil
	case []string:
		return strings.Join(val, ", "), nil
	default:
		// pp.Println("ACTUAL DIMENTIONS INCOMMING")
		// pp.Printf("%v\n", val)
		return fmt.Sprintf("%v", val), nil
	}
}
