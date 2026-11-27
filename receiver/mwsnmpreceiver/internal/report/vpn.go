package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/profile"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/snmp"
)

// Cisco IPsec VPN tunnels, ported from the Datadog agent's
// buildVPNTunnelsMetadata in pkg/collector/corechecks/snmp/internal/report/
// report_device_metadata.go and vpn_tunnels.go (Apache-2.0).
//
// One record per tunnel from cipSecTunnelTable. Its routed subnets come from
// the route tables: a route whose next hop is the tunnel's remote address, or,
// failing that, a route out of the tunnel's interface (found via TUNNEL-MIB).

// EntityVPNTunnel is the entity type of a VPN tunnel record.
const EntityVPNTunnel = "network.vpn_tunnel"

// VPN tunnel record ID keys, the tunnel's two outside addresses.
const (
	AttrLocalOutsideIP  = "local_outside_ip"
	AttrRemoteOutsideIP = "remote_outside_ip"
)

var ciscoIPsecStatus = map[string]string{"1": "active", "2": "destroy"}

// InetAddressType values.
const (
	inetAddressUnknown = "0"
	inetAddressIPv4    = "1"
)

type vpnTunnel struct {
	local, remote string
	status        string
	lifeSize      int64
	lifeTime      int64
	ifIndex       string
	nextHopRoutes []string
}

type deviceRoute struct {
	destination string
	prefixLen   int
	nextHop     string
	ifIndex     string
}

func (r deviceRoute) cidr() string { return fmt.Sprintf("%s/%d", r.destination, r.prefixLen) }

func buildVPNTunnels(deviceID string, store *snmp.ValueStore) []Record {
	locals, err := store.Column(profile.OIDCipSecTunLocalAddr)
	if err != nil || len(locals) == 0 {
		return nil
	}

	byOutside := map[string]*vpnTunnel{}
	byRemote := map[string][]*vpnTunnel{}
	for _, index := range sortedKeys(locals) {
		if strings.Contains(index, ".") {
			// cipSecTunnelEntry is indexed by cipSecTunIndex alone.
			continue
		}
		t := &vpnTunnel{
			local:    ipString(locals[index]),
			remote:   columnIP(store, profile.OIDCipSecTunRemoteAddr, index),
			status:   "unknown",
			lifeSize: columnInt(store, profile.OIDCipSecTunLifeSize, index),
			lifeTime: columnInt(store, profile.OIDCipSecTunLifeTime, index),
		}
		if s, ok := ciscoIPsecStatus[columnString(store, profile.OIDCipSecTunStatus, index)]; ok {
			t.status = s
		}
		byOutside[t.local+"|"+t.remote] = t
		byRemote[t.remote] = append(byRemote[t.remote], t)
	}

	routes := deviceRoutes(store)
	routesByIfIndex := map[string][]deviceRoute{}
	for _, route := range routes {
		routesByIfIndex[route.ifIndex] = append(routesByIfIndex[route.ifIndex], route)
		for _, t := range byRemote[route.nextHop] {
			t.nextHopRoutes = append(t.nextHopRoutes, route.cidr())
		}
	}

	for _, tun := range deviceTunnels(store) {
		if t, ok := byOutside[tun.local+"|"+tun.remote]; ok {
			t.ifIndex = tun.ifIndex
		}
	}

	records := make([]Record, 0, len(byOutside))
	for _, key := range sortedStrings(byOutside) {
		t := byOutside[key]
		routeSet := map[string]struct{}{}
		for _, r := range t.nextHopRoutes {
			routeSet[r] = struct{}{}
		}
		// Like the Datadog agent, fall back to the routes out of the tunnel's
		// interface when no route names the tunnel peer as its next hop.
		if len(routeSet) == 0 && t.ifIndex != "" {
			for _, r := range routesByIfIndex[t.ifIndex] {
				routeSet[r.cidr()] = struct{}{}
			}
		}
		routeList := make([]string, 0, len(routeSet))
		for r := range routeSet {
			routeList = append(routeList, r)
		}
		sort.Strings(routeList)

		attrs := map[string]any{
			"integration":     "snmp",
			"protocol":        "ipsec",
			"status":          t.status,
			"route_addresses": routeList,
			"life_size":       t.lifeSize,
			"life_time":       t.lifeTime,
		}
		if t.ifIndex != "" {
			attrs[AttrInterfaceIndex] = indexValue(t.ifIndex)
		}
		records = append(records, Record{
			EntityType: EntityVPNTunnel,
			ID: []KV{
				{Key: AttrDeviceID, Value: deviceID},
				{Key: AttrLocalOutsideIP, Value: t.local},
				{Key: AttrRemoteOutsideIP, Value: t.remote},
			},
			Attributes: attrs,
		})
	}
	return records
}

// deviceRoutes reads the active IPv4 routes from ipCidrRouteTable (old) and
// inetCidrRouteTable (new). Both carry the route in the row index.
func deviceRoutes(store *snmp.ValueStore) []deviceRoute {
	seen := map[deviceRoute]struct{}{}
	var out []deviceRoute
	add := func(r deviceRoute) {
		if _, dup := seen[r]; dup {
			return
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}

	// ipCidrRouteDest(4).ipCidrRouteMask(4).ipCidrRouteTos(1).ipCidrRouteNextHop(4)
	if rows, err := store.Column(profile.OIDIPCidrRouteIfIndex); err == nil {
		for _, index := range sortedKeys(rows) {
			if columnString(store, profile.OIDIPCidrRouteStatus, index) != "1" {
				continue
			}
			p := strings.Split(index, ".")
			if len(p) != 13 {
				continue
			}
			prefix, _ := prefixLength(strings.Join(p[4:8], "."))
			add(deviceRoute{
				destination: strings.Join(p[0:4], "."),
				prefixLen:   prefix,
				nextHop:     strings.Join(p[9:13], "."),
				ifIndex:     rows[index].String(),
			})
		}
	}

	// destType.destLen.dest....prefixLen.policyLen.policy....nextHopType.nextHopLen.nextHop...
	if rows, err := store.Column(profile.OIDInetCidrRouteIfIndex); err == nil {
		for _, index := range sortedKeys(rows) {
			if columnString(store, profile.OIDInetCidrRouteStatus, index) != "1" {
				continue
			}
			if r, ok := parseInetCidrRouteIndex(index); ok {
				r.ifIndex = rows[index].String()
				add(r)
			}
		}
	}
	return out
}

func parseInetCidrRouteIndex(index string) (deviceRoute, bool) {
	p := strings.Split(index, ".")
	pos := 0
	next := func(n int) ([]string, bool) {
		if n < 0 || pos+n > len(p) {
			return nil, false
		}
		out := p[pos : pos+n]
		pos += n
		return out, true
	}
	nextInt := func() (int, bool) {
		v, ok := next(1)
		if !ok {
			return 0, false
		}
		n, err := strconv.Atoi(v[0])
		return n, err == nil
	}

	destType, ok := next(1)
	if !ok || destType[0] != inetAddressIPv4 {
		return deviceRoute{}, false
	}
	destLen, ok := nextInt()
	if !ok {
		return deviceRoute{}, false
	}
	dest, ok := next(destLen)
	if !ok {
		return deviceRoute{}, false
	}
	prefix, ok := nextInt()
	if !ok {
		return deviceRoute{}, false
	}
	policyLen, ok := nextInt()
	if !ok {
		return deviceRoute{}, false
	}
	if _, ok := next(policyLen); !ok {
		return deviceRoute{}, false
	}
	hopType, ok := next(1)
	if !ok || (hopType[0] != inetAddressUnknown && hopType[0] != inetAddressIPv4) {
		return deviceRoute{}, false
	}
	hopLen, ok := nextInt()
	if !ok {
		return deviceRoute{}, false
	}
	hop, ok := next(hopLen)
	if !ok {
		return deviceRoute{}, false
	}
	nextHop := "0.0.0.0"
	if hopLen != 0 {
		nextHop = strings.Join(hop, ".")
	}
	return deviceRoute{destination: strings.Join(dest, "."), prefixLen: prefix, nextHop: nextHop}, true
}

type deviceTunnel struct {
	local, remote, ifIndex string
}

// deviceTunnels reads TUNNEL-MIB tunnelConfigTable (old) and
// tunnelInetConfigTable (new), which map a tunnel's endpoints to its ifIndex.
func deviceTunnels(store *snmp.ValueStore) []deviceTunnel {
	var out []deviceTunnel

	// local(4).remote(4).encapsMethod.configID
	if rows, err := store.Column(profile.OIDTunnelConfigIfIndex); err == nil {
		for _, index := range sortedKeys(rows) {
			p := strings.Split(index, ".")
			if len(p) != 10 {
				continue
			}
			out = append(out, deviceTunnel{
				local:   strings.Join(p[0:4], "."),
				remote:  strings.Join(p[4:8], "."),
				ifIndex: rows[index].String(),
			})
		}
	}

	// addrType.localLen.local....remoteLen.remote....encapsMethod.configID
	if rows, err := store.Column(profile.OIDTunnelInetConfigIfIndex); err == nil {
		for _, index := range sortedKeys(rows) {
			p := strings.Split(index, ".")
			if len(p) < 2 || p[0] != inetAddressIPv4 {
				continue
			}
			localLen, err := strconv.Atoi(p[1])
			if err != nil || len(p) < 2+localLen+1 {
				continue
			}
			local := strings.Join(p[2:2+localLen], ".")
			remoteLen, err := strconv.Atoi(p[2+localLen])
			start := 3 + localLen
			if err != nil || len(p) < start+remoteLen {
				continue
			}
			out = append(out, deviceTunnel{
				local:   local,
				remote:  strings.Join(p[start:start+remoteLen], "."),
				ifIndex: rows[index].String(),
			})
		}
	}
	return out
}

func columnIP(store *snmp.ValueStore, oid, index string) string {
	value, err := store.ColumnValue(oid, index)
	if err != nil {
		return ""
	}
	return ipString(value)
}

func columnInt(store *snmp.ValueStore, oid, index string) int64 {
	value, err := store.ColumnValue(oid, index)
	if err != nil {
		return 0
	}
	f, err := value.Float()
	if err != nil {
		return 0
	}
	return int64(f)
}

func sortedStrings[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
