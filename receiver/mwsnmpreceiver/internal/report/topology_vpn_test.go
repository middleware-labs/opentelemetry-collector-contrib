package report_test

import (
	"context"
	"testing"

	"github.com/gosnmp/gosnmp"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/naming"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/profile"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/report"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/snmp"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/snmptest"
)

func fetchInventory(t *testing.T, profileName string, dev *snmptest.Fake, opts report.InventoryOptions) *report.Inventory {
	t.Helper()
	compiled := compileInventoryProfile(t, profileName, profile.CompileOptions{
		InterfaceMetadata: opts.Interfaces, IPAddresses: opts.IPAddresses,
		Topology: opts.Topology, VPN: opts.VPN,
	})
	scalars, columns := compiled.FetchOIDs()
	store, _, err := snmp.Fetch(context.Background(), dev, scalars, columns, snmp.FetchConfig{})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	return newBuilder(t, naming.Options{}).BuildInventory(testDevice(), compiled, store, opts)
}

func ipPDU(addr string) gosnmp.SnmpPDU { return gosnmp.SnmpPDU{Type: gosnmp.IPAddress, Value: addr} }

// Profiles cut os_name, os_version and model out of sysDescr with
// match_pattern. Without it, each of those fields was the whole sysDescr.
func TestDeviceFieldsApplyMatchPattern(t *testing.T) {
	dev := snmptest.New()
	dev.SetString(sysName, "arista-1")
	dev.SetString(sysDescr, "Arista Networks EOS version 4.28.3M running on an Arista Networks DCS-7050SX-64")

	inv := fetchInventory(t, "arista", dev, report.InventoryOptions{})
	want := map[string]string{"os_name": "EOS", "os_version": "4.28.3M", "model": "DCS-7050SX-64"}
	for field, value := range want {
		if got := inv.DeviceAttributes[field]; got != value {
			t.Errorf("%s = %q, want %q", field, got, value)
		}
	}
	// The metric resource gets the same fix.
	if v, _ := inv.Resource.Get("os.name"); v.Str() != "EOS" {
		t.Errorf("resource os.name = %q, want EOS", v.Str())
	}
}

const (
	lldpRemChassisIDSubtype = "1.0.8802.1.1.2.1.4.1.1.4"
	lldpRemChassisID        = "1.0.8802.1.1.2.1.4.1.1.5"
	lldpRemPortIDSubtype    = "1.0.8802.1.1.2.1.4.1.1.6"
	lldpRemPortID           = "1.0.8802.1.1.2.1.4.1.1.7"
	lldpRemPortDesc         = "1.0.8802.1.1.2.1.4.1.1.8"
	lldpRemSysName          = "1.0.8802.1.1.2.1.4.1.1.9"
	lldpRemManAddrIfSubtype = "1.0.8802.1.1.2.1.4.2.1.3"
	lldpLocPortID           = "1.0.8802.1.1.2.1.3.7.1.3"

	cdpCacheDeviceID           = "1.3.6.1.4.1.9.9.23.1.2.1.1.6"
	cdpCacheDevicePort         = "1.3.6.1.4.1.9.9.23.1.2.1.1.7"
	cdpCacheSysName            = "1.3.6.1.4.1.9.9.23.1.2.1.1.17"
	cdpCachePrimaryMgmtAddrTyp = "1.3.6.1.4.1.9.9.23.1.2.1.1.19"
	cdpCachePrimaryMgmtAddr    = "1.3.6.1.4.1.9.9.23.1.2.1.1.20"
)

func TestLLDPLinks(t *testing.T) {
	dev := twoInterfaceDevice(1, 1)
	// The local port (lldpLocPortNum 7) is named "Gi0/2" with no subtype, so
	// it is resolved by name to ifIndex 2, the same way as the Datadog agent.
	dev.SetString(lldpLocPortID+".7", "Gi0/2")

	row := ".0.7.1"                            // timeMark 0, local port 7, remote index 1
	dev.SetInt(lldpRemChassisIDSubtype+row, 4) // mac_address
	dev.SetBytes(lldpRemChassisID+row, []byte{0x82, 0x8a, 0x8c, 0x2f, 0xf8, 0x36})
	dev.SetInt(lldpRemPortIDSubtype+row, 5) // interface_name
	dev.SetString(lldpRemPortID+row, "gi9")
	dev.SetString(lldpRemPortDesc+row, "uplink to core")
	dev.SetString(lldpRemSysName+row, "K05-ITV")
	// Management address 10.25.0.19, carried in the index.
	dev.SetInt(lldpRemManAddrIfSubtype+row+".1.4.10.25.0.19", 2)

	inv := fetchInventory(t, "_generic-if", dev, report.InventoryOptions{Topology: true})
	if len(inv.Links) != 1 {
		t.Fatalf("got %d links, want 1", len(inv.Links))
	}
	link := inv.Links[0]
	if link.EntityType != report.EntityLink || link.ID[1].Value != "7.1" {
		t.Errorf("link id = %v (%s), want link_id 7.1", link.ID, link.EntityType)
	}
	want := map[string]any{
		"source_type":                  "lldp",
		"local.interface.id":           "Gi0/2",
		"local.interface_index":        int64(2),
		"remote.device.id":             "82:8a:8c:2f:f8:36",
		"remote.device.id_type":        "mac_address",
		"remote.device.name":           "K05-ITV",
		"remote.device.ip_address":     "10.25.0.19",
		"remote.interface.id":          "gi9",
		"remote.interface.id_type":     "interface_name",
		"remote.interface.description": "uplink to core",
	}
	for k, v := range want {
		if link.Attributes[k] != v {
			t.Errorf("%s = %#v, want %#v", k, link.Attributes[k], v)
		}
	}
	// Interface records were not asked for, only used to resolve the port.
	if len(inv.Interfaces) != 0 {
		t.Errorf("interface records sent without metadata.interfaces")
	}
}

func TestCDPFallback(t *testing.T) {
	dev := twoInterfaceDevice(1, 1)
	row := ".1.3" // local ifIndex 1, device index 3
	dev.SetString(cdpCacheDeviceID+row, "core-router.example")
	dev.SetString(cdpCacheDevicePort+row, "GigabitEthernet0/0/1")
	dev.SetString(cdpCacheSysName+row, "core-router")
	dev.SetInt(cdpCachePrimaryMgmtAddrTyp+row, 1)
	dev.SetBytes(cdpCachePrimaryMgmtAddr+row, []byte{10, 1, 2, 3})

	inv := fetchInventory(t, "_generic-if", dev, report.InventoryOptions{Topology: true})
	if len(inv.Links) != 1 {
		t.Fatalf("got %d links, want 1 from CDP", len(inv.Links))
	}
	a := inv.Links[0].Attributes
	if a["source_type"] != "cdp" || a["local.interface_index"] != int64(1) ||
		a["remote.device.ip_address"] != "10.1.2.3" || a["remote.interface.id"] != "GigabitEthernet0/0/1" {
		t.Errorf("cdp link = %v", a)
	}
}

const (
	cipSecTunLocalAddr  = "1.3.6.1.4.1.9.9.171.1.3.2.1.4"
	cipSecTunRemoteAddr = "1.3.6.1.4.1.9.9.171.1.3.2.1.5"
	cipSecTunLifeSize   = "1.3.6.1.4.1.9.9.171.1.3.2.1.8"
	cipSecTunLifeTime   = "1.3.6.1.4.1.9.9.171.1.3.2.1.9"
	cipSecTunStatus     = "1.3.6.1.4.1.9.9.171.1.3.2.1.51"

	inetCidrRouteIfIndex    = "1.3.6.1.2.1.4.24.7.1.7"
	inetCidrRouteStatus     = "1.3.6.1.2.1.4.24.7.1.17"
	tunnelInetConfigIfIndex = "1.3.6.1.2.1.10.131.1.1.3.1.6"
)

func TestVPNTunnels(t *testing.T) {
	dev := twoInterfaceDevice(1, 1)

	// Tunnel A: a route names its peer as next hop.
	dev.Set(cipSecTunLocalAddr+".1", ipPDU("203.0.113.1"))
	dev.Set(cipSecTunRemoteAddr+".1", ipPDU("198.51.100.7"))
	dev.SetInt(cipSecTunStatus+".1", 1)
	dev.SetGauge(cipSecTunLifeSize+".1", 4608000)
	dev.SetInt(cipSecTunLifeTime+".1", 3600)
	// 10.20.0.0/16 via next hop 198.51.100.7:
	// destType 1, len 4, dest, prefix 16, policy len 2 (0.0), hopType 1, len 4, hop
	routeA := ".1.4.10.20.0.0.16.2.0.0.1.4.198.51.100.7"
	dev.SetInt(inetCidrRouteIfIndex+routeA, 12)
	dev.SetInt(inetCidrRouteStatus+routeA, 1)

	// Tunnel B: no next-hop route, so its routes come from its tunnel
	// interface (ifIndex 20, from TUNNEL-MIB).
	dev.Set(cipSecTunLocalAddr+".2", ipPDU("203.0.113.1"))
	dev.Set(cipSecTunRemoteAddr+".2", ipPDU("192.0.2.9"))
	dev.SetInt(cipSecTunStatus+".2", 2)
	routeB := ".1.4.172.16.0.0.12.2.0.0.0.0" // 172.16.0.0/12, next hop unknown
	dev.SetInt(inetCidrRouteIfIndex+routeB, 20)
	dev.SetInt(inetCidrRouteStatus+routeB, 1)
	// addrType 1, local len 4, local, remote len 4, remote, encaps 1, id 1
	dev.SetInt(tunnelInetConfigIfIndex+".1.4.203.0.113.1.4.192.0.2.9.1.1", 20)

	// An inactive route is ignored.
	routeC := ".1.4.10.99.0.0.16.2.0.0.1.4.198.51.100.7"
	dev.SetInt(inetCidrRouteIfIndex+routeC, 12)
	dev.SetInt(inetCidrRouteStatus+routeC, 2)

	inv := fetchInventory(t, "_generic-if", dev, report.InventoryOptions{VPN: true})
	if len(inv.VPNTunnels) != 2 {
		t.Fatalf("got %d tunnels, want 2", len(inv.VPNTunnels))
	}
	byRemote := map[any]report.Record{}
	for _, rec := range inv.VPNTunnels {
		byRemote[rec.ID[2].Value] = rec
	}

	a := byRemote["198.51.100.7"].Attributes
	if a["status"] != "active" || a["life_size"] != int64(4608000) || a["life_time"] != int64(3600) {
		t.Errorf("tunnel A = %v", a)
	}
	if routes, _ := a["route_addresses"].([]string); len(routes) != 1 || routes[0] != "10.20.0.0/16" {
		t.Errorf("tunnel A routes = %v, want [10.20.0.0/16]", a["route_addresses"])
	}

	b := byRemote["192.0.2.9"].Attributes
	if b["status"] != "destroy" || b["interface_index"] != int64(20) {
		t.Errorf("tunnel B = %v, want status destroy on interface 20", b)
	}
	if routes, _ := b["route_addresses"].([]string); len(routes) != 1 || routes[0] != "172.16.0.0/12" {
		t.Errorf("tunnel B routes = %v, want [172.16.0.0/12]", b["route_addresses"])
	}
}
