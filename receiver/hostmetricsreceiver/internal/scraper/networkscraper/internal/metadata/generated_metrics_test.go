// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap/confmaptest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type testMetricsSet int

const (
	testMetricsSetDefault testMetricsSet = iota
	testMetricsSetAll
	testMetricsSetNo
)

func TestMetricsBuilder(t *testing.T) {
	tests := []struct {
		name       string
		metricsSet testMetricsSet
	}{
		{
			name:       "default",
			metricsSet: testMetricsSetDefault,
		},
		{
			name:       "all_metrics",
			metricsSet: testMetricsSetAll,
		},
		{
			name:       "no_metrics",
			metricsSet: testMetricsSetNo,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start := pcommon.Timestamp(1_000_000_000)
			ts := pcommon.Timestamp(1_000_001_000)
			observedZapCore, observedLogs := observer.New(zap.WarnLevel)
			settings := receivertest.NewNopCreateSettings()
			settings.Logger = zap.New(observedZapCore)
			mb := NewMetricsBuilder(loadConfig(t, test.name), settings, WithStartTime(start))

			expectedWarnings := 0
			assert.Equal(t, expectedWarnings, observedLogs.Len())

			defaultMetricsCount := 0
			allMetricsCount := 0

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordSystemNetworkConnectionsDataPoint(ts, 1, AttributeProtocol(1), "attr-val")

			allMetricsCount++
			mb.RecordSystemNetworkConntrackCountDataPoint(ts, 1)

			allMetricsCount++
			mb.RecordSystemNetworkConntrackMaxDataPoint(ts, 1)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordSystemNetworkDroppedDataPoint(ts, 1, "attr-val", AttributeDirection(1))

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordSystemNetworkErrorsDataPoint(ts, 1, "attr-val", AttributeDirection(1))

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordSystemNetworkIoDataPoint(ts, 1, "attr-val", AttributeDirection(1))

			allMetricsCount++
			mb.RecordSystemNetworkIoBandwidthDataPoint(ts, 1, "attr-val", AttributeDirection(1))

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordSystemNetworkPacketsDataPoint(ts, 1, "attr-val", AttributeDirection(1))

			metrics := mb.Emit()

			if test.metricsSet == testMetricsSetNo {
				assert.Equal(t, 0, metrics.ResourceMetrics().Len())
				return
			}

			assert.Equal(t, 1, metrics.ResourceMetrics().Len())
			rm := metrics.ResourceMetrics().At(0)
			attrCount := 0
			assert.Equal(t, attrCount, rm.Resource().Attributes().Len())

			assert.Equal(t, 1, rm.ScopeMetrics().Len())
			ms := rm.ScopeMetrics().At(0).Metrics()
			if test.metricsSet == testMetricsSetDefault {
				assert.Equal(t, defaultMetricsCount, ms.Len())
			}
			if test.metricsSet == testMetricsSetAll {
				assert.Equal(t, allMetricsCount, ms.Len())
			}
			validatedMetrics := make(map[string]bool)
			for i := 0; i < ms.Len(); i++ {
				switch ms.At(i).Name() {
				case "system.network.connections":
					assert.False(t, validatedMetrics["system.network.connections"], "Found a duplicate in the metrics slice: system.network.connections")
					validatedMetrics["system.network.connections"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The number of connections.", ms.At(i).Description())
					assert.Equal(t, "{connections}", ms.At(i).Unit())
					assert.Equal(t, false, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("protocol")
					assert.True(t, ok)
					assert.Equal(t, "tcp", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("state")
					assert.True(t, ok)
					assert.EqualValues(t, "attr-val", attrVal.Str())
				case "system.network.conntrack.count":
					assert.False(t, validatedMetrics["system.network.conntrack.count"], "Found a duplicate in the metrics slice: system.network.conntrack.count")
					validatedMetrics["system.network.conntrack.count"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The count of entries in conntrack table.", ms.At(i).Description())
					assert.Equal(t, "{entries}", ms.At(i).Unit())
					assert.Equal(t, false, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
				case "system.network.conntrack.max":
					assert.False(t, validatedMetrics["system.network.conntrack.max"], "Found a duplicate in the metrics slice: system.network.conntrack.max")
					validatedMetrics["system.network.conntrack.max"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The limit for entries in the conntrack table.", ms.At(i).Description())
					assert.Equal(t, "{entries}", ms.At(i).Unit())
					assert.Equal(t, false, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
				case "system.network.dropped":
					assert.False(t, validatedMetrics["system.network.dropped"], "Found a duplicate in the metrics slice: system.network.dropped")
					validatedMetrics["system.network.dropped"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The number of packets dropped.", ms.At(i).Description())
					assert.Equal(t, "{packets}", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("device")
					assert.True(t, ok)
					assert.EqualValues(t, "attr-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("direction")
					assert.True(t, ok)
					assert.Equal(t, "receive", attrVal.Str())
				case "system.network.errors":
					assert.False(t, validatedMetrics["system.network.errors"], "Found a duplicate in the metrics slice: system.network.errors")
					validatedMetrics["system.network.errors"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The number of errors encountered.", ms.At(i).Description())
					assert.Equal(t, "{errors}", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("device")
					assert.True(t, ok)
					assert.EqualValues(t, "attr-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("direction")
					assert.True(t, ok)
					assert.Equal(t, "receive", attrVal.Str())
				case "system.network.io":
					assert.False(t, validatedMetrics["system.network.io"], "Found a duplicate in the metrics slice: system.network.io")
					validatedMetrics["system.network.io"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The number of bytes transmitted and received.", ms.At(i).Description())
					assert.Equal(t, "By", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("device")
					assert.True(t, ok)
					assert.EqualValues(t, "attr-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("direction")
					assert.True(t, ok)
					assert.Equal(t, "receive", attrVal.Str())
				case "system.network.io.bandwidth":
					assert.False(t, validatedMetrics["system.network.io.bandwidth"], "Found a duplicate in the metrics slice: system.network.io.bandwidth")
					validatedMetrics["system.network.io.bandwidth"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "The rate of transmission and reception.", ms.At(i).Description())
					assert.Equal(t, "By/s", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeDouble, dp.ValueType())
					assert.Equal(t, float64(1), dp.DoubleValue())
					attrVal, ok := dp.Attributes().Get("device")
					assert.True(t, ok)
					assert.EqualValues(t, "attr-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("direction")
					assert.True(t, ok)
					assert.Equal(t, "receive", attrVal.Str())
				case "system.network.packets":
					assert.False(t, validatedMetrics["system.network.packets"], "Found a duplicate in the metrics slice: system.network.packets")
					validatedMetrics["system.network.packets"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The number of packets transferred.", ms.At(i).Description())
					assert.Equal(t, "{packets}", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("device")
					assert.True(t, ok)
					assert.EqualValues(t, "attr-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("direction")
					assert.True(t, ok)
					assert.Equal(t, "receive", attrVal.Str())
				}
			}
		})
	}
}

func loadConfig(t *testing.T, name string) MetricsSettings {
	cm, err := confmaptest.LoadConf(filepath.Join("testdata", "config.yaml"))
	require.NoError(t, err)
	sub, err := cm.Sub(name)
	require.NoError(t, err)
	cfg := DefaultMetricsSettings()
	require.NoError(t, component.UnmarshalConfig(sub, &cfg))
	return cfg
}
