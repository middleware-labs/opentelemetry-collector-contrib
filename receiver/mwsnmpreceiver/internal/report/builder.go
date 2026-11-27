package report

import (
	"errors"
	"fmt"
	"hash/maphash"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/naming"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/profile"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/profiledefinition"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/snmp"
)

// ScopeName identifies this receiver as the instrumentation scope.
const ScopeName = "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver"

// DeviceInfo is what the discovery layer knows about a device, and becomes the
// resource the metrics are reported for.
type DeviceInfo struct {
	// ID is a stable identifier for the device across polls and restarts.
	ID string
	// Address is the device's IP.
	Address string
	Port    uint16
	// Subnet is the configured CIDR the device was discovered in, empty for a
	// statically configured device.
	Subnet      string
	SysObjectID string
	ProfileName string
	// Namespace is the device's namespace; ID is <Namespace>:<Address>.
	Namespace string
	// HostName selects whether and how host.name is set; see HostName*.
	HostName string
}

// host.name modes. A network device is not a host, and sysName is often a
// default ("Switch", "Router") shared by many devices, so by default host.name
// is not set and the device is identified by host.id, as in the Datadog agent.
const (
	// HostNameNone leaves host.name unset (the default).
	HostNameNone = "none"
	// HostNameSysName sets host.name from sysName, falling back to the IP.
	HostNameSysName = "sys_name"
	// HostNameDeviceID sets host.name to device:<namespace>:<ip>, the Datadog
	// agent's use_device_id_as_hostname.
	HostNameDeviceID = "device_id"
)

// BuildReport summarises one build, for self-telemetry and for reporting how
// much of a profile is still unmapped.
type BuildReport struct {
	DataPoints int
	// GeneratedMetrics counts distinct metric names that fell back to the
	// generated namespace, i.e. the size of the curation gap for this device.
	GeneratedMetrics map[string]struct{}
	// SkippedSymbols counts symbols that produced nothing.
	SkippedSymbols int
}

// Builder converts poll results to OTLP metrics for one device. It is stateful:
// cumulative streams need a start timestamp, and bandwidth utilisation needs the
// previous poll's counters.
type Builder struct {
	registry *naming.Registry

	mu sync.Mutex
	// startTimes records when each cumulative stream was first observed.
	//
	// Both maps are keyed by a 64-bit hash of the stream key rather than the
	// key text, and entries not seen for staleAfterPolls polls are removed.
	// This is how the Datadog agent keeps per-series state too (64-bit
	// context keys that expire). Keyed by text and never pruned, the maps
	// were most of the receiver's memory, about 170 KB for a 48-port switch,
	// and grew whenever a port's alias changed or a port went away.
	startTimes map[uint64]startEntry
	// prevCounters holds the last reading per interface counter stream, for
	// deriving bandwidth utilisation.
	prevCounters map[uint64]counterSample
	// generation counts builds, so stale entries can be found.
	generation uint32
	// prevAt and curAt are the previous and current build times. Every series
	// of a device is read in the same poll, so the previous point's time is
	// kept once here rather than per series.
	prevAt, curAt pcommon.Timestamp
	// lastUptime is the previous poll's sysUpTime in hundredths of a second,
	// 0 if the device did not report it.
	lastUptime float64
}

// uptimeWrapped reports whether sysUpTime went from near its 32-bit maximum to
// near zero, which is the TimeTicks counter wrapping (about every 497 days),
// not a reboot. A reboot at that moment is still caught per series, since its
// counters go down.
func uptimeWrapped(previous, current float64) bool {
	const wrap = float64(1 << 32)
	return previous > 0.9*wrap && current < 0.1*wrap
}

// oidSysUpTime is SNMPv2-MIB sysUpTime.0. When it goes backwards the device
// rebooted, and every cumulative counter on it restarted from zero.
const oidSysUpTime = "1.3.6.1.2.1.1.3.0"

// staleAfterPolls is how many polls a stream may be absent before its state
// is dropped. A stream that comes back after that gets a new start timestamp,
// which a backend treats as a counter reset.
const staleAfterPolls = 5

// keySeed makes stream key hashes. The state is in memory only, so a seed per
// process is fine.
var keySeed = maphash.MakeSeed()

func hashKey(key string) uint64 { return maphash.String(keySeed, key) }

// startEntry is 24 bytes: it exists once per cumulative series, which is
// most of the receiver's memory.
type startEntry struct {
	start pcommon.Timestamp
	// last is the previous value, to detect a counter reset.
	last float64
	seen uint32
}

type counterSample struct {
	value float64
	at    time.Time
	seen  uint32
}

// NewBuilder returns a builder for one device.
func NewBuilder(registry *naming.Registry) *Builder {
	return &Builder{
		registry:     registry,
		startTimes:   map[uint64]startEntry{},
		prevCounters: map[uint64]counterSample{},
	}
}

// Build produces the metrics for one poll.
//
// Errors are collected rather than fatal: SNMP polls are routinely partial, and
// a device that answers ninety per cent of its OIDs should report ninety per
// cent of its metrics.
func (b *Builder) Build(dev DeviceInfo, compiled *profile.Compiled, store *snmp.ValueStore, now time.Time) (pmetric.Metrics, *BuildReport, error) {
	def := compiled.Definition
	resolver := &tagResolver{compiled: compiled, store: store}
	report := &BuildReport{GeneratedMetrics: map[string]struct{}{}}
	var errs []error

	b.mu.Lock()
	b.generation++
	b.prevAt, b.curAt = b.curAt, pcommon.NewTimestampFromTime(now)
	if uptime, err := store.Scalar(oidSysUpTime); err == nil {
		if ticks, err := uptime.Float(); err == nil {
			if b.lastUptime > 0 && ticks < b.lastUptime && !uptimeWrapped(b.lastUptime, ticks) {
				// Rebooted: every series starts again.
				clear(b.startTimes)
			}
			b.lastUptime = ticks
		}
	}
	b.mu.Unlock()
	defer b.pruneStale()

	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()

	deviceTags, err := resolver.deviceTags(def.MetricTags)
	if err != nil {
		errs = append(errs, fmt.Errorf("device tags: %w", err))
	}
	b.setResourceAttributes(rm.Resource(), dev, compiled, store, deviceTags)

	sm := rm.ScopeMetrics().AppendEmpty()
	sm.Scope().SetName(ScopeName)
	pool := newMetricPool(sm.Metrics())

	profileStatic := staticTags(def.StaticTags)
	ts := pcommon.NewTimestampFromTime(now)

	for i := range def.Metrics {
		m := &def.Metrics[i]
		if err := b.emitMetric(pool, resolver, store, m, profileStatic, ts, now, report); err != nil {
			errs = append(errs, fmt.Errorf("metrics[%d] (%s): %w", i, metricLabel(m), err))
		}
	}

	b.emitBandwidthUtilization(pool, resolver, store, def, ts, now, report)

	report.DataPoints = md.DataPointCount()
	return md, report, errors.Join(errs...)
}

func metricLabel(m *profiledefinition.MetricsConfig) string {
	if m.Symbol.Name != "" {
		return m.Symbol.Name
	}
	if m.Table.Name != "" {
		return m.Table.Name
	}
	return m.MIB
}

// setResourceAttributes puts device identity on the resource. The hardware
// semantic conventions require this: hw.* metrics must not carry attributes
// identifying the device they came from.
func (b *Builder) setResourceAttributes(res pcommon.Resource, dev DeviceInfo,
	compiled *profile.Compiled, store *snmp.ValueStore, deviceTags map[string]string) {

	attrs := res.Attributes()
	attrs.PutStr("host.id", dev.ID)
	attrs.PutStr("snmp.device.ip", dev.Address)
	if dev.Namespace != "" {
		attrs.PutStr("snmp.device.namespace", dev.Namespace)
	}
	if dev.SysObjectID != "" {
		attrs.PutStr("snmp.device.sys_object_id", dev.SysObjectID)
	}
	if dev.ProfileName != "" {
		attrs.PutStr("snmp.profile", dev.ProfileName)
	}
	if dev.Subnet != "" {
		attrs.PutStr("snmp.subnet", dev.Subnet)
	}

	// Device inventory fields become resource attributes, mapped onto semconv
	// names where one exists.
	meta := b.deviceMetadata(compiled, store)
	for field, value := range meta {
		switch field {
		case "name", "sys_name":
			// Set once below, so a profile defining both cannot make the
			// name flip between polls with map iteration order.
		case "vendor":
			attrs.PutStr("device.manufacturer", value)
		case "model":
			attrs.PutStr("device.model.identifier", value)
		case "os_name":
			attrs.PutStr("os.name", value)
		case "os_version", "version":
			attrs.PutStr("os.version", value)
		case "serial_number":
			attrs.PutStr("snmp.device.serial_number", value)
		default:
			attrs.PutStr("snmp.device."+field, value)
		}
	}

	if name := preferredDeviceName(meta); name != "" {
		attrs.PutStr("snmp.device.name", name)
		if dev.HostName == HostNameSysName {
			attrs.PutStr("host.name", name)
		}
	}

	// Profile-level tags identify the device (snmp_host from sysName, for
	// example), so they belong here rather than on datapoints.
	for k, v := range deviceTags {
		if _, exists := attrs.Get(k); !exists {
			attrs.PutStr(k, v)
		}
	}
	switch dev.HostName {
	case HostNameSysName:
		if _, ok := attrs.Get("host.name"); !ok {
			attrs.PutStr("host.name", dev.Address)
		}
	case HostNameDeviceID:
		attrs.PutStr("host.name", "device:"+dev.ID)
	}
}

// preferredDeviceName picks the device name: the profile's name field, else
// sys_name.
func preferredDeviceName(meta map[string]string) string {
	if name := meta["name"]; name != "" {
		return name
	}
	return meta["sys_name"]
}

// deviceMetadata resolves the profile's device inventory fields.
func (b *Builder) deviceMetadata(compiled *profile.Compiled, store *snmp.ValueStore) map[string]string {
	res, ok := compiled.Definition.Metadata["device"]
	if !ok {
		return nil
	}
	out := make(map[string]string, len(res.Fields))
	for name, field := range res.Fields {
		if field.Value != "" {
			out[name] = field.Value
			continue
		}
		candidates := field.Symbols
		if field.Symbol.OID != "" {
			candidates = append([]profiledefinition.SymbolConfig{field.Symbol}, candidates...)
		}
		// The first candidate that yields a value wins, which is how profiles
		// try a vendor-specific OID before falling back to sysDescr.
		for _, sym := range candidates {
			value, err := store.Scalar(sym.OID)
			if err != nil {
				continue
			}
			// stringValue applies match_pattern and extract_value. Profiles
			// lean on them to cut os_name, os_version and model out of
			// sysDescr; without them the whole sysDescr text would be
			// reported for each of those fields.
			text, err := stringValue(compiled, sym, value)
			if err != nil || text == "" {
				continue
			}
			out[name] = text
			break
		}
	}
	return out
}

// emitMetric handles one profile metric entry, scalar or table.
func (b *Builder) emitMetric(pool *metricPool, resolver *tagResolver, store *snmp.ValueStore,
	m *profiledefinition.MetricsConfig, profileStatic map[string]string,
	ts pcommon.Timestamp, now time.Time, report *BuildReport) error {

	metricStatic := mergeTags(nil, profileStatic, staticTags(m.StaticTags))

	if m.IsColumn() {
		return b.emitTableMetric(pool, resolver, store, m, metricStatic, ts, now, report)
	}
	return b.emitScalarMetric(pool, resolver, store, m, metricStatic, ts, report)
}

func (b *Builder) emitScalarMetric(pool *metricPool, resolver *tagResolver, store *snmp.ValueStore,
	m *profiledefinition.MetricsConfig, static map[string]string,
	ts pcommon.Timestamp, report *BuildReport) error {

	value, err := store.Scalar(m.Symbol.OID)
	if err != nil {
		report.SkippedSymbols++
		// An absent scalar is normal; the OID gets pruned by the caller.
		return nil //nolint:nilerr // absence is not a failure
	}

	tags, tagErr := resolver.deviceTags(m.MetricTags)
	attrs := mergeTags(nil, static, tags)

	err = b.emitSymbol(pool, store, m, m.Symbol, value, attrs, "", "", ts, report)
	return errors.Join(tagErr, err)
}

func (b *Builder) emitTableMetric(pool *metricPool, resolver *tagResolver, store *snmp.ValueStore,
	m *profiledefinition.MetricsConfig, static map[string]string,
	ts pcommon.Timestamp, now time.Time, report *BuildReport) error {

	var errs []error
	component := componentPrefix(m.Table.Name, m.Table.OID)

	// Row tags depend only on the row index, not the symbol, so resolve each
	// row once and share the result across the metric's symbols: tag resolution
	// (column lookups, index transforms, regexes) is the hot path of a poll.
	rowTagCache := map[string]map[string]string{}

	for _, sym := range m.Symbols {
		rows, err := b.rowsFor(store, m, sym)
		if err != nil {
			report.SkippedSymbols++
			continue
		}
		for _, index := range sortedIndexes(rows) {
			tags, cached := rowTagCache[index]
			if !cached {
				var tagErr error
				tags, tagErr = resolver.rowTags(m.MetricTags, index)
				if tagErr != nil {
					errs = append(errs, tagErr)
				}
				rowTagCache[index] = tags
			}
			attrs := mergeTags(nil, static, tags)

			value := rows[index]
			if err := b.emitSymbol(pool, store, m, sym, value, attrs, component, index, ts, report); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// rowsFor returns the rows to emit for a symbol. A constant_value_one symbol has
// no column of its own, so it borrows the row set of a sibling that does --
// otherwise the entity count it represents would never be reported.
func (b *Builder) rowsFor(store *snmp.ValueStore, m *profiledefinition.MetricsConfig,
	sym profiledefinition.SymbolConfig) (map[string]snmp.ResultValue, error) {

	if sym.OID != "" {
		return store.Column(sym.OID)
	}
	if !sym.ConstantValueOne {
		return nil, fmt.Errorf("symbol %q has no OID", sym.Name)
	}
	for _, sibling := range m.Symbols {
		if sibling.OID == "" {
			continue
		}
		if rows, err := store.Column(sibling.OID); err == nil && len(rows) > 0 {
			return rows, nil
		}
	}
	// Fall back to any tag column of the same table.
	for _, tag := range m.MetricTags {
		if tag.Symbol.OID == "" || tag.Table != "" {
			continue
		}
		if rows, err := store.Column(tag.Symbol.OID); err == nil && len(rows) > 0 {
			return rows, nil
		}
	}
	return nil, fmt.Errorf("no sibling column to enumerate rows for %q", sym.Name)
}

// emitSymbol resolves one symbol through the naming registry and appends its
// datapoints.
func (b *Builder) emitSymbol(pool *metricPool, store *snmp.ValueStore, m *profiledefinition.MetricsConfig,
	sym profiledefinition.SymbolConfig, value snmp.ResultValue, attrs map[string]string,
	component, index string, ts pcommon.Timestamp, report *BuildReport) error {

	// flag_stream produces one metric per bit, distinguished by suffix, so the
	// suffix is part of the name the registry resolves.
	lookupName := sym.Name
	if m.MetricType == profiledefinition.MetricTypeFlagStream {
		lookupName += m.Options.MetricSuffix
	}

	resolution := b.registry.Resolve(m.MIB, profiledefinition.SymbolConfig{Name: lookupName})
	entry := resolution.Entry

	// Datadog-compatible names first: they take the raw value, so they do not
	// depend on the semconv mapping (or on a sensor dispatch succeeding).
	if err := b.emitRaw(pool, m, sym, value, attrs, component, index, ts, report, resolution); err != nil {
		return err
	}
	if len(resolution.Names) == 0 {
		return nil
	}

	// A sensor column's meaning comes from a sibling column in the same row, so
	// both the target metric and its exponent are per-row.
	if entry.TypeDispatch != nil {
		dispatched, err := b.dispatchSensor(resolution.Entry.TypeDispatch, store, m, lookupName, index, report)
		if err != nil {
			return err
		}
		resolution = dispatched
		entry = dispatched.Entry
	}

	number, err := b.numericValue(m, sym, value, entry)
	if err != nil {
		report.SkippedSymbols++
		return fmt.Errorf("symbol %s: %w", sym.Name, err)
	}

	if resolution.Generated {
		for _, name := range resolution.Names {
			report.GeneratedMetrics[name] = struct{}{}
		}
	}

	instrument := entry.Instrument
	if resolution.Generated {
		// Only a curated entry is authoritative about the instrument; otherwise
		// infer it from the profile and the SNMP type.
		instrument = instrumentFor(effectiveMetricType(m, sym), value)
	}

	for _, name := range resolution.Names {
		dpAttrs := buildAttributes(entry, attrs, name, component, index, sym.Name)

		if entry.StateSet != nil {
			if err := b.emitStateSet(pool, name, entry, value, dpAttrs, ts); err != nil {
				return err
			}
			continue
		}
		if err := pool.add(name, entry.Unit, instrument, dpAttrs, number, ts, b, entry.Priority); err != nil {
			return err
		}
	}
	return nil
}

// emitRaw emits a symbol under its Datadog-compatible names with the raw SNMP
// value: the profile's own scale_factor applies, as in the Datadog agent, but
// none of the semconv entry's scale, value map or state set.
func (b *Builder) emitRaw(pool *metricPool, m *profiledefinition.MetricsConfig,
	sym profiledefinition.SymbolConfig, value snmp.ResultValue, attrs map[string]string,
	component, index string, ts pcommon.Timestamp, report *BuildReport, resolution naming.Resolution) error {

	if len(resolution.RawNames) == 0 {
		return nil
	}
	raw := naming.Entry{Scale: 1}
	number, err := b.numericValue(m, sym, value, raw)
	if err != nil {
		report.SkippedSymbols++
		return fmt.Errorf("symbol %s: %w", sym.Name, err)
	}
	instrument := instrumentFor(effectiveMetricType(m, sym), value)
	for _, name := range resolution.RawNames {
		if resolution.Generated {
			report.GeneratedMetrics[name] = struct{}{}
		}
		raw.Metric = name
		dpAttrs := buildAttributes(raw, attrs, name, component, index, sym.Name)
		if err := pool.add(name, "", instrument, dpAttrs, number, ts, b, 0); err != nil {
			return err
		}
	}
	return nil
}

// emitStateSet writes one datapoint per possible state: 1 for the active state
// and 0 for the rest, which is the StateSet representation hw.status uses.
//
// An SNMP value outside the mapping yields all-zero rather than no datapoints,
// so a device in an unexpected state reads as "no known state active" instead of
// silently dropping out of the series.
func (b *Builder) emitStateSet(pool *metricPool, name string, entry naming.Entry,
	value snmp.ResultValue, attrs map[string]string, ts pcommon.Timestamp) error {

	active := entry.StateSet.Map[value.String()]
	for _, state := range entry.StateSet.States {
		stateAttrs := make(map[string]string, len(attrs)+1)
		for k, v := range attrs {
			stateAttrs[k] = v
		}
		stateAttrs[entry.StateSet.Attribute] = state

		reading := 0.0
		if state == active {
			reading = 1
		}
		if err := pool.add(name, entry.Unit, entry.Instrument, stateAttrs, reading, ts, b, entry.Priority); err != nil {
			return err
		}
	}
	return nil
}

// dispatchSensor follows a type_dispatch to the resolution for this row's sensor
// kind, and folds the row's own exponent into the scale.
func (b *Builder) dispatchSensor(td *naming.TypeDispatch, store *snmp.ValueStore,
	m *profiledefinition.MetricsConfig, symbolName, index string,
	report *BuildReport) (naming.Resolution, error) {

	// The sibling type column is found by name among the metric's tags or
	// symbols, since the profile does not otherwise link them.
	typeValue, ok := siblingValue(store, td.TypeSymbol, m, index)
	if !ok {
		report.SkippedSymbols++
		return naming.Resolution{}, fmt.Errorf("sensor type column %q not collected for row %s", td.TypeSymbol, index)
	}
	target, ok := td.Cases[typeValue]
	if !ok {
		// A sensor kind with no hw.* home, such as percentRH. Skipping is right:
		// filing it under another metric would corrupt that metric.
		report.SkippedSymbols++
		return naming.Resolution{}, fmt.Errorf("sensor type %q has no mapping", typeValue)
	}

	resolution, ok := b.registry.ResolveDispatched(symbolName, target)
	if !ok {
		return naming.Resolution{}, fmt.Errorf("type_dispatch target %q is not a registry entry", target)
	}
	// A sensor reporting deci-celsius and one reporting whole degrees both come
	// out in Cel, without a per-device divisor.
	resolution.Entry.Scale *= sensorExponent(store, td, m, index)
	return resolution, nil
}

// sensorExponent computes the multiplier a sensor row's scale and precision
// columns imply, returning 1 when the device does not report them.
//
// ENTITY-SENSOR-MIB defines EntitySensorDataScale as an SI prefix enum where
// units(9) is 10^0 and each step is three orders of magnitude, and
// EntitySensorPrecision as the number of decimal places already folded into the
// integer value. So a reading of 334 with scale=units and precision=2 is 3.34.
func sensorExponent(store *snmp.ValueStore, td *naming.TypeDispatch,
	m *profiledefinition.MetricsConfig, index string) float64 {

	exponent := 0
	if td.ScaleSymbol != "" {
		if raw, ok := siblingValue(store, td.ScaleSymbol, m, index); ok {
			if scale, err := strconv.Atoi(raw); err == nil && scale >= 1 && scale <= 17 {
				exponent += 3 * (scale - 9)
			}
		}
	}
	if td.PrecisionSymbol != "" {
		if raw, ok := siblingValue(store, td.PrecisionSymbol, m, index); ok {
			if precision, err := strconv.Atoi(raw); err == nil {
				exponent -= precision
			}
		}
	}
	return math.Pow(10, float64(exponent))
}

// numericValue produces the number to report for a symbol.
func (b *Builder) numericValue(m *profiledefinition.MetricsConfig, sym profiledefinition.SymbolConfig,
	value snmp.ResultValue, entry naming.Entry) (float64, error) {

	if m.MetricType == profiledefinition.MetricTypeFlagStream {
		return flagValue(value, m.Options.Placement)
	}
	// A value map turns an enum into a number, as ifOperStatus up(1) becomes 1
	// and everything else 0.
	if len(entry.ValueMap) > 0 {
		if mapped, ok := entry.ValueMap[value.String()]; ok {
			return mapped, nil
		}
		if fallback, ok := entry.ValueMap["default"]; ok {
			return fallback, nil
		}
	}
	return floatValue(sym, value, entry.Scale)
}

// effectiveMetricType resolves the metric type, a symbol overriding its metric.
func effectiveMetricType(m *profiledefinition.MetricsConfig, sym profiledefinition.SymbolConfig) profiledefinition.ProfileMetricType {
	if sym.MetricType != "" {
		return sym.MetricType
	}
	return m.MetricType
}

// instrumentFor picks an instrument when the registry has no opinion.
func instrumentFor(metricType profiledefinition.ProfileMetricType, value snmp.ResultValue) naming.Instrument {
	switch metricType {
	case profiledefinition.MetricTypeMonotonicCount,
		profiledefinition.MetricTypeMonotonicCountAndRate,
		profiledefinition.MetricTypeRate:
		// Rates are not computed here: the cumulative total is emitted and the
		// pipeline derives a rate, so monotonic_count_and_rate collapses to one
		// Sum rather than two streams.
		return naming.Sum
	case profiledefinition.MetricTypeGauge, profiledefinition.MetricTypeFlagStream:
		return naming.Gauge
	default:
		// No declared type: trust the device. A counter is cumulative.
		if value.IsCounter() {
			return naming.Sum
		}
		return naming.Gauge
	}
}

// buildAttributes assembles the datapoint attributes: registry-declared ones,
// the resolved profile tags, and the hardware component identity.
func buildAttributes(entry naming.Entry, tags map[string]string,
	metricName, component, index, symbolName string) map[string]string {

	out := make(map[string]string, len(entry.Attributes)+len(tags)+2)
	for k, v := range entry.Attributes {
		out[k] = v
	}
	for k, v := range tags {
		out[k] = v
	}

	// hw.id is required on hw.* metrics and must be unique within the device.
	// Other namespaces do not carry it unless the entry opts in, so the attribute
	// keeps its meaning.
	if !strings.HasPrefix(metricName, "hw.") && !entry.ComponentIdentity {
		return out
	}
	if id := componentID(component, index, symbolName); id != "" {
		out["hw.id"] = id
	}
	name := preferredName(tags)
	if name != "" {
		out["hw.name"] = name
	}
	// A component metric outside hw.* also carries the interface name the system
	// conventions expect, so it is identifiable on its own terms.
	if name != "" && !strings.HasPrefix(metricName, "hw.") {
		out["network.interface.name"] = name
	}
	return out
}

// siblingValue reads another column of the same row, by symbol name. Used to
// resolve a sensor's kind from the column that declares it.
func siblingValue(store *snmp.ValueStore, symbolName string,
	m *profiledefinition.MetricsConfig, index string) (string, bool) {

	lookup := func(sym profiledefinition.SymbolConfig, transforms []profiledefinition.MetricIndexTransform) (string, bool) {
		if sym.Name != symbolName || sym.OID == "" {
			return "", false
		}
		target, err := transformIndex(index, transforms)
		if err != nil {
			return "", false
		}
		value, err := store.ColumnValue(sym.OID, target)
		if err != nil {
			return "", false
		}
		return value.String(), true
	}

	for _, sym := range m.Symbols {
		if v, ok := lookup(sym, nil); ok {
			return v, true
		}
	}
	for _, tag := range m.MetricTags {
		if v, ok := lookup(tag.Symbol, tag.IndexTransform); ok {
			return v, true
		}
	}
	return "", false
}

// componentID builds a per-device unique component identifier: the table's short
// name plus the row index, so interface 1 of ifTable becomes "if_1".
func componentID(component, index, symbolName string) string {
	switch {
	case component != "" && index != "":
		return component + "_" + index
	case symbolName != "":
		return naming.Snake(symbolName)
	default:
		return ""
	}
}

// preferredName picks a human-readable component name from the row's tags.
func preferredName(tags map[string]string) string {
	for _, key := range []string{"interface", "name", "sensor", "port"} {
		if v, ok := tags[key]; ok && v != "" {
			return v
		}
	}
	return ""
}

// canonicalComponents maps tables that describe the same physical components to
// one component namespace, keyed by table OID.
//
// ifTable and ifXTable are both indexed by ifIndex and describe the same
// interfaces, so metrics from either must agree on hw.id -- otherwise interface
// 1 appears as both "if_1" and "if_x_1" and no consumer can join them.
var canonicalComponents = map[string]string{
	"1.3.6.1.2.1.2.2":    "if", // ifTable
	"1.3.6.1.2.1.31.1.1": "if", // ifXTable
}

// componentPrefix derives a short component namespace from a table name:
// ifTable becomes "if", entPhySensorTable becomes "ent_phy_sensor".
func componentPrefix(tableName, tableOID string) string {
	if canonical, ok := canonicalComponents[snmp.CanonicalOID(tableOID)]; ok {
		return canonical
	}
	if tableName == "" {
		// An unnamed table falls back to its OID, with the arc separators
		// flattened so the result reads as one identifier.
		return strings.Trim(strings.ReplaceAll(snmp.CanonicalOID(tableOID), ".", "_"), "_")
	}
	trimmed := strings.TrimSuffix(tableName, "Table")
	if trimmed == "" {
		trimmed = tableName
	}
	return naming.Snake(trimmed)
}

func sortedIndexes(rows map[string]snmp.ResultValue) []string {
	out := make([]string, 0, len(rows))
	for index := range rows {
		out = append(out, index)
	}
	sort.Slice(out, func(i, j int) bool { return snmp.OIDLess(out[i], out[j]) })
	return out
}

// streamKeyFromMap identifies one metric stream, so a cumulative series keeps a
// stable start timestamp across polls and duplicate streams can be detected.
func streamKeyFromMap(name string, attrs map[string]string) string {
	pairs := make([]string, 0, len(attrs))
	for k, v := range attrs {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	return name + "|" + strings.Join(pairs, ",")
}

// startTime returns the start timestamp for a stream, recording the first
// observation. A cumulative point without a start timestamp cannot be turned
// into a rate by a backend that did not see the earlier points.
//
// A value lower than the previous point means the counter was reset (device
// reboot, counter wrap, interface re-created). OTel requires a new start time
// then; keeping the old one would make the series look like it went
// backwards, which consumers turn into negative or huge rates. The new start
// is the previous point's time: the reset happened somewhere after it. The
// Datadog agent handles the same case by dropping negative deltas.
func (b *Builder) startTime(key string, now pcommon.Timestamp, value float64, monotonic bool) pcommon.Timestamp {
	h := hashKey(key)
	b.mu.Lock()
	defer b.mu.Unlock()
	if entry, ok := b.startTimes[h]; ok {
		if monotonic && value < entry.last {
			entry.start = now
			if b.prevAt != 0 && b.prevAt < now {
				entry.start = b.prevAt
			}
		}
		entry.last, entry.seen = value, b.generation
		b.startTimes[h] = entry
		return entry.start
	}
	b.startTimes[h] = startEntry{start: now, last: value, seen: b.generation}
	return now
}

// pruneStale drops the state of streams absent for staleAfterPolls builds.
// It runs once per build; a failed poll does not build, so an outage does not
// age out a device's state.
func (b *Builder) pruneStale() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.generation <= staleAfterPolls {
		return
	}
	cutoff := b.generation - staleAfterPolls
	for k, entry := range b.startTimes {
		if entry.seen < cutoff {
			delete(b.startTimes, k)
		}
	}
	for k, sample := range b.prevCounters {
		if sample.seen < cutoff {
			delete(b.prevCounters, k)
		}
	}
}

// StateSize reports how many streams the builder keeps state for, for tests.
func (b *Builder) StateSize() (startTimes, counters int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.startTimes), len(b.prevCounters)
}
