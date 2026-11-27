package mwsnmpreceiver

import (
	"context"
	"hash/maphash"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/experimentalmetricmetadata"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/discovery"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/profile"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/report"
)

// The inventory flow.
//
// When the receiver is in a logs pipeline, each poll also produces inventory
// records -- one per device, interface, IP address, LLDP/CDP link and IPsec
// tunnel -- sent as OpenTelemetry
// entity events (log records with otel.entity.* attributes, the format
// k8sclusterreceiver uses for Kubernetes objects). This is the equivalent of the
// Datadog agent's NDM metadata payload: the backend upserts one row per item and
// joins it to the metrics on host.id (+ interface_index).
//
// Unlike Datadog, which resends everything every check run, only changed records
// are sent between full resends (metadata.resend_interval). Each device's
// records go out in their own ConsumeLogs call, and polls are spread across the
// collection interval, so there is no startup burst of every device at once.

// checkDurationThreshold matches the Datadog agent's: a poll slower than this
// gets a SNMP_HIGH_CHECK_DURATION warning.
const checkDurationThreshold = 30 * time.Second

// Diagnosis messages are fixed text, not error strings: a message that changed
// on every poll would make the device record look changed and be resent.
var (
	diagOpenConnection = report.Diagnosis{
		Severity: "error", Code: report.DiagFailedToOpenConnection,
		Message: "Collector failed to open an SNMP connection to this device.",
	}
	diagPollDevice = report.Diagnosis{
		Severity: "error", Code: report.DiagFailedToPollDevice,
		Message: "Collector failed to poll this network device. Check the authentication method and ensure the collector can reach it.",
	}
	diagDetectProfile = report.Diagnosis{
		Severity: "error", Code: report.DiagFailedToDetectProfile,
		Message: "Collector failed to detect a profile for this network device.",
	}
)

func diagHighDuration() report.Diagnosis {
	return report.Diagnosis{
		Severity: "warn", Code: report.DiagHighCheckDuration,
		Message: "Poll duration is high for this device.",
	}
}

// inventoryTracker remembers what was last sent for one device: a hash of each
// record's key and of its content, 16 bytes per record plus map overhead.
type inventoryTracker struct {
	sent     map[uint64]uint64
	lastFull time.Time
}

var trackerSeed = maphash.MakeSeed()

func recordKeyHash(rec report.Record) uint64 { return maphash.String(trackerSeed, rec.Key()) }

// pending returns the records to send now: all of them on a full resend,
// otherwise only new or changed ones.
func (t *inventoryTracker) pending(records []report.Record, now time.Time, resend time.Duration) ([]report.Record, bool) {
	full := resend == 0 || t.lastFull.IsZero() || now.Sub(t.lastFull) >= resend
	if full {
		return records, true
	}
	var out []report.Record
	for _, rec := range records {
		if prev, ok := t.sent[recordKeyHash(rec)]; !ok || prev != rec.Hash() {
			out = append(out, rec)
		}
	}
	return out, false
}

// commit records a successful send. A full send also forgets items that are
// gone, so one that comes back is sent again.
func (t *inventoryTracker) commit(sent []report.Record, full bool, now time.Time) {
	if t.sent == nil || full {
		t.sent = make(map[uint64]uint64, len(sent))
	}
	for _, rec := range sent {
		t.sent[recordKeyHash(rec)] = rec.Hash()
	}
	if full {
		t.lastFull = now
	}
}

func (r *snmpReceiver) inventoryEnabled() bool { return r.logsConsumer != nil }

// compileOptions adds the inventory columns to each poll only when something
// consumes them.
func (r *snmpReceiver) compileOptions() profile.CompileOptions {
	if !r.inventoryEnabled() {
		return profile.CompileOptions{}
	}
	return profile.CompileOptions{
		InterfaceMetadata: r.cfg.Metadata.Interfaces,
		IPAddresses:       r.cfg.Metadata.IPAddresses,
		Topology:          r.cfg.Metadata.Topology,
		VPN:               r.cfg.Metadata.VPN,
	}
}

func (r *snmpReceiver) inventoryOptions() report.InventoryOptions {
	return report.InventoryOptions{
		Interfaces:  r.cfg.Metadata.Interfaces,
		IPAddresses: r.cfg.Metadata.IPAddresses,
		Topology:    r.cfg.Metadata.Topology,
		VPN:         r.cfg.Metadata.VPN,
	}
}

// reportInventory sends the records of a successful poll.
func (r *snmpReceiver) reportInventory(ctx context.Context, state *deviceState, inv *report.Inventory,
	pollDuration time.Duration, now time.Time) {

	var diagnoses []report.Diagnosis
	if pollDuration > checkDurationThreshold {
		diagnoses = append(diagnoses, diagHighDuration())
	}

	// Kept so a later failed poll can still send a complete device record.
	state.lastDeviceAttrs = inv.DeviceAttributes
	state.lastResource = pcommon.NewMap()
	inv.Resource.CopyTo(state.lastResource)

	records := make([]report.Record, 0,
		1+len(inv.Interfaces)+len(inv.IPAddresses)+len(inv.Links)+len(inv.VPNTunnels))
	records = append(records, report.DeviceRecord(state.deviceID, inv.DeviceAttributes, report.DeviceStatusReachable, diagnoses))
	records = append(records, inv.Interfaces...)
	records = append(records, inv.IPAddresses...)
	records = append(records, inv.Links...)
	records = append(records, inv.VPNTunnels...)

	r.sendRecords(ctx, state, inv.Resource, records, now)
}

// reportFailure sends the device record of a failed poll, with its status and
// diagnosis. Interface and IP records are left as last sent; the backend ages
// them out if they stop arriving.
func (r *snmpReceiver) reportFailure(ctx context.Context, device discovery.Device, diag report.Diagnosis, status int64) {
	state := r.stateFor(device)

	attrs := state.lastDeviceAttrs
	resource := state.lastResource
	if attrs == nil {
		info := r.deviceInfo(device, state.profileName)
		attrs = report.BaseDeviceAttributes(info)
		resource = minimalResource(info)
	}
	record := report.DeviceRecord(state.deviceID, attrs, status, []report.Diagnosis{diag})
	r.sendRecords(ctx, state, resource, []report.Record{record}, time.Now())
}

// reportDeleted tells the backend a device is gone, after it ran out of its
// failure budget.
func (r *snmpReceiver) reportDeleted(ctx context.Context, device discovery.Device, resource pcommon.Map) {
	events := experimentalmetricmetadata.NewEntityEventsSlice()
	event := events.AppendEmpty()
	event.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	info := r.deviceInfo(device, "")
	event.ID().PutStr(report.AttrDeviceID, info.ID)
	event.SetEntityDelete().SetEntityType(report.EntityDevice)

	if resource == (pcommon.Map{}) {
		resource = minimalResource(info)
	}
	r.consumeEvents(ctx, device.Address, events, resource)
}

// sendRecords sends what changed and remembers it on success.
func (r *snmpReceiver) sendRecords(ctx context.Context, state *deviceState, resource pcommon.Map,
	records []report.Record, now time.Time) {

	toSend, full := state.inventory.pending(records, now, r.cfg.Metadata.ResendInterval)
	if len(toSend) == 0 {
		return
	}

	events := experimentalmetricmetadata.NewEntityEventsSlice()
	events.EnsureCapacity(len(toSend))
	ts := pcommon.NewTimestampFromTime(now)
	for _, rec := range toSend {
		event := events.AppendEmpty()
		event.SetTimestamp(ts)
		rec.PutID(event.ID())
		details := event.SetEntityState()
		details.SetEntityType(rec.EntityType)
		if r.cfg.Metadata.ResendInterval > 0 {
			details.SetInterval(r.cfg.Metadata.ResendInterval)
		}
		rec.PutAttributes(details.Attributes())
	}

	if r.consumeEvents(ctx, state.deviceID, events, resource) {
		state.inventory.commit(toSend, full, now)
	}
}

func (r *snmpReceiver) consumeEvents(ctx context.Context, device string,
	events experimentalmetricmetadata.EntityEventsSlice, resource pcommon.Map) bool {

	count := events.Len()
	logs := events.ConvertAndMoveToLogs()
	rl := logs.ResourceLogs().At(0)
	resource.CopyTo(rl.Resource().Attributes())
	rl.ScopeLogs().At(0).Scope().SetName(report.ScopeName)

	if err := r.logsConsumer.ConsumeLogs(ctx, logs); err != nil {
		// Not counted as a failed poll: the records are resent next poll
		// because they were not committed.
		r.logger.Error("pipeline rejected inventory records",
			zap.String("device", device), zap.Int("records", count), zap.Error(err))
		return false
	}
	r.telemetry.inventoryRecords.Add(ctx, int64(count))
	return true
}

// deviceInfo describes a device for reporting. Its ID is the exported device
// ID, <namespace>:<ip> as in Datadog NDM, and becomes host.id on the metrics
// and the inventory records. The registry keeps its own internal ID.
func (r *snmpReceiver) deviceInfo(device discovery.Device, profileName string) report.DeviceInfo {
	if profileName == "" {
		profileName = device.ProfileName
	}
	ns := r.namespaceFor(device)
	return report.DeviceInfo{
		ID:          exportedDeviceID(ns, device.Address),
		Namespace:   ns,
		HostName:    r.cfg.HostName,
		Address:     device.Address,
		Port:        device.Port,
		Subnet:      device.Subnet,
		SysObjectID: device.SysObjectID,
		ProfileName: profileName,
	}
}

// minimalResource identifies a device that has never been polled successfully.
// In sys_name mode host.name is left out: sysName is unknown yet, and the
// address would create a second host on the backend.
func minimalResource(info report.DeviceInfo) pcommon.Map {
	m := pcommon.NewMap()
	m.PutStr(report.AttrDeviceID, info.ID)
	m.PutStr("snmp.device.ip", info.Address)
	if info.Subnet != "" {
		m.PutStr("snmp.subnet", info.Subnet)
	}
	if info.ProfileName != "" {
		m.PutStr("snmp.profile", info.ProfileName)
	}
	if info.HostName == report.HostNameDeviceID {
		m.PutStr("host.name", "device:"+info.ID)
	}
	return m
}

func exportedDeviceID(namespace, address string) string {
	return namespace + ":" + address
}

// namespaceFor resolves a device's namespace: the device's or subnet's own,
// else the receiver's.
func (r *snmpReceiver) namespaceFor(device discovery.Device) string {
	ns := ""
	if device.Static {
		for _, configured := range r.cfg.Devices {
			host, _, err := parseEndpoint(configured.Endpoint)
			if err == nil && host == device.Address {
				ns = configured.Namespace
				break
			}
		}
	} else {
		for _, subnet := range r.cfg.Subnets {
			if subnet.Network == device.Subnet {
				ns = subnet.Namespace
				break
			}
		}
	}
	if ns == "" {
		ns = r.cfg.Namespace
	}
	if ns == "" {
		ns = defaultNamespace
	}
	return ns
}
