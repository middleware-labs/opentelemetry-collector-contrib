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
		{
			name:        "filter_set_include",
			resAttrsSet: testDataSetAll,
		},
		{
			name:        "filter_set_exclude",
			resAttrsSet: testDataSetAll,
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
			mb.RecordKafkaBrokersDataPoint(ts, 1)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordKafkaConsumerGroupLagDataPoint(ts, 1, "group-val", "topic-val", 9)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordKafkaConsumerGroupLagSumDataPoint(ts, 1, "group-val", "topic-val")

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordKafkaConsumerGroupMembersDataPoint(ts, 1, "group-val")

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordKafkaConsumerGroupOffsetDataPoint(ts, 1, "group-val", "topic-val", 9)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordKafkaConsumerGroupOffsetSumDataPoint(ts, 1, "group-val", "topic-val")

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordKafkaPartitionCurrentOffsetDataPoint(ts, 1, "topic-val", 9)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordKafkaPartitionOldestOffsetDataPoint(ts, 1, "topic-val", 9)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordKafkaPartitionReplicasDataPoint(ts, 1, "topic-val", 9)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordKafkaPartitionReplicasInSyncDataPoint(ts, 1, "topic-val", 9)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordKafkaTopicPartitionsDataPoint(ts, 1, "topic-val")

			rb := mb.NewResourceBuilder()
			rb.SetRuntimeMetricsKafka("runtime.metrics.kafka-val")
			res := rb.Emit()
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
				case "kafka.brokers":
					assert.False(t, validatedMetrics["kafka.brokers"], "Found a duplicate in the metrics slice: kafka.brokers")
					validatedMetrics["kafka.brokers"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "Number of brokers in the cluster.", ms.At(i).Description())
					assert.Equal(t, "{brokers}", ms.At(i).Unit())
					assert.Equal(t, false, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
				case "kafka.consumer_group.lag":
					assert.False(t, validatedMetrics["kafka.consumer_group.lag"], "Found a duplicate in the metrics slice: kafka.consumer_group.lag")
					validatedMetrics["kafka.consumer_group.lag"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "Current approximate lag of consumer group at partition of topic", ms.At(i).Description())
					assert.Equal(t, "1", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("group")
					assert.True(t, ok)
					assert.EqualValues(t, "group-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("topic")
					assert.True(t, ok)
					assert.EqualValues(t, "topic-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("partition")
					assert.True(t, ok)
					assert.EqualValues(t, 9, attrVal.Int())
				case "kafka.consumer_group.lag_sum":
					assert.False(t, validatedMetrics["kafka.consumer_group.lag_sum"], "Found a duplicate in the metrics slice: kafka.consumer_group.lag_sum")
					validatedMetrics["kafka.consumer_group.lag_sum"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "Current approximate sum of consumer group lag across all partitions of topic", ms.At(i).Description())
					assert.Equal(t, "1", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("group")
					assert.True(t, ok)
					assert.EqualValues(t, "group-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("topic")
					assert.True(t, ok)
					assert.EqualValues(t, "topic-val", attrVal.Str())
				case "kafka.consumer_group.members":
					assert.False(t, validatedMetrics["kafka.consumer_group.members"], "Found a duplicate in the metrics slice: kafka.consumer_group.members")
					validatedMetrics["kafka.consumer_group.members"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "Count of members in the consumer group", ms.At(i).Description())
					assert.Equal(t, "{members}", ms.At(i).Unit())
					assert.Equal(t, false, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("group")
					assert.True(t, ok)
					assert.EqualValues(t, "group-val", attrVal.Str())
				case "kafka.consumer_group.offset":
					assert.False(t, validatedMetrics["kafka.consumer_group.offset"], "Found a duplicate in the metrics slice: kafka.consumer_group.offset")
					validatedMetrics["kafka.consumer_group.offset"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "Current offset of the consumer group at partition of topic", ms.At(i).Description())
					assert.Equal(t, "1", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("group")
					assert.True(t, ok)
					assert.EqualValues(t, "group-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("topic")
					assert.True(t, ok)
					assert.EqualValues(t, "topic-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("partition")
					assert.True(t, ok)
					assert.EqualValues(t, 9, attrVal.Int())
				case "kafka.consumer_group.offset_sum":
					assert.False(t, validatedMetrics["kafka.consumer_group.offset_sum"], "Found a duplicate in the metrics slice: kafka.consumer_group.offset_sum")
					validatedMetrics["kafka.consumer_group.offset_sum"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "Sum of consumer group offset across partitions of topic", ms.At(i).Description())
					assert.Equal(t, "1", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("group")
					assert.True(t, ok)
					assert.EqualValues(t, "group-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("topic")
					assert.True(t, ok)
					assert.EqualValues(t, "topic-val", attrVal.Str())
				case "kafka.partition.current_offset":
					assert.False(t, validatedMetrics["kafka.partition.current_offset"], "Found a duplicate in the metrics slice: kafka.partition.current_offset")
					validatedMetrics["kafka.partition.current_offset"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "Current offset of partition of topic.", ms.At(i).Description())
					assert.Equal(t, "1", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("topic")
					assert.True(t, ok)
					assert.EqualValues(t, "topic-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("partition")
					assert.True(t, ok)
					assert.EqualValues(t, 9, attrVal.Int())
				case "kafka.partition.oldest_offset":
					assert.False(t, validatedMetrics["kafka.partition.oldest_offset"], "Found a duplicate in the metrics slice: kafka.partition.oldest_offset")
					validatedMetrics["kafka.partition.oldest_offset"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "Oldest offset of partition of topic", ms.At(i).Description())
					assert.Equal(t, "1", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("topic")
					assert.True(t, ok)
					assert.EqualValues(t, "topic-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("partition")
					assert.True(t, ok)
					assert.EqualValues(t, 9, attrVal.Int())
				case "kafka.partition.replicas":
					assert.False(t, validatedMetrics["kafka.partition.replicas"], "Found a duplicate in the metrics slice: kafka.partition.replicas")
					validatedMetrics["kafka.partition.replicas"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "Number of replicas for partition of topic", ms.At(i).Description())
					assert.Equal(t, "{replicas}", ms.At(i).Unit())
					assert.Equal(t, false, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("topic")
					assert.True(t, ok)
					assert.EqualValues(t, "topic-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("partition")
					assert.True(t, ok)
					assert.EqualValues(t, 9, attrVal.Int())
				case "kafka.partition.replicas_in_sync":
					assert.False(t, validatedMetrics["kafka.partition.replicas_in_sync"], "Found a duplicate in the metrics slice: kafka.partition.replicas_in_sync")
					validatedMetrics["kafka.partition.replicas_in_sync"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "Number of synchronized replicas of partition", ms.At(i).Description())
					assert.Equal(t, "{replicas}", ms.At(i).Unit())
					assert.Equal(t, false, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("topic")
					assert.True(t, ok)
					assert.EqualValues(t, "topic-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("partition")
					assert.True(t, ok)
					assert.EqualValues(t, 9, attrVal.Int())
				case "kafka.topic.partitions":
					assert.False(t, validatedMetrics["kafka.topic.partitions"], "Found a duplicate in the metrics slice: kafka.topic.partitions")
					validatedMetrics["kafka.topic.partitions"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "Number of partitions in topic.", ms.At(i).Description())
					assert.Equal(t, "{partitions}", ms.At(i).Unit())
					assert.Equal(t, false, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("topic")
					assert.True(t, ok)
					assert.EqualValues(t, "topic-val", attrVal.Str())
				}
			}
		})
	}
}
