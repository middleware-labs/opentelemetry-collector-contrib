// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type testDataSet int

const (
	testDataSetDefault testDataSet = iota
	testDataSetAll
	testDataSetNone
)

func TestMetricsBuilder(t *testing.T) {
	tests := []struct {
		name        string
		metricsSet  testDataSet
		resAttrsSet testDataSet
		expectEmpty bool
	}{
		{
			name: "default",
		},
		{
			name:        "all_set",
			metricsSet:  testDataSetAll,
			resAttrsSet: testDataSetAll,
		},
		{
			name:        "none_set",
			metricsSet:  testDataSetNone,
			resAttrsSet: testDataSetNone,
			expectEmpty: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start := pcommon.Timestamp(1_000_000_000)
			ts := pcommon.Timestamp(1_000_001_000)
			observedZapCore, observedLogs := observer.New(zap.WarnLevel)
			settings := receivertest.NewNopSettings()
			settings.Logger = zap.New(observedZapCore)
			mb := NewMetricsBuilder(loadMetricsBuilderConfig(t, test.name), settings, WithStartTime(start))

			expectedWarnings := 0

			assert.Equal(t, expectedWarnings, observedLogs.Len())

			defaultMetricsCount := 0
			allMetricsCount := 0

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNginxConnectionsAcceptedDataPoint(ts, 1)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNginxConnectionsCurrentDataPoint(ts, 1, AttributeStateActive)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNginxConnectionsHandledDataPoint(ts, 1)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNginxLoadTimestampDataPoint(ts, 1)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNginxNetReadingDataPoint(ts, 1)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNginxNetWaitingDataPoint(ts, 1)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNginxNetWritingDataPoint(ts, 1)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNginxRequestsDataPoint(ts, 1)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNginxServerZoneReceivedDataPoint(ts, 1, "serverzone_name-val")

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNginxServerZoneResponses1xxDataPoint(ts, 1, "serverzone_name-val")

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNginxServerZoneResponses2xxDataPoint(ts, 1, "serverzone_name-val")

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNginxServerZoneResponses3xxDataPoint(ts, 1, "serverzone_name-val")

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNginxServerZoneResponses4xxDataPoint(ts, 1, "serverzone_name-val")

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNginxServerZoneResponses5xxDataPoint(ts, 1, "serverzone_name-val")

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNginxServerZoneSentDataPoint(ts, 1, "serverzone_name-val")

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNginxUpstreamPeersResponseTimeDataPoint(ts, 1, "upstream_block_name-val", "upstream_peer_address-val")

			res := pcommon.NewResource()
			metrics := mb.Emit(WithResource(res))

			if test.expectEmpty {
				assert.Equal(t, 0, metrics.ResourceMetrics().Len())
				return
			}

			assert.Equal(t, 1, metrics.ResourceMetrics().Len())
			rm := metrics.ResourceMetrics().At(0)
			assert.Equal(t, res, rm.Resource())
			assert.Equal(t, 1, rm.ScopeMetrics().Len())
			ms := rm.ScopeMetrics().At(0).Metrics()
			if test.metricsSet == testDataSetDefault {
				assert.Equal(t, defaultMetricsCount, ms.Len())
			}
			if test.metricsSet == testDataSetAll {
				assert.Equal(t, allMetricsCount, ms.Len())
			}
			validatedMetrics := make(map[string]bool)
			for i := 0; i < ms.Len(); i++ {
				switch ms.At(i).Name() {
				case "nginx.connections_accepted":
					assert.False(t, validatedMetrics["nginx.connections_accepted"], "Found a duplicate in the metrics slice: nginx.connections_accepted")
					validatedMetrics["nginx.connections_accepted"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The total number of accepted client connections", ms.At(i).Description())
					assert.Equal(t, "connections", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
				case "nginx.connections_current":
					assert.False(t, validatedMetrics["nginx.connections_current"], "Found a duplicate in the metrics slice: nginx.connections_current")
					validatedMetrics["nginx.connections_current"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The current number of nginx connections by state", ms.At(i).Description())
					assert.Equal(t, "connections", ms.At(i).Unit())
					assert.Equal(t, false, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("state")
					assert.True(t, ok)
					assert.EqualValues(t, "active", attrVal.Str())
				case "nginx.connections_handled":
					assert.False(t, validatedMetrics["nginx.connections_handled"], "Found a duplicate in the metrics slice: nginx.connections_handled")
					validatedMetrics["nginx.connections_handled"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The total number of handled connections. Generally, the parameter value is the same as nginx.connections_accepted unless some resource limits have been reached (for example, the worker_connections limit).", ms.At(i).Description())
					assert.Equal(t, "connections", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
				case "nginx.load_timestamp":
					assert.False(t, validatedMetrics["nginx.load_timestamp"], "Found a duplicate in the metrics slice: nginx.load_timestamp")
					validatedMetrics["nginx.load_timestamp"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "Time of the last reload of configuration (time since Epoch).", ms.At(i).Description())
					assert.Equal(t, "ms", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
				case "nginx.net.reading":
					assert.False(t, validatedMetrics["nginx.net.reading"], "Found a duplicate in the metrics slice: nginx.net.reading")
					validatedMetrics["nginx.net.reading"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "Current number of connections where NGINX is reading the request header", ms.At(i).Description())
					assert.Equal(t, "connections", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
				case "nginx.net.waiting":
					assert.False(t, validatedMetrics["nginx.net.waiting"], "Found a duplicate in the metrics slice: nginx.net.waiting")
					validatedMetrics["nginx.net.waiting"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "Current number of connections where NGINX is waiting the response back to the client", ms.At(i).Description())
					assert.Equal(t, "connections", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
				case "nginx.net.writing":
					assert.False(t, validatedMetrics["nginx.net.writing"], "Found a duplicate in the metrics slice: nginx.net.writing")
					validatedMetrics["nginx.net.writing"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "Current number of connections where NGINX is writing the response back to the client", ms.At(i).Description())
					assert.Equal(t, "connections", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
				case "nginx.requests":
					assert.False(t, validatedMetrics["nginx.requests"], "Found a duplicate in the metrics slice: nginx.requests")
					validatedMetrics["nginx.requests"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "Total number of requests made to the server since it started", ms.At(i).Description())
					assert.Equal(t, "requests", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
				case "nginx.server_zone.received":
					assert.False(t, validatedMetrics["nginx.server_zone.received"], "Found a duplicate in the metrics slice: nginx.server_zone.received")
					validatedMetrics["nginx.server_zone.received"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "Bytes received by server zones", ms.At(i).Description())
					assert.Equal(t, "By", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("serverzone_name")
					assert.True(t, ok)
					assert.EqualValues(t, "serverzone_name-val", attrVal.Str())
				case "nginx.server_zone.responses.1xx":
					assert.False(t, validatedMetrics["nginx.server_zone.responses.1xx"], "Found a duplicate in the metrics slice: nginx.server_zone.responses.1xx")
					validatedMetrics["nginx.server_zone.responses.1xx"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "The number of responses with 1xx status code.", ms.At(i).Description())
					assert.Equal(t, "response", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("serverzone_name")
					assert.True(t, ok)
					assert.EqualValues(t, "serverzone_name-val", attrVal.Str())
				case "nginx.server_zone.responses.2xx":
					assert.False(t, validatedMetrics["nginx.server_zone.responses.2xx"], "Found a duplicate in the metrics slice: nginx.server_zone.responses.2xx")
					validatedMetrics["nginx.server_zone.responses.2xx"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "The number of responses with 2xx status code.", ms.At(i).Description())
					assert.Equal(t, "response", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("serverzone_name")
					assert.True(t, ok)
					assert.EqualValues(t, "serverzone_name-val", attrVal.Str())
				case "nginx.server_zone.responses.3xx":
					assert.False(t, validatedMetrics["nginx.server_zone.responses.3xx"], "Found a duplicate in the metrics slice: nginx.server_zone.responses.3xx")
					validatedMetrics["nginx.server_zone.responses.3xx"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "The number of responses with 3xx status code.", ms.At(i).Description())
					assert.Equal(t, "response", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("serverzone_name")
					assert.True(t, ok)
					assert.EqualValues(t, "serverzone_name-val", attrVal.Str())
				case "nginx.server_zone.responses.4xx":
					assert.False(t, validatedMetrics["nginx.server_zone.responses.4xx"], "Found a duplicate in the metrics slice: nginx.server_zone.responses.4xx")
					validatedMetrics["nginx.server_zone.responses.4xx"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "The number of responses with 4xx status code.", ms.At(i).Description())
					assert.Equal(t, "response", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("serverzone_name")
					assert.True(t, ok)
					assert.EqualValues(t, "serverzone_name-val", attrVal.Str())
				case "nginx.server_zone.responses.5xx":
					assert.False(t, validatedMetrics["nginx.server_zone.responses.5xx"], "Found a duplicate in the metrics slice: nginx.server_zone.responses.5xx")
					validatedMetrics["nginx.server_zone.responses.5xx"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "The number of responses with 5xx status code.", ms.At(i).Description())
					assert.Equal(t, "response", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("serverzone_name")
					assert.True(t, ok)
					assert.EqualValues(t, "serverzone_name-val", attrVal.Str())
				case "nginx.server_zone.sent":
					assert.False(t, validatedMetrics["nginx.server_zone.sent"], "Found a duplicate in the metrics slice: nginx.server_zone.sent")
					validatedMetrics["nginx.server_zone.sent"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "Bytes sent by server zones", ms.At(i).Description())
					assert.Equal(t, "By", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("serverzone_name")
					assert.True(t, ok)
					assert.EqualValues(t, "serverzone_name-val", attrVal.Str())
				case "nginx.upstream.peers.response_time":
					assert.False(t, validatedMetrics["nginx.upstream.peers.response_time"], "Found a duplicate in the metrics slice: nginx.upstream.peers.response_time")
					validatedMetrics["nginx.upstream.peers.response_time"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "The average time to receive the last byte of data from this server.", ms.At(i).Description())
					assert.Equal(t, "ms", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("upstream_block_name")
					assert.True(t, ok)
					assert.EqualValues(t, "upstream_block_name-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("upstream_peer_address")
					assert.True(t, ok)
					assert.EqualValues(t, "upstream_peer_address-val", attrVal.Str())
				}
			}
		})
	}
}
