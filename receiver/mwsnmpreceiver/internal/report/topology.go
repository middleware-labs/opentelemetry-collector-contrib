package report

import (
	"net"
	"strconv"
	"strings"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/profile"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/snmp"
)

// Topology links (LLDP, with CDP as the fallback), ported from the Datadog
// agent's buildNetworkTopologyMetadata in
// pkg/collector/corechecks/snmp/internal/report/report_device_metadata.go
// (Apache-2.0). One record per neighbour seen on a local port.

// EntityLink is the entity type of a topology link record.
const EntityLink = "network.link"

// AttrLinkID is the link's ID within its device: lldpRemLocalPortNum.
// lldpRemIndex for LLDP, cdpCacheIfIndex.cdpCacheDeviceIndex for CDP. With
// host.id it matches Datadog's link id (<device_id>:<local>.<remote>).
const AttrLinkID = "link_id"

const (
	linkSourceLLDP = "lldp"
	linkSourceCDP  = "cdp"

	idTypeMACAddress    = "mac_address"
	idTypeInterfaceName = "interface_name"
)

// lldpChassisIDSubtypes and lldpPortIDSubtypes translate LldpChassisIdSubtype
// and LldpPortIdSubtype into the names Datadog uses.
var lldpChassisIDSubtypes = map[string]string{
	"1": "chassis_component",
	"2": "interface_alias",
	"3": "port_component",
	"4": "mac_address",
	"5": "network_address",
	"6": "interface_name",
	"7": "local",
}

var lldpPortIDSubtypes = map[string]string{
	"1": "interface_alias",
	"2": "port_component",
	"3": "mac_address",
	"4": "network_address",
	"5": "interface_name",
	"6": "agent_circuit_id",
	"7": "local",
}

// CDP management address types (CiscoNetworkProtocol).
const (
	ciscoNetworkProtocolIPv4 = "1"
	ciscoNetworkProtocolIPv6 = "20"
)

func buildLinks(deviceID string, store *snmp.ValueStore, interfaces []Record) []Record {
	links := buildLLDPLinks(deviceID, store, interfaces)
	if len(links) == 0 {
		links = buildCDPLinks(deviceID, store)
	}
	return links
}

func buildLLDPLinks(deviceID string, store *snmp.ValueStore, interfaces []Record) []Record {
	// lldpRemPortId is used to enumerate neighbours since every entry has one.
	remotes, err := store.Column(profile.OIDLldpRemPortID)
	if err != nil || len(remotes) == 0 {
		return nil
	}
	byIDType := interfaceCandidatesByIDType(interfaces)
	managementIPs := lldpManagementIPs(store)

	var links []Record
	for _, index := range sortedKeys(remotes) {
		// lldpRemTimeMark.lldpRemLocalPortNum.lldpRemIndex. The time mark is
		// left out of the ID because it changes often.
		parts := strings.Split(index, ".")
		if len(parts) != 3 {
			continue
		}
		localPortNum, remIndex := parts[1], parts[2]
		linkID := localPortNum + "." + remIndex

		remoteDeviceIDType := lldpChassisIDSubtypes[columnString(store, profile.OIDLldpRemChassisIDSubtype, index)]
		remoteIfIDType := lldpPortIDSubtypes[columnString(store, profile.OIDLldpRemPortIDSubtype, index)]
		localIfIDType := lldpPortIDSubtypes[columnString(store, profile.OIDLldpLocPortIDSubtype, localPortNum)]
		localIfID := formatLinkID(store, localIfIDType, profile.OIDLldpLocPortID, localPortNum)

		attrs := map[string]any{
			"source_type":                  linkSourceLLDP,
			"integration":                  "snmp",
			"local.interface.id":           localIfID,
			"local.interface.id_type":      localIfIDType,
			"remote.device.id":             formatLinkID(store, remoteDeviceIDType, profile.OIDLldpRemChassisID, index),
			"remote.device.id_type":        remoteDeviceIDType,
			"remote.device.name":           columnString(store, profile.OIDLldpRemSysName, index),
			"remote.device.description":    columnString(store, profile.OIDLldpRemSysDesc, index),
			"remote.device.ip_address":     managementIPs[linkID],
			"remote.interface.id":          formatLinkID(store, remoteIfIDType, profile.OIDLldpRemPortID, index),
			"remote.interface.id_type":     remoteIfIDType,
			"remote.interface.description": columnString(store, profile.OIDLldpRemPortDesc, index),
		}
		if ifIndex, ok := resolveLocalInterface(byIDType, localIfIDType, localIfID); ok {
			attrs["local."+AttrInterfaceIndex] = ifIndex
		}
		links = append(links, linkRecord(deviceID, linkID, attrs))
	}
	return links
}

func buildCDPLinks(deviceID string, store *snmp.ValueStore) []Record {
	remotes, err := store.Column(profile.OIDCdpCacheDevicePort)
	if err != nil || len(remotes) == 0 {
		return nil
	}

	var links []Record
	for _, index := range sortedKeys(remotes) {
		// cdpCacheIfIndex.cdpCacheDeviceIndex: CDP names the local port by
		// ifIndex directly, so no resolution is needed.
		parts := strings.Split(index, ".")
		if len(parts) != 2 {
			continue
		}
		attrs := map[string]any{
			"source_type":               linkSourceCDP,
			"integration":               "snmp",
			"remote.device.id":          columnString(store, profile.OIDCdpCacheDeviceID, index),
			"remote.device.name":        columnString(store, profile.OIDCdpCacheSysName, index),
			"remote.device.description": columnString(store, profile.OIDCdpCacheVersion, index),
			"remote.device.ip_address":  cdpRemoteAddress(store, index),
			"remote.interface.id":       columnString(store, profile.OIDCdpCacheDevicePort, index),
			"remote.interface.id_type":  idTypeInterfaceName,
		}
		if ifIndex, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
			attrs["local."+AttrInterfaceIndex] = ifIndex
		}
		links = append(links, linkRecord(deviceID, index, attrs))
	}
	return links
}

func linkRecord(deviceID, linkID string, attrs map[string]any) Record {
	for k, v := range attrs {
		if s, ok := v.(string); ok && s == "" {
			delete(attrs, k)
		}
	}
	return Record{
		EntityType: EntityLink,
		ID:         []KV{{Key: AttrDeviceID, Value: deviceID}, {Key: AttrLinkID, Value: linkID}},
		Attributes: attrs,
	}
}

// lldpManagementIPs maps lldpRemLocalPortNum.lldpRemIndex to the neighbour's
// IPv4 management address, which lldpRemManAddrTable carries in its index:
// timeMark.localPortNum.remIndex.addrSubtype.<length>.<address octets>.
func lldpManagementIPs(store *snmp.ValueStore) map[string]string {
	out := map[string]string{}
	rows, err := store.Column(profile.OIDLldpRemManAddrIfSubtype)
	if err != nil {
		return out
	}
	for index := range rows {
		parts := strings.Split(index, ".")
		if len(parts) < 9 {
			continue
		}
		// Only IPv4 (subtype 1, length 4), like the Datadog agent.
		if parts[3] == "1" && parts[4] == "4" {
			out[parts[1]+"."+parts[2]] = strings.Join(parts[5:9], ".")
		}
	}
	return out
}

func cdpRemoteAddress(store *snmp.ValueStore, index string) string {
	pairs := [][2]string{
		{profile.OIDCdpCachePrimaryMgmtAddrType, profile.OIDCdpCachePrimaryMgmtAddr},
		{profile.OIDCdpCacheSecondaryMgmtAddrType, profile.OIDCdpCacheSecondaryMgmtAddr},
		{profile.OIDCdpCacheAddressType, profile.OIDCdpCacheAddress},
	}
	for _, pair := range pairs {
		addrType := columnString(store, pair[0], index)
		if addrType != ciscoNetworkProtocolIPv4 && addrType != ciscoNetworkProtocolIPv6 {
			continue
		}
		if value, err := store.ColumnValue(pair[1], index); err == nil {
			if ip := ipString(value); ip != "" {
				return ip
			}
		}
	}
	return ""
}

// interfaceCandidate is a local interface an LLDP port ID may refer to.
type interfaceCandidate struct {
	ifIndex    int64
	isPhysical bool
	macAddress string
}

func interfaceCandidatesByIDType(interfaces []Record) map[string]map[string][]interfaceCandidate {
	out := map[string]map[string][]interfaceCandidate{}
	for _, idType := range []string{"mac_address", "interface_name", "interface_alias", "interface_index"} {
		out[idType] = map[string][]interfaceCandidate{}
	}
	for _, rec := range interfaces {
		ifIndex, ok := rec.ID[len(rec.ID)-1].Value.(int64)
		if !ok {
			continue
		}
		mac, _ := rec.Attributes["mac_address"].(string)
		name, _ := rec.Attributes["name"].(string)
		alias, _ := rec.Attributes["alias"].(string)
		physical, _ := rec.Attributes["is_physical"].(bool)
		c := interfaceCandidate{ifIndex: ifIndex, isPhysical: physical, macAddress: mac}

		out["mac_address"][mac] = append(out["mac_address"][mac], c)
		out["interface_name"][name] = append(out["interface_name"][name], c)
		out["interface_alias"][alias] = append(out["interface_alias"][alias], c)
		// Not an LLDP type: used when the device gives no subtype.
		idx := strconv.FormatInt(ifIndex, 10)
		out["interface_index"][idx] = append(out["interface_index"][idx], c)
	}
	return out
}

// resolveLocalInterface finds the ifIndex an LLDP local port ID refers to,
// the same way as the Datadog agent: by the declared subtype, or, when the
// device gives none (common), by trying MAC, name, alias and index. Exactly one
// match wins; several matches win only if they share a MAC and exactly one is
// physical (LLDP frames leave from the physical port).
func resolveLocalInterface(byIDType map[string]map[string][]interfaceCandidate, idType, id string) (int64, bool) {
	if id == "" {
		return 0, false
	}
	types := []string{idType}
	if idType == "" {
		types = []string{"mac_address", "interface_name", "interface_alias", "interface_index"}
	}
	matched := map[int64]interfaceCandidate{}
	for _, t := range types {
		for _, c := range byIDType[t][id] {
			matched[c.ifIndex] = c
		}
	}
	switch len(matched) {
	case 0:
		return 0, false
	case 1:
		for ifIndex := range matched {
			return ifIndex, true
		}
	}
	if c, ok := singlePhysicalSharingMAC(matched); ok {
		return c.ifIndex, true
	}
	return 0, false
}

func singlePhysicalSharingMAC(candidates map[int64]interfaceCandidate) (interfaceCandidate, bool) {
	var found interfaceCandidate
	physical := 0
	shared := ""
	for _, c := range candidates {
		if c.macAddress == "" {
			return interfaceCandidate{}, false
		}
		if shared == "" {
			shared = c.macAddress
		} else if c.macAddress != shared {
			return interfaceCandidate{}, false
		}
		if c.isPhysical {
			found = c
			physical++
		}
	}
	return found, physical == 1
}

// formatLinkID renders an LLDP ID: colon-separated hex for a MAC, text
// otherwise.
func formatLinkID(store *snmp.ValueStore, idType, oid, index string) string {
	value, err := store.ColumnValue(oid, index)
	if err != nil {
		return ""
	}
	if idType == idTypeMACAddress {
		if raw, ok := value.Bytes(); ok {
			return formatMAC(raw)
		}
	}
	return sanitizeText(value.String())
}

func columnString(store *snmp.ValueStore, oid, index string) string {
	value, err := store.ColumnValue(oid, index)
	if err != nil {
		return ""
	}
	return sanitizeText(value.String())
}

// ipString renders an address that may come as an IpAddress (text) or as
// packed octets in an OCTET STRING.
func ipString(value snmp.ResultValue) string {
	if raw, ok := value.Bytes(); ok {
		if len(raw) != net.IPv4len && len(raw) != net.IPv6len {
			return ""
		}
		return net.IP(raw).String()
	}
	if s := value.String(); net.ParseIP(s) != nil {
		return s
	}
	return ""
}
