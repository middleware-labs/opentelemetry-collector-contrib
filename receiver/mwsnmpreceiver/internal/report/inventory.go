package report

import (
	"fmt"
	"hash/fnv"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/profile"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/profiledefinition"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/snmp"
)

// Entity types of the inventory records. One record describes one item; the
// backend keeps the latest record per item (one SQL row), and history comes from
// the metrics, as in Datadog NDM.
const (
	EntityDevice    = "network.device"
	EntityInterface = "network.interface"
	EntityIPAddress = "network.ip_address"
)

// Record ID keys. They are the attribute names the metrics already carry, so
// the backend joins a record to its metrics without a mapping table: host.id is
// a resource attribute of every metric, interface_index a datapoint attribute
// of every interface metric.
const (
	AttrDeviceID       = "host.id"
	AttrInterfaceIndex = "interface_index"
	AttrIPAddress      = "ip_address"
)

// Device status values, the same numbers as the Datadog metadata payload.
const (
	DeviceStatusReachable   int64 = 1
	DeviceStatusUnreachable int64 = 2
)

// Diagnosis codes, the same as the Datadog agent's, so a UI written against
// Datadog's diagnoses works unchanged.
const (
	DiagFailedToOpenConnection = "SNMP_FAILED_TO_OPEN_CONNECTION"
	DiagFailedToPollDevice     = "SNMP_FAILED_TO_POLL_DEVICE"
	DiagFailedToDetectProfile  = "SNMP_FAILED_TO_DETECT_PROFILE"
	DiagHighCheckDuration      = "SNMP_HIGH_CHECK_DURATION"
)

// Diagnosis is one problem found while polling a device.
type Diagnosis struct {
	Severity string // "error" or "warn"
	Code     string
	Message  string
}

// KV is one record ID part. IDs are ordered so a record's key is stable.
type KV struct {
	Key   string
	Value any // string or int64
}

// Record is one inventory item: a device, an interface or an IP address.
//
// Attribute values are string, int64, bool or []Diagnosis.
type Record struct {
	EntityType string
	ID         []KV
	Attributes map[string]any
}

// Key identifies the item across polls.
func (r Record) Key() string {
	var sb strings.Builder
	sb.WriteString(r.EntityType)
	for _, kv := range r.ID {
		fmt.Fprintf(&sb, "|%s=%v", kv.Key, kv.Value)
	}
	return sb.String()
}

// Hash summarises the record's content, for change detection.
func (r Record) Hash() uint64 {
	keys := make([]string, 0, len(r.Attributes))
	for k := range r.Attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := fnv.New64a()
	_, _ = h.Write([]byte(r.Key()))
	for _, k := range keys {
		_, _ = fmt.Fprintf(h, "\x00%s=%#v", k, r.Attributes[k])
	}
	return h.Sum64()
}

// PutID writes the record ID into an entity event's ID map.
func (r Record) PutID(m pcommon.Map) {
	for _, kv := range r.ID {
		putValue(m, kv.Key, kv.Value)
	}
}

// PutAttributes writes the record attributes into an entity event's attribute
// map.
func (r Record) PutAttributes(m pcommon.Map) {
	for k, v := range r.Attributes {
		putValue(m, k, v)
	}
}

func putValue(m pcommon.Map, key string, value any) {
	switch v := value.(type) {
	case string:
		m.PutStr(key, v)
	case int64:
		m.PutInt(key, v)
	case float64:
		m.PutDouble(key, v)
	case bool:
		m.PutBool(key, v)
	case []string:
		s := m.PutEmptySlice(key)
		for _, item := range v {
			s.AppendEmpty().SetStr(item)
		}
	case []Diagnosis:
		// Always written, even when empty: an empty list is how the backend
		// learns that an earlier problem is resolved.
		s := m.PutEmptySlice(key)
		for _, d := range v {
			dm := s.AppendEmpty().SetEmptyMap()
			dm.PutStr("severity", d.Severity)
			dm.PutStr("code", d.Code)
			dm.PutStr("message", d.Message)
		}
	default:
		m.PutStr(key, fmt.Sprint(v))
	}
}

// InventoryOptions selects which sub-records are built.
type InventoryOptions struct {
	Interfaces  bool
	IPAddresses bool
	Topology    bool
	VPN         bool
}

// Inventory is what one successful poll knows about a device.
type Inventory struct {
	// Resource holds the same resource attributes as the device's metrics.
	Resource pcommon.Map
	// DeviceAttributes are the device record's attributes, without status and
	// diagnoses, which the caller adds because they depend on the poll outcome.
	DeviceAttributes map[string]any
	Interfaces       []Record
	IPAddresses      []Record
	Links            []Record
	VPNTunnels       []Record
}

// BuildInventory reads the inventory from one poll's values. It uses the same
// profile metadata section as the metric resource attributes, plus the
// metadata.interface section and ipAddrTable when asked.
func (b *Builder) BuildInventory(dev DeviceInfo, compiled *profile.Compiled, store *snmp.ValueStore, opts InventoryOptions) *Inventory {
	def := compiled.Definition
	resolver := &tagResolver{compiled: compiled, store: store}

	res := pcommon.NewResource()
	deviceTags, _ := resolver.deviceTags(def.MetricTags)
	b.setResourceAttributes(res, dev, compiled, store, deviceTags)

	inv := &Inventory{
		Resource:         res.Attributes(),
		DeviceAttributes: BaseDeviceAttributes(dev),
	}
	for field, value := range b.deviceMetadata(compiled, store) {
		switch field {
		case "type":
			// Datadog reports the profile's device type as device_type,
			// lowercased (router, switch, firewall, ...).
			inv.DeviceAttributes["device_type"] = strings.ToLower(value)
		default:
			inv.DeviceAttributes[field] = value
		}
	}

	// Topology needs the interfaces to resolve each link's local port, even
	// when interface records themselves are not sent.
	var interfaces []Record
	if opts.Interfaces || opts.Topology {
		interfaces = buildInterfaces(dev.ID, def, resolver)
	}
	if opts.Interfaces {
		inv.Interfaces = interfaces
	}
	if opts.IPAddresses {
		inv.IPAddresses = buildIPAddresses(dev.ID, store)
	}
	if opts.Topology {
		inv.Links = buildLinks(dev.ID, store, interfaces)
	}
	if opts.VPN {
		inv.VPNTunnels = buildVPNTunnels(dev.ID, store)
	}
	return inv
}

// BaseDeviceAttributes are the device attributes known without polling: what
// discovery and configuration say. A failed poll reports these when no earlier
// poll succeeded.
func BaseDeviceAttributes(dev DeviceInfo) map[string]any {
	attrs := map[string]any{"integration": "snmp", AttrIPAddress: dev.Address}
	if dev.Port != 0 {
		attrs["port"] = int64(dev.Port)
	}
	for k, v := range map[string]string{
		"namespace":     dev.Namespace,
		"subnet":        dev.Subnet,
		"sys_object_id": dev.SysObjectID,
		"profile":       dev.ProfileName,
	} {
		if v != "" {
			attrs[k] = v
		}
	}
	return attrs
}

// DeviceRecord assembles the device record.
func DeviceRecord(deviceID string, attrs map[string]any, status int64, diagnoses []Diagnosis) Record {
	out := make(map[string]any, len(attrs)+2)
	for k, v := range attrs {
		out[k] = v
	}
	out["status"] = status
	if diagnoses == nil {
		diagnoses = []Diagnosis{}
	}
	out["diagnoses"] = diagnoses
	return Record{
		EntityType: EntityDevice,
		ID:         []KV{{Key: AttrDeviceID, Value: deviceID}},
		Attributes: out,
	}
}

// physicalIfTypes are the ifType values Datadog counts as physical ethernet
// ports: ethernetCsmacd(6), fastEther(62), fastEtherFX(69), gigabitEthernet(117).
var physicalIfTypes = map[int64]bool{6: true, 62: true, 69: true, 117: true}

// buildInterfaces makes one record per row of the metadata.interface columns.
func buildInterfaces(deviceID string, def *profiledefinition.ProfileDefinition, resolver *tagResolver) []Record {
	section, ok := def.Metadata["interface"]
	if !ok {
		return nil
	}

	// The rows are the union of every field column's indexes: a device that
	// omits ifAlias for some ports still reports those ports.
	indexes := map[string]struct{}{}
	for _, field := range section.Fields {
		for _, sym := range fieldSymbols(field) {
			rows, err := resolver.store.Column(sym.OID)
			if err != nil {
				continue
			}
			for index := range rows {
				indexes[index] = struct{}{}
			}
		}
	}

	records := make([]Record, 0, len(indexes))
	for _, index := range sortedKeys(indexes) {
		attrs := map[string]any{}
		for name, field := range section.Fields {
			if field.Value != "" {
				attrs[name] = field.Value
				continue
			}
			for _, sym := range fieldSymbols(field) {
				value, err := resolver.store.ColumnValue(sym.OID, index)
				if err != nil {
					continue
				}
				if v, ok := inventoryValue(resolver.compiled, sym, value); ok {
					attrs[name] = v
					break
				}
			}
		}
		if len(attrs) == 0 {
			continue
		}
		// The id_tags are the tags interface metrics carry (interface:<ifName>),
		// so they are copied onto the record for joining by name as well.
		if tags, _ := resolver.rowTags(section.IDTags, index); len(tags) > 0 {
			for k, v := range tags {
				if _, exists := attrs[k]; !exists {
					attrs[k] = v
				}
			}
		}
		if ifType, ok := attrs["type"].(int64); ok && ifType != 0 {
			attrs["is_physical"] = physicalIfTypes[ifType]
		}

		records = append(records, Record{
			EntityType: EntityInterface,
			ID:         []KV{{Key: AttrDeviceID, Value: deviceID}, {Key: AttrInterfaceIndex, Value: indexValue(index)}},
			Attributes: attrs,
		})
	}
	return records
}

// buildIPAddresses makes one record per ipAddrTable row. The row index is the
// address itself.
func buildIPAddresses(deviceID string, store *snmp.ValueStore) []Record {
	ifIndexes, err := store.Column(profile.OIDIPAdEntIfIndex)
	if err != nil {
		return nil
	}
	masks, _ := store.Column(profile.OIDIPAdEntNetMask)

	records := make([]Record, 0, len(ifIndexes))
	for _, address := range sortedKeys(ifIndexes) {
		if net.ParseIP(address) == nil {
			continue
		}
		attrs := map[string]any{}
		if mask, ok := masks[address]; ok {
			if prefix, ok := prefixLength(mask.String()); ok {
				attrs["prefixlen"] = int64(prefix)
			}
		}
		records = append(records, Record{
			EntityType: EntityIPAddress,
			ID: []KV{
				{Key: AttrDeviceID, Value: deviceID},
				{Key: AttrInterfaceIndex, Value: indexValue(ifIndexes[address].String())},
				{Key: AttrIPAddress, Value: address},
			},
			Attributes: attrs,
		})
	}
	return records
}

func prefixLength(mask string) (int, bool) {
	ip := net.ParseIP(mask).To4()
	if ip == nil {
		return 0, false
	}
	ones, bits := net.IPMask(ip).Size()
	if bits == 0 {
		// Not a contiguous mask.
		return 0, false
	}
	return ones, true
}

func fieldSymbols(field profiledefinition.MetadataField) []profiledefinition.SymbolConfig {
	var out []profiledefinition.SymbolConfig
	if field.Symbol.OID != "" {
		out = append(out, field.Symbol)
	}
	for _, sym := range field.Symbols {
		if sym.OID != "" {
			out = append(out, sym)
		}
	}
	return out
}

// inventoryValue shapes one metadata value. Numbers stay numbers (ifType,
// ifAdminStatus and ifOperStatus are enums the backend stores as integers);
// text goes through the profile's format and extract_value.
func inventoryValue(compiled *profile.Compiled, sym profiledefinition.SymbolConfig, value snmp.ResultValue) (any, bool) {
	if f, ok := value.Value.(float64); ok && sym.Format == "" && sym.ExtractValue == "" {
		if f == math.Trunc(f) && math.Abs(f) < 1e15 {
			return int64(f), true
		}
		return f, true
	}
	text, err := stringValue(compiled, sym, value)
	if err != nil || text == "" {
		return nil, false
	}
	return text, true
}

// indexValue returns a numeric row index as an int64, as Datadog sends it. The
// interface metrics carry the same digits as a string attribute.
func indexValue(index string) any {
	if n, err := strconv.ParseInt(index, 10, 64); err == nil {
		return n
	}
	return index
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return snmp.OIDLess(out[i], out[j]) })
	return out
}
