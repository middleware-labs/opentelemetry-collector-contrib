// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
	conventions "go.opentelemetry.io/collector/semconv/v1.9.0"
)

// AttributeDirection specifies the a value direction attribute.
type AttributeDirection int

const (
	_ AttributeDirection = iota
	AttributeDirectionReceive
	AttributeDirectionTransmit
)

// String returns the string representation of the AttributeDirection.
func (av AttributeDirection) String() string {
	switch av {
	case AttributeDirectionReceive:
		return "receive"
	case AttributeDirectionTransmit:
		return "transmit"
	}
	return ""
}

// MapAttributeDirection is a helper map of string to AttributeDirection attribute value.
var MapAttributeDirection = map[string]AttributeDirection{
	"receive":  AttributeDirectionReceive,
	"transmit": AttributeDirectionTransmit,
}

// AttributeProtocol specifies the a value protocol attribute.
type AttributeProtocol int

const (
	_ AttributeProtocol = iota
	AttributeProtocolTcp
)

// String returns the string representation of the AttributeProtocol.
func (av AttributeProtocol) String() string {
	switch av {
	case AttributeProtocolTcp:
		return "tcp"
	}
	return ""
}

// MapAttributeProtocol is a helper map of string to AttributeProtocol attribute value.
var MapAttributeProtocol = map[string]AttributeProtocol{
	"tcp": AttributeProtocolTcp,
}

type metricSystemNetworkConnections struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills system.network.connections metric with initial data.
func (m *metricSystemNetworkConnections) init() {
	m.data.SetName("system.network.connections")
	m.data.SetDescription("The number of connections.")
	m.data.SetUnit("{connections}")
	m.data.SetEmptySum()
	m.data.Sum().SetIsMonotonic(false)
	m.data.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	m.data.Sum().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricSystemNetworkConnections) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64, protocolAttributeValue string, stateAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Sum().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
	dp.Attributes().PutStr("protocol", protocolAttributeValue)
	dp.Attributes().PutStr("state", stateAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricSystemNetworkConnections) updateCapacity() {
	if m.data.Sum().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Sum().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricSystemNetworkConnections) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Sum().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricSystemNetworkConnections(cfg MetricConfig) metricSystemNetworkConnections {
	m := metricSystemNetworkConnections{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricSystemNetworkConntrackCount struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills system.network.conntrack.count metric with initial data.
func (m *metricSystemNetworkConntrackCount) init() {
	m.data.SetName("system.network.conntrack.count")
	m.data.SetDescription("The count of entries in conntrack table.")
	m.data.SetUnit("{entries}")
	m.data.SetEmptySum()
	m.data.Sum().SetIsMonotonic(false)
	m.data.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
}

func (m *metricSystemNetworkConntrackCount) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Sum().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricSystemNetworkConntrackCount) updateCapacity() {
	if m.data.Sum().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Sum().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricSystemNetworkConntrackCount) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Sum().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricSystemNetworkConntrackCount(cfg MetricConfig) metricSystemNetworkConntrackCount {
	m := metricSystemNetworkConntrackCount{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricSystemNetworkConntrackMax struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills system.network.conntrack.max metric with initial data.
func (m *metricSystemNetworkConntrackMax) init() {
	m.data.SetName("system.network.conntrack.max")
	m.data.SetDescription("The limit for entries in the conntrack table.")
	m.data.SetUnit("{entries}")
	m.data.SetEmptySum()
	m.data.Sum().SetIsMonotonic(false)
	m.data.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
}

func (m *metricSystemNetworkConntrackMax) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Sum().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricSystemNetworkConntrackMax) updateCapacity() {
	if m.data.Sum().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Sum().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricSystemNetworkConntrackMax) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Sum().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricSystemNetworkConntrackMax(cfg MetricConfig) metricSystemNetworkConntrackMax {
	m := metricSystemNetworkConntrackMax{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricSystemNetworkDropped struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills system.network.dropped metric with initial data.
func (m *metricSystemNetworkDropped) init() {
	m.data.SetName("system.network.dropped")
	m.data.SetDescription("The number of packets dropped.")
	m.data.SetUnit("{packets}")
	m.data.SetEmptySum()
	m.data.Sum().SetIsMonotonic(true)
	m.data.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	m.data.Sum().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricSystemNetworkDropped) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64, deviceAttributeValue string, directionAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Sum().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
	dp.Attributes().PutStr("device", deviceAttributeValue)
	dp.Attributes().PutStr("direction", directionAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricSystemNetworkDropped) updateCapacity() {
	if m.data.Sum().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Sum().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricSystemNetworkDropped) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Sum().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricSystemNetworkDropped(cfg MetricConfig) metricSystemNetworkDropped {
	m := metricSystemNetworkDropped{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricSystemNetworkErrors struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills system.network.errors metric with initial data.
func (m *metricSystemNetworkErrors) init() {
	m.data.SetName("system.network.errors")
	m.data.SetDescription("The number of errors encountered.")
	m.data.SetUnit("{errors}")
	m.data.SetEmptySum()
	m.data.Sum().SetIsMonotonic(true)
	m.data.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	m.data.Sum().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricSystemNetworkErrors) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64, deviceAttributeValue string, directionAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Sum().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
	dp.Attributes().PutStr("device", deviceAttributeValue)
	dp.Attributes().PutStr("direction", directionAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricSystemNetworkErrors) updateCapacity() {
	if m.data.Sum().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Sum().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricSystemNetworkErrors) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Sum().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricSystemNetworkErrors(cfg MetricConfig) metricSystemNetworkErrors {
	m := metricSystemNetworkErrors{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricSystemNetworkIo struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills system.network.io metric with initial data.
func (m *metricSystemNetworkIo) init() {
	m.data.SetName("system.network.io")
	m.data.SetDescription("The number of bytes transmitted and received.")
	m.data.SetUnit("By")
	m.data.SetEmptySum()
	m.data.Sum().SetIsMonotonic(true)
	m.data.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	m.data.Sum().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricSystemNetworkIo) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64, deviceAttributeValue string, directionAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Sum().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
	dp.Attributes().PutStr("device", deviceAttributeValue)
	dp.Attributes().PutStr("direction", directionAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricSystemNetworkIo) updateCapacity() {
	if m.data.Sum().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Sum().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricSystemNetworkIo) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Sum().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricSystemNetworkIo(cfg MetricConfig) metricSystemNetworkIo {
	m := metricSystemNetworkIo{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricSystemNetworkIoBandwidth struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills system.network.io.bandwidth metric with initial data.
func (m *metricSystemNetworkIoBandwidth) init() {
	m.data.SetName("system.network.io.bandwidth")
	m.data.SetDescription("The rate of transmission and reception.")
	m.data.SetUnit("By/s")
	m.data.SetEmptyGauge()
	m.data.Gauge().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricSystemNetworkIoBandwidth) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val float64, deviceAttributeValue string, directionAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetDoubleValue(val)
	dp.Attributes().PutStr("device", deviceAttributeValue)
	dp.Attributes().PutStr("direction", directionAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricSystemNetworkIoBandwidth) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricSystemNetworkIoBandwidth) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricSystemNetworkIoBandwidth(cfg MetricConfig) metricSystemNetworkIoBandwidth {
	m := metricSystemNetworkIoBandwidth{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricSystemNetworkPackets struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills system.network.packets metric with initial data.
func (m *metricSystemNetworkPackets) init() {
	m.data.SetName("system.network.packets")
	m.data.SetDescription("The number of packets transferred.")
	m.data.SetUnit("{packets}")
	m.data.SetEmptySum()
	m.data.Sum().SetIsMonotonic(true)
	m.data.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	m.data.Sum().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricSystemNetworkPackets) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64, deviceAttributeValue string, directionAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Sum().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
	dp.Attributes().PutStr("device", deviceAttributeValue)
	dp.Attributes().PutStr("direction", directionAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricSystemNetworkPackets) updateCapacity() {
	if m.data.Sum().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Sum().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricSystemNetworkPackets) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Sum().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricSystemNetworkPackets(cfg MetricConfig) metricSystemNetworkPackets {
	m := metricSystemNetworkPackets{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

// MetricsBuilder provides an interface for scrapers to report metrics while taking care of all the transformations
// required to produce metric representation defined in metadata and user config.
type MetricsBuilder struct {
	config                            MetricsBuilderConfig // config of the metrics builder.
	startTime                         pcommon.Timestamp    // start time that will be applied to all recorded data points.
	metricsCapacity                   int                  // maximum observed number of metrics per resource.
	metricsBuffer                     pmetric.Metrics      // accumulates metrics data before emitting.
	buildInfo                         component.BuildInfo  // contains version information.
	metricSystemNetworkConnections    metricSystemNetworkConnections
	metricSystemNetworkConntrackCount metricSystemNetworkConntrackCount
	metricSystemNetworkConntrackMax   metricSystemNetworkConntrackMax
	metricSystemNetworkDropped        metricSystemNetworkDropped
	metricSystemNetworkErrors         metricSystemNetworkErrors
	metricSystemNetworkIo             metricSystemNetworkIo
	metricSystemNetworkIoBandwidth    metricSystemNetworkIoBandwidth
	metricSystemNetworkPackets        metricSystemNetworkPackets
}

// MetricBuilderOption applies changes to default metrics builder.
type MetricBuilderOption interface {
	apply(*MetricsBuilder)
}

type metricBuilderOptionFunc func(mb *MetricsBuilder)

func (mbof metricBuilderOptionFunc) apply(mb *MetricsBuilder) {
	mbof(mb)
}

// WithStartTime sets startTime on the metrics builder.
func WithStartTime(startTime pcommon.Timestamp) MetricBuilderOption {
	return metricBuilderOptionFunc(func(mb *MetricsBuilder) {
		mb.startTime = startTime
	})
}

func NewMetricsBuilder(mbc MetricsBuilderConfig, settings receiver.Settings, options ...MetricBuilderOption) *MetricsBuilder {
	mb := &MetricsBuilder{
		config:                            mbc,
		startTime:                         pcommon.NewTimestampFromTime(time.Now()),
		metricsBuffer:                     pmetric.NewMetrics(),
		buildInfo:                         settings.BuildInfo,
		metricSystemNetworkConnections:    newMetricSystemNetworkConnections(mbc.Metrics.SystemNetworkConnections),
		metricSystemNetworkConntrackCount: newMetricSystemNetworkConntrackCount(mbc.Metrics.SystemNetworkConntrackCount),
		metricSystemNetworkConntrackMax:   newMetricSystemNetworkConntrackMax(mbc.Metrics.SystemNetworkConntrackMax),
		metricSystemNetworkDropped:        newMetricSystemNetworkDropped(mbc.Metrics.SystemNetworkDropped),
		metricSystemNetworkErrors:         newMetricSystemNetworkErrors(mbc.Metrics.SystemNetworkErrors),
		metricSystemNetworkIo:             newMetricSystemNetworkIo(mbc.Metrics.SystemNetworkIo),
		metricSystemNetworkIoBandwidth:    newMetricSystemNetworkIoBandwidth(mbc.Metrics.SystemNetworkIoBandwidth),
		metricSystemNetworkPackets:        newMetricSystemNetworkPackets(mbc.Metrics.SystemNetworkPackets),
	}

	for _, op := range options {
		op.apply(mb)
	}
	return mb
}

// updateCapacity updates max length of metrics and resource attributes that will be used for the slice capacity.
func (mb *MetricsBuilder) updateCapacity(rm pmetric.ResourceMetrics) {
	if mb.metricsCapacity < rm.ScopeMetrics().At(0).Metrics().Len() {
		mb.metricsCapacity = rm.ScopeMetrics().At(0).Metrics().Len()
	}
}

// ResourceMetricsOption applies changes to provided resource metrics.
type ResourceMetricsOption interface {
	apply(pmetric.ResourceMetrics)
}

type resourceMetricsOptionFunc func(pmetric.ResourceMetrics)

func (rmof resourceMetricsOptionFunc) apply(rm pmetric.ResourceMetrics) {
	rmof(rm)
}

// WithResource sets the provided resource on the emitted ResourceMetrics.
// It's recommended to use ResourceBuilder to create the resource.
func WithResource(res pcommon.Resource) ResourceMetricsOption {
	return resourceMetricsOptionFunc(func(rm pmetric.ResourceMetrics) {
		res.CopyTo(rm.Resource())
	})
}

// WithStartTimeOverride overrides start time for all the resource metrics data points.
// This option should be only used if different start time has to be set on metrics coming from different resources.
func WithStartTimeOverride(start pcommon.Timestamp) ResourceMetricsOption {
	return resourceMetricsOptionFunc(func(rm pmetric.ResourceMetrics) {
		var dps pmetric.NumberDataPointSlice
		metrics := rm.ScopeMetrics().At(0).Metrics()
		for i := 0; i < metrics.Len(); i++ {
			switch metrics.At(i).Type() {
			case pmetric.MetricTypeGauge:
				dps = metrics.At(i).Gauge().DataPoints()
			case pmetric.MetricTypeSum:
				dps = metrics.At(i).Sum().DataPoints()
			}
			for j := 0; j < dps.Len(); j++ {
				dps.At(j).SetStartTimestamp(start)
			}
		}
	})
}

// EmitForResource saves all the generated metrics under a new resource and updates the internal state to be ready for
// recording another set of data points as part of another resource. This function can be helpful when one scraper
// needs to emit metrics from several resources. Otherwise calling this function is not required,
// just `Emit` function can be called instead.
// Resource attributes should be provided as ResourceMetricsOption arguments.
func (mb *MetricsBuilder) EmitForResource(options ...ResourceMetricsOption) {
	rm := pmetric.NewResourceMetrics()
	rm.SetSchemaUrl(conventions.SchemaURL)
	ils := rm.ScopeMetrics().AppendEmpty()
	ils.Scope().SetName("github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/networkscraper")
	ils.Scope().SetVersion(mb.buildInfo.Version)
	ils.Metrics().EnsureCapacity(mb.metricsCapacity)
	mb.metricSystemNetworkConnections.emit(ils.Metrics())
	mb.metricSystemNetworkConntrackCount.emit(ils.Metrics())
	mb.metricSystemNetworkConntrackMax.emit(ils.Metrics())
	mb.metricSystemNetworkDropped.emit(ils.Metrics())
	mb.metricSystemNetworkErrors.emit(ils.Metrics())
	mb.metricSystemNetworkIo.emit(ils.Metrics())
	mb.metricSystemNetworkIoBandwidth.emit(ils.Metrics())
	mb.metricSystemNetworkPackets.emit(ils.Metrics())

	for _, op := range options {
		op.apply(rm)
	}

	if ils.Metrics().Len() > 0 {
		mb.updateCapacity(rm)
		rm.MoveTo(mb.metricsBuffer.ResourceMetrics().AppendEmpty())
	}
}

// Emit returns all the metrics accumulated by the metrics builder and updates the internal state to be ready for
// recording another set of metrics. This function will be responsible for applying all the transformations required to
// produce metric representation defined in metadata and user config, e.g. delta or cumulative.
func (mb *MetricsBuilder) Emit(options ...ResourceMetricsOption) pmetric.Metrics {
	mb.EmitForResource(options...)
	metrics := mb.metricsBuffer
	mb.metricsBuffer = pmetric.NewMetrics()
	return metrics
}

// RecordSystemNetworkConnectionsDataPoint adds a data point to system.network.connections metric.
func (mb *MetricsBuilder) RecordSystemNetworkConnectionsDataPoint(ts pcommon.Timestamp, val int64, protocolAttributeValue AttributeProtocol, stateAttributeValue string) {
	mb.metricSystemNetworkConnections.recordDataPoint(mb.startTime, ts, val, protocolAttributeValue.String(), stateAttributeValue)
}

// RecordSystemNetworkConntrackCountDataPoint adds a data point to system.network.conntrack.count metric.
func (mb *MetricsBuilder) RecordSystemNetworkConntrackCountDataPoint(ts pcommon.Timestamp, val int64) {
	mb.metricSystemNetworkConntrackCount.recordDataPoint(mb.startTime, ts, val)
}

// RecordSystemNetworkConntrackMaxDataPoint adds a data point to system.network.conntrack.max metric.
func (mb *MetricsBuilder) RecordSystemNetworkConntrackMaxDataPoint(ts pcommon.Timestamp, val int64) {
	mb.metricSystemNetworkConntrackMax.recordDataPoint(mb.startTime, ts, val)
}

// RecordSystemNetworkDroppedDataPoint adds a data point to system.network.dropped metric.
func (mb *MetricsBuilder) RecordSystemNetworkDroppedDataPoint(ts pcommon.Timestamp, val int64, deviceAttributeValue string, directionAttributeValue AttributeDirection) {
	mb.metricSystemNetworkDropped.recordDataPoint(mb.startTime, ts, val, deviceAttributeValue, directionAttributeValue.String())
}

// RecordSystemNetworkErrorsDataPoint adds a data point to system.network.errors metric.
func (mb *MetricsBuilder) RecordSystemNetworkErrorsDataPoint(ts pcommon.Timestamp, val int64, deviceAttributeValue string, directionAttributeValue AttributeDirection) {
	mb.metricSystemNetworkErrors.recordDataPoint(mb.startTime, ts, val, deviceAttributeValue, directionAttributeValue.String())
}

// RecordSystemNetworkIoDataPoint adds a data point to system.network.io metric.
func (mb *MetricsBuilder) RecordSystemNetworkIoDataPoint(ts pcommon.Timestamp, val int64, deviceAttributeValue string, directionAttributeValue AttributeDirection) {
	mb.metricSystemNetworkIo.recordDataPoint(mb.startTime, ts, val, deviceAttributeValue, directionAttributeValue.String())
}

// RecordSystemNetworkIoBandwidthDataPoint adds a data point to system.network.io.bandwidth metric.
func (mb *MetricsBuilder) RecordSystemNetworkIoBandwidthDataPoint(ts pcommon.Timestamp, val float64, deviceAttributeValue string, directionAttributeValue AttributeDirection) {
	mb.metricSystemNetworkIoBandwidth.recordDataPoint(mb.startTime, ts, val, deviceAttributeValue, directionAttributeValue.String())
}

// RecordSystemNetworkPacketsDataPoint adds a data point to system.network.packets metric.
func (mb *MetricsBuilder) RecordSystemNetworkPacketsDataPoint(ts pcommon.Timestamp, val int64, deviceAttributeValue string, directionAttributeValue AttributeDirection) {
	mb.metricSystemNetworkPackets.recordDataPoint(mb.startTime, ts, val, deviceAttributeValue, directionAttributeValue.String())
}

// Reset resets metrics builder to its initial state. It should be used when external metrics source is restarted,
// and metrics builder should update its startTime and reset it's internal state accordingly.
func (mb *MetricsBuilder) Reset(options ...MetricBuilderOption) {
	mb.startTime = pcommon.NewTimestampFromTime(time.Now())
	for _, op := range options {
		op.apply(mb)
	}
}
