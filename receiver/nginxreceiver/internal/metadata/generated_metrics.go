// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
)

// AttributeState specifies the a value state attribute.
type AttributeState int

const (
	_ AttributeState = iota
	AttributeStateActive
	AttributeStateReading
	AttributeStateWriting
	AttributeStateWaiting
)

// String returns the string representation of the AttributeState.
func (av AttributeState) String() string {
	switch av {
	case AttributeStateActive:
		return "active"
	case AttributeStateReading:
		return "reading"
	case AttributeStateWriting:
		return "writing"
	case AttributeStateWaiting:
		return "waiting"
	}
	return ""
}

// MapAttributeState is a helper map of string to AttributeState attribute value.
var MapAttributeState = map[string]AttributeState{
	"active":  AttributeStateActive,
	"reading": AttributeStateReading,
	"writing": AttributeStateWriting,
	"waiting": AttributeStateWaiting,
}

type metricNginxConnectionsAccepted struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills nginx.connections_accepted metric with initial data.
func (m *metricNginxConnectionsAccepted) init() {
	m.data.SetName("nginx.connections_accepted")
	m.data.SetDescription("The total number of accepted client connections")
	m.data.SetUnit("connections")
	m.data.SetEmptySum()
	m.data.Sum().SetIsMonotonic(true)
	m.data.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
}

func (m *metricNginxConnectionsAccepted) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Sum().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricNginxConnectionsAccepted) updateCapacity() {
	if m.data.Sum().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Sum().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricNginxConnectionsAccepted) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Sum().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricNginxConnectionsAccepted(cfg MetricConfig) metricNginxConnectionsAccepted {
	m := metricNginxConnectionsAccepted{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricNginxConnectionsCurrent struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills nginx.connections_current metric with initial data.
func (m *metricNginxConnectionsCurrent) init() {
	m.data.SetName("nginx.connections_current")
	m.data.SetDescription("The current number of nginx connections by state")
	m.data.SetUnit("connections")
	m.data.SetEmptySum()
	m.data.Sum().SetIsMonotonic(false)
	m.data.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	m.data.Sum().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricNginxConnectionsCurrent) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64, stateAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Sum().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
	dp.Attributes().PutStr("state", stateAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricNginxConnectionsCurrent) updateCapacity() {
	if m.data.Sum().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Sum().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricNginxConnectionsCurrent) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Sum().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricNginxConnectionsCurrent(cfg MetricConfig) metricNginxConnectionsCurrent {
	m := metricNginxConnectionsCurrent{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricNginxConnectionsHandled struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills nginx.connections_handled metric with initial data.
func (m *metricNginxConnectionsHandled) init() {
	m.data.SetName("nginx.connections_handled")
	m.data.SetDescription("The total number of handled connections. Generally, the parameter value is the same as nginx.connections_accepted unless some resource limits have been reached (for example, the worker_connections limit).")
	m.data.SetUnit("connections")
	m.data.SetEmptySum()
	m.data.Sum().SetIsMonotonic(true)
	m.data.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
}

func (m *metricNginxConnectionsHandled) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Sum().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricNginxConnectionsHandled) updateCapacity() {
	if m.data.Sum().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Sum().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricNginxConnectionsHandled) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Sum().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricNginxConnectionsHandled(cfg MetricConfig) metricNginxConnectionsHandled {
	m := metricNginxConnectionsHandled{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricNginxLoadTimestamp struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills nginx.load_timestamp metric with initial data.
func (m *metricNginxLoadTimestamp) init() {
	m.data.SetName("nginx.load_timestamp")
	m.data.SetDescription("Time of the last reload of configuration (time since Epoch).")
	m.data.SetUnit("ms")
	m.data.SetEmptyGauge()
}

func (m *metricNginxLoadTimestamp) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricNginxLoadTimestamp) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricNginxLoadTimestamp) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricNginxLoadTimestamp(cfg MetricConfig) metricNginxLoadTimestamp {
	m := metricNginxLoadTimestamp{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricNginxNetReading struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills nginx.net.reading metric with initial data.
func (m *metricNginxNetReading) init() {
	m.data.SetName("nginx.net.reading")
	m.data.SetDescription("Current number of connections where NGINX is reading the request header")
	m.data.SetUnit("connections")
	m.data.SetEmptyGauge()
}

func (m *metricNginxNetReading) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricNginxNetReading) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricNginxNetReading) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricNginxNetReading(cfg MetricConfig) metricNginxNetReading {
	m := metricNginxNetReading{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricNginxNetWaiting struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills nginx.net.waiting metric with initial data.
func (m *metricNginxNetWaiting) init() {
	m.data.SetName("nginx.net.waiting")
	m.data.SetDescription("Current number of connections where NGINX is waiting the response back to the client")
	m.data.SetUnit("connections")
	m.data.SetEmptyGauge()
}

func (m *metricNginxNetWaiting) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricNginxNetWaiting) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricNginxNetWaiting) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricNginxNetWaiting(cfg MetricConfig) metricNginxNetWaiting {
	m := metricNginxNetWaiting{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricNginxNetWriting struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills nginx.net.writing metric with initial data.
func (m *metricNginxNetWriting) init() {
	m.data.SetName("nginx.net.writing")
	m.data.SetDescription("Current number of connections where NGINX is writing the response back to the client")
	m.data.SetUnit("connections")
	m.data.SetEmptyGauge()
}

func (m *metricNginxNetWriting) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricNginxNetWriting) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricNginxNetWriting) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricNginxNetWriting(cfg MetricConfig) metricNginxNetWriting {
	m := metricNginxNetWriting{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricNginxRequests struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills nginx.requests metric with initial data.
func (m *metricNginxRequests) init() {
	m.data.SetName("nginx.requests")
	m.data.SetDescription("Total number of requests made to the server since it started")
	m.data.SetUnit("requests")
	m.data.SetEmptySum()
	m.data.Sum().SetIsMonotonic(true)
	m.data.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
}

func (m *metricNginxRequests) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Sum().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricNginxRequests) updateCapacity() {
	if m.data.Sum().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Sum().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricNginxRequests) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Sum().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricNginxRequests(cfg MetricConfig) metricNginxRequests {
	m := metricNginxRequests{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricNginxServerZoneResponses1xx struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills nginx.server_zone.responses.1xx metric with initial data.
func (m *metricNginxServerZoneResponses1xx) init() {
	m.data.SetName("nginx.server_zone.responses.1xx")
	m.data.SetDescription("The number of responses with 1xx status code.")
	m.data.SetUnit("response")
	m.data.SetEmptyGauge()
	m.data.Gauge().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricNginxServerZoneResponses1xx) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64, serverzoneNameAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
	dp.Attributes().PutStr("serverzone_name", serverzoneNameAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricNginxServerZoneResponses1xx) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricNginxServerZoneResponses1xx) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricNginxServerZoneResponses1xx(cfg MetricConfig) metricNginxServerZoneResponses1xx {
	m := metricNginxServerZoneResponses1xx{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricNginxServerZoneResponses2xx struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills nginx.server_zone.responses.2xx metric with initial data.
func (m *metricNginxServerZoneResponses2xx) init() {
	m.data.SetName("nginx.server_zone.responses.2xx")
	m.data.SetDescription("The number of responses with 2xx status code.")
	m.data.SetUnit("response")
	m.data.SetEmptyGauge()
	m.data.Gauge().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricNginxServerZoneResponses2xx) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64, serverzoneNameAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
	dp.Attributes().PutStr("serverzone_name", serverzoneNameAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricNginxServerZoneResponses2xx) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricNginxServerZoneResponses2xx) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricNginxServerZoneResponses2xx(cfg MetricConfig) metricNginxServerZoneResponses2xx {
	m := metricNginxServerZoneResponses2xx{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricNginxServerZoneResponses3xx struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills nginx.server_zone.responses.3xx metric with initial data.
func (m *metricNginxServerZoneResponses3xx) init() {
	m.data.SetName("nginx.server_zone.responses.3xx")
	m.data.SetDescription("The number of responses with 3xx status code.")
	m.data.SetUnit("response")
	m.data.SetEmptyGauge()
	m.data.Gauge().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricNginxServerZoneResponses3xx) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64, serverzoneNameAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
	dp.Attributes().PutStr("serverzone_name", serverzoneNameAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricNginxServerZoneResponses3xx) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricNginxServerZoneResponses3xx) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricNginxServerZoneResponses3xx(cfg MetricConfig) metricNginxServerZoneResponses3xx {
	m := metricNginxServerZoneResponses3xx{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricNginxServerZoneResponses4xx struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills nginx.server_zone.responses.4xx metric with initial data.
func (m *metricNginxServerZoneResponses4xx) init() {
	m.data.SetName("nginx.server_zone.responses.4xx")
	m.data.SetDescription("The number of responses with 4xx status code.")
	m.data.SetUnit("response")
	m.data.SetEmptyGauge()
	m.data.Gauge().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricNginxServerZoneResponses4xx) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64, serverzoneNameAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
	dp.Attributes().PutStr("serverzone_name", serverzoneNameAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricNginxServerZoneResponses4xx) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricNginxServerZoneResponses4xx) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricNginxServerZoneResponses4xx(cfg MetricConfig) metricNginxServerZoneResponses4xx {
	m := metricNginxServerZoneResponses4xx{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricNginxServerZoneResponses5xx struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills nginx.server_zone.responses.5xx metric with initial data.
func (m *metricNginxServerZoneResponses5xx) init() {
	m.data.SetName("nginx.server_zone.responses.5xx")
	m.data.SetDescription("The number of responses with 5xx status code.")
	m.data.SetUnit("response")
	m.data.SetEmptyGauge()
	m.data.Gauge().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricNginxServerZoneResponses5xx) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64, serverzoneNameAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
	dp.Attributes().PutStr("serverzone_name", serverzoneNameAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricNginxServerZoneResponses5xx) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricNginxServerZoneResponses5xx) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricNginxServerZoneResponses5xx(cfg MetricConfig) metricNginxServerZoneResponses5xx {
	m := metricNginxServerZoneResponses5xx{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricNginxUpstreamPeersResponseTime struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills nginx.upstream.peers.response_time metric with initial data.
func (m *metricNginxUpstreamPeersResponseTime) init() {
	m.data.SetName("nginx.upstream.peers.response_time")
	m.data.SetDescription("The average time to receive the last byte of data from this server.")
	m.data.SetUnit("ms")
	m.data.SetEmptyGauge()
	m.data.Gauge().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricNginxUpstreamPeersResponseTime) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64, upstreamBlockNameAttributeValue string, upstreamPeerAddressAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
	dp.Attributes().PutStr("upstream_block_name", upstreamBlockNameAttributeValue)
	dp.Attributes().PutStr("upstream_peer_address", upstreamPeerAddressAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricNginxUpstreamPeersResponseTime) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricNginxUpstreamPeersResponseTime) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricNginxUpstreamPeersResponseTime(cfg MetricConfig) metricNginxUpstreamPeersResponseTime {
	m := metricNginxUpstreamPeersResponseTime{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

// MetricsBuilder provides an interface for scrapers to report metrics while taking care of all the transformations
// required to produce metric representation defined in metadata and user config.
type MetricsBuilder struct {
	config                               MetricsBuilderConfig // config of the metrics builder.
	startTime                            pcommon.Timestamp    // start time that will be applied to all recorded data points.
	metricsCapacity                      int                  // maximum observed number of metrics per resource.
	metricsBuffer                        pmetric.Metrics      // accumulates metrics data before emitting.
	buildInfo                            component.BuildInfo  // contains version information.
	metricNginxConnectionsAccepted       metricNginxConnectionsAccepted
	metricNginxConnectionsCurrent        metricNginxConnectionsCurrent
	metricNginxConnectionsHandled        metricNginxConnectionsHandled
	metricNginxLoadTimestamp             metricNginxLoadTimestamp
	metricNginxNetReading                metricNginxNetReading
	metricNginxNetWaiting                metricNginxNetWaiting
	metricNginxNetWriting                metricNginxNetWriting
	metricNginxRequests                  metricNginxRequests
	metricNginxServerZoneResponses1xx    metricNginxServerZoneResponses1xx
	metricNginxServerZoneResponses2xx    metricNginxServerZoneResponses2xx
	metricNginxServerZoneResponses3xx    metricNginxServerZoneResponses3xx
	metricNginxServerZoneResponses4xx    metricNginxServerZoneResponses4xx
	metricNginxServerZoneResponses5xx    metricNginxServerZoneResponses5xx
	metricNginxUpstreamPeersResponseTime metricNginxUpstreamPeersResponseTime
}

// metricBuilderOption applies changes to default metrics builder.
type metricBuilderOption func(*MetricsBuilder)

// WithStartTime sets startTime on the metrics builder.
func WithStartTime(startTime pcommon.Timestamp) metricBuilderOption {
	return func(mb *MetricsBuilder) {
		mb.startTime = startTime
	}
}

func NewMetricsBuilder(mbc MetricsBuilderConfig, settings receiver.Settings, options ...metricBuilderOption) *MetricsBuilder {
	mb := &MetricsBuilder{
		config:                               mbc,
		startTime:                            pcommon.NewTimestampFromTime(time.Now()),
		metricsBuffer:                        pmetric.NewMetrics(),
		buildInfo:                            settings.BuildInfo,
		metricNginxConnectionsAccepted:       newMetricNginxConnectionsAccepted(mbc.Metrics.NginxConnectionsAccepted),
		metricNginxConnectionsCurrent:        newMetricNginxConnectionsCurrent(mbc.Metrics.NginxConnectionsCurrent),
		metricNginxConnectionsHandled:        newMetricNginxConnectionsHandled(mbc.Metrics.NginxConnectionsHandled),
		metricNginxLoadTimestamp:             newMetricNginxLoadTimestamp(mbc.Metrics.NginxLoadTimestamp),
		metricNginxNetReading:                newMetricNginxNetReading(mbc.Metrics.NginxNetReading),
		metricNginxNetWaiting:                newMetricNginxNetWaiting(mbc.Metrics.NginxNetWaiting),
		metricNginxNetWriting:                newMetricNginxNetWriting(mbc.Metrics.NginxNetWriting),
		metricNginxRequests:                  newMetricNginxRequests(mbc.Metrics.NginxRequests),
		metricNginxServerZoneResponses1xx:    newMetricNginxServerZoneResponses1xx(mbc.Metrics.NginxServerZoneResponses1xx),
		metricNginxServerZoneResponses2xx:    newMetricNginxServerZoneResponses2xx(mbc.Metrics.NginxServerZoneResponses2xx),
		metricNginxServerZoneResponses3xx:    newMetricNginxServerZoneResponses3xx(mbc.Metrics.NginxServerZoneResponses3xx),
		metricNginxServerZoneResponses4xx:    newMetricNginxServerZoneResponses4xx(mbc.Metrics.NginxServerZoneResponses4xx),
		metricNginxServerZoneResponses5xx:    newMetricNginxServerZoneResponses5xx(mbc.Metrics.NginxServerZoneResponses5xx),
		metricNginxUpstreamPeersResponseTime: newMetricNginxUpstreamPeersResponseTime(mbc.Metrics.NginxUpstreamPeersResponseTime),
	}

	for _, op := range options {
		op(mb)
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
type ResourceMetricsOption func(pmetric.ResourceMetrics)

// WithResource sets the provided resource on the emitted ResourceMetrics.
// It's recommended to use ResourceBuilder to create the resource.
func WithResource(res pcommon.Resource) ResourceMetricsOption {
	return func(rm pmetric.ResourceMetrics) {
		res.CopyTo(rm.Resource())
	}
}

// WithStartTimeOverride overrides start time for all the resource metrics data points.
// This option should be only used if different start time has to be set on metrics coming from different resources.
func WithStartTimeOverride(start pcommon.Timestamp) ResourceMetricsOption {
	return func(rm pmetric.ResourceMetrics) {
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
	}
}

// EmitForResource saves all the generated metrics under a new resource and updates the internal state to be ready for
// recording another set of data points as part of another resource. This function can be helpful when one scraper
// needs to emit metrics from several resources. Otherwise calling this function is not required,
// just `Emit` function can be called instead.
// Resource attributes should be provided as ResourceMetricsOption arguments.
func (mb *MetricsBuilder) EmitForResource(rmo ...ResourceMetricsOption) {
	rm := pmetric.NewResourceMetrics()
	ils := rm.ScopeMetrics().AppendEmpty()
	ils.Scope().SetName("otelcol/nginxreceiver")
	ils.Scope().SetVersion(mb.buildInfo.Version)
	ils.Metrics().EnsureCapacity(mb.metricsCapacity)
	mb.metricNginxConnectionsAccepted.emit(ils.Metrics())
	mb.metricNginxConnectionsCurrent.emit(ils.Metrics())
	mb.metricNginxConnectionsHandled.emit(ils.Metrics())
	mb.metricNginxLoadTimestamp.emit(ils.Metrics())
	mb.metricNginxNetReading.emit(ils.Metrics())
	mb.metricNginxNetWaiting.emit(ils.Metrics())
	mb.metricNginxNetWriting.emit(ils.Metrics())
	mb.metricNginxRequests.emit(ils.Metrics())
	mb.metricNginxServerZoneResponses1xx.emit(ils.Metrics())
	mb.metricNginxServerZoneResponses2xx.emit(ils.Metrics())
	mb.metricNginxServerZoneResponses3xx.emit(ils.Metrics())
	mb.metricNginxServerZoneResponses4xx.emit(ils.Metrics())
	mb.metricNginxServerZoneResponses5xx.emit(ils.Metrics())
	mb.metricNginxUpstreamPeersResponseTime.emit(ils.Metrics())

	for _, op := range rmo {
		op(rm)
	}

	if ils.Metrics().Len() > 0 {
		mb.updateCapacity(rm)
		rm.MoveTo(mb.metricsBuffer.ResourceMetrics().AppendEmpty())
	}
}

// Emit returns all the metrics accumulated by the metrics builder and updates the internal state to be ready for
// recording another set of metrics. This function will be responsible for applying all the transformations required to
// produce metric representation defined in metadata and user config, e.g. delta or cumulative.
func (mb *MetricsBuilder) Emit(rmo ...ResourceMetricsOption) pmetric.Metrics {
	mb.EmitForResource(rmo...)
	metrics := mb.metricsBuffer
	mb.metricsBuffer = pmetric.NewMetrics()
	return metrics
}

// RecordNginxConnectionsAcceptedDataPoint adds a data point to nginx.connections_accepted metric.
func (mb *MetricsBuilder) RecordNginxConnectionsAcceptedDataPoint(ts pcommon.Timestamp, val int64) {
	mb.metricNginxConnectionsAccepted.recordDataPoint(mb.startTime, ts, val)
}

// RecordNginxConnectionsCurrentDataPoint adds a data point to nginx.connections_current metric.
func (mb *MetricsBuilder) RecordNginxConnectionsCurrentDataPoint(ts pcommon.Timestamp, val int64, stateAttributeValue AttributeState) {
	mb.metricNginxConnectionsCurrent.recordDataPoint(mb.startTime, ts, val, stateAttributeValue.String())
}

// RecordNginxConnectionsHandledDataPoint adds a data point to nginx.connections_handled metric.
func (mb *MetricsBuilder) RecordNginxConnectionsHandledDataPoint(ts pcommon.Timestamp, val int64) {
	mb.metricNginxConnectionsHandled.recordDataPoint(mb.startTime, ts, val)
}

// RecordNginxLoadTimestampDataPoint adds a data point to nginx.load_timestamp metric.
func (mb *MetricsBuilder) RecordNginxLoadTimestampDataPoint(ts pcommon.Timestamp, val int64) {
	mb.metricNginxLoadTimestamp.recordDataPoint(mb.startTime, ts, val)
}

// RecordNginxNetReadingDataPoint adds a data point to nginx.net.reading metric.
func (mb *MetricsBuilder) RecordNginxNetReadingDataPoint(ts pcommon.Timestamp, val int64) {
	mb.metricNginxNetReading.recordDataPoint(mb.startTime, ts, val)
}

// RecordNginxNetWaitingDataPoint adds a data point to nginx.net.waiting metric.
func (mb *MetricsBuilder) RecordNginxNetWaitingDataPoint(ts pcommon.Timestamp, val int64) {
	mb.metricNginxNetWaiting.recordDataPoint(mb.startTime, ts, val)
}

// RecordNginxNetWritingDataPoint adds a data point to nginx.net.writing metric.
func (mb *MetricsBuilder) RecordNginxNetWritingDataPoint(ts pcommon.Timestamp, val int64) {
	mb.metricNginxNetWriting.recordDataPoint(mb.startTime, ts, val)
}

// RecordNginxRequestsDataPoint adds a data point to nginx.requests metric.
func (mb *MetricsBuilder) RecordNginxRequestsDataPoint(ts pcommon.Timestamp, val int64) {
	mb.metricNginxRequests.recordDataPoint(mb.startTime, ts, val)
}

// RecordNginxServerZoneResponses1xxDataPoint adds a data point to nginx.server_zone.responses.1xx metric.
func (mb *MetricsBuilder) RecordNginxServerZoneResponses1xxDataPoint(ts pcommon.Timestamp, val int64, serverzoneNameAttributeValue string) {
	mb.metricNginxServerZoneResponses1xx.recordDataPoint(mb.startTime, ts, val, serverzoneNameAttributeValue)
}

// RecordNginxServerZoneResponses2xxDataPoint adds a data point to nginx.server_zone.responses.2xx metric.
func (mb *MetricsBuilder) RecordNginxServerZoneResponses2xxDataPoint(ts pcommon.Timestamp, val int64, serverzoneNameAttributeValue string) {
	mb.metricNginxServerZoneResponses2xx.recordDataPoint(mb.startTime, ts, val, serverzoneNameAttributeValue)
}

// RecordNginxServerZoneResponses3xxDataPoint adds a data point to nginx.server_zone.responses.3xx metric.
func (mb *MetricsBuilder) RecordNginxServerZoneResponses3xxDataPoint(ts pcommon.Timestamp, val int64, serverzoneNameAttributeValue string) {
	mb.metricNginxServerZoneResponses3xx.recordDataPoint(mb.startTime, ts, val, serverzoneNameAttributeValue)
}

// RecordNginxServerZoneResponses4xxDataPoint adds a data point to nginx.server_zone.responses.4xx metric.
func (mb *MetricsBuilder) RecordNginxServerZoneResponses4xxDataPoint(ts pcommon.Timestamp, val int64, serverzoneNameAttributeValue string) {
	mb.metricNginxServerZoneResponses4xx.recordDataPoint(mb.startTime, ts, val, serverzoneNameAttributeValue)
}

// RecordNginxServerZoneResponses5xxDataPoint adds a data point to nginx.server_zone.responses.5xx metric.
func (mb *MetricsBuilder) RecordNginxServerZoneResponses5xxDataPoint(ts pcommon.Timestamp, val int64, serverzoneNameAttributeValue string) {
	mb.metricNginxServerZoneResponses5xx.recordDataPoint(mb.startTime, ts, val, serverzoneNameAttributeValue)
}

// RecordNginxUpstreamPeersResponseTimeDataPoint adds a data point to nginx.upstream.peers.response_time metric.
func (mb *MetricsBuilder) RecordNginxUpstreamPeersResponseTimeDataPoint(ts pcommon.Timestamp, val int64, upstreamBlockNameAttributeValue string, upstreamPeerAddressAttributeValue string) {
	mb.metricNginxUpstreamPeersResponseTime.recordDataPoint(mb.startTime, ts, val, upstreamBlockNameAttributeValue, upstreamPeerAddressAttributeValue)
}

// Reset resets metrics builder to its initial state. It should be used when external metrics source is restarted,
// and metrics builder should update its startTime and reset it's internal state accordingly.
func (mb *MetricsBuilder) Reset(options ...metricBuilderOption) {
	mb.startTime = pcommon.NewTimestampFromTime(time.Now())
	for _, op := range options {
		op(mb)
	}
}
