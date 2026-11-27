package report_test

import (
	"context"
	"testing"

	"github.com/gosnmp/gosnmp"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/naming"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/profile"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/report"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/snmp"
)

const (
	ifDescr       = "1.3.6.1.2.1.2.2.1.2"
	ifType        = "1.3.6.1.2.1.2.2.1.3"
	ifPhysAddress = "1.3.6.1.2.1.2.2.1.6"
	sysName       = "1.3.6.1.2.1.1.5.0"
	sysDescr      = "1.3.6.1.2.1.1.1.0"
	sysLocation   = "1.3.6.1.2.1.1.6.0"
)

func compileInventoryProfile(t *testing.T, name string, opts profile.CompileOptions) *profile.Compiled {
	t.Helper()
	store, err := profile.NewStore("")
	if err != nil {
		t.Fatal(err)
	}
	def, err := store.Resolve(name)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := profile.CompileWith(def, opts)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func containsOID(oids []string, want string) bool {
	for _, oid := range oids {
		if oid == want {
			return true
		}
	}
	return false
}

// The inventory columns are only walked when asked for, so a metrics-only
// receiver polls exactly what it did before.
func TestCompileInventoryColumnsOnlyWhenAsked(t *testing.T) {
	plain := compileInventoryProfile(t, "_generic-if", profile.CompileOptions{})
	for _, oid := range []string{ifPhysAddress, ifType, profile.OIDIPAdEntIfIndex} {
		if containsOID(plain.ColumnOIDs, oid) {
			t.Errorf("metrics-only compile fetches inventory column %s", oid)
		}
	}

	full := compileInventoryProfile(t, "_generic-if", profile.CompileOptions{InterfaceMetadata: true, IPAddresses: true})
	for _, oid := range []string{ifPhysAddress, ifType, ifDescr, ifAlias, profile.OIDIPAdEntIfIndex, profile.OIDIPAdEntNetMask} {
		if !containsOID(full.ColumnOIDs, oid) {
			t.Errorf("inventory compile does not fetch %s", oid)
		}
	}
}

func TestBuildInventory(t *testing.T) {
	compiled := compileInventoryProfile(t, "generic-device", profile.CompileOptions{InterfaceMetadata: true, IPAddresses: true})

	dev := twoInterfaceDevice(1000, 2000)
	dev.SetString(sysName, "core-sw-1")
	dev.SetString(sysDescr, "Test Switch")
	dev.SetString(sysLocation, "rack 4")
	for _, idx := range []string{"1", "2"} {
		dev.SetString(ifDescr+"."+idx, "GigabitEthernet0/"+idx)
		dev.SetInt(ifType+"."+idx, 6)
		dev.SetBytes(ifPhysAddress+"."+idx, []byte{0x82, 0x8a, 0x8c, 0x2f, 0xf8, byte(0x30 + len(idx))})
	}
	dev.SetInt(ifOperStatus+".2", 2) // port 2 down
	dev.SetInt(profile.OIDIPAdEntIfIndex+".10.0.0.1", 1)
	dev.Set(profile.OIDIPAdEntNetMask+".10.0.0.1", gosnmp.SnmpPDU{Type: gosnmp.IPAddress, Value: "255.255.255.0"})

	scalars, columns := compiled.FetchOIDs()
	store, _, err := snmp.Fetch(context.Background(), dev, scalars, columns, snmp.FetchConfig{})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	b := newBuilder(t, naming.Options{})
	inv := b.BuildInventory(testDevice(), compiled, store, report.InventoryOptions{Interfaces: true, IPAddresses: true})

	if got := inv.DeviceAttributes["name"]; got != "core-sw-1" {
		t.Errorf("device name = %v, want core-sw-1", got)
	}
	if got := inv.DeviceAttributes["ip_address"]; got != "10.0.0.1" {
		t.Errorf("device ip_address = %v", got)
	}
	if got := inv.DeviceAttributes["location"]; got != "rack 4" {
		t.Errorf("device location = %v", got)
	}
	if v, ok := inv.Resource.Get("host.id"); !ok || v.Str() != "10.0.0.1" {
		t.Errorf("resource host.id = %v", v.AsString())
	}

	if len(inv.Interfaces) != 2 {
		t.Fatalf("got %d interface records, want 2", len(inv.Interfaces))
	}
	port2 := inv.Interfaces[1]
	if port2.EntityType != report.EntityInterface {
		t.Errorf("entity type = %s", port2.EntityType)
	}
	if port2.ID[0].Value != "10.0.0.1" || port2.ID[1].Value != int64(2) {
		t.Errorf("interface id = %v, want host.id 10.0.0.1 and interface_index 2", port2.ID)
	}
	want := map[string]any{
		"name":         "Gi0/2",
		"interface":    "Gi0/2",
		"alias":        "uplink-2",
		"description":  "GigabitEthernet0/2",
		"admin_status": int64(1),
		"oper_status":  int64(2),
		"type":         int64(6),
		"is_physical":  true,
		"mac_address":  "82:8a:8c:2f:f8:31",
	}
	for k, v := range want {
		if port2.Attributes[k] != v {
			t.Errorf("interface %s = %#v, want %#v", k, port2.Attributes[k], v)
		}
	}

	if len(inv.IPAddresses) != 1 {
		t.Fatalf("got %d IP records, want 1", len(inv.IPAddresses))
	}
	ip := inv.IPAddresses[0]
	if ip.ID[1].Value != int64(1) || ip.ID[2].Value != "10.0.0.1" {
		t.Errorf("ip id = %v", ip.ID)
	}
	if ip.Attributes["prefixlen"] != int64(24) {
		t.Errorf("prefixlen = %v, want 24", ip.Attributes["prefixlen"])
	}
}

func TestRecordHashTracksContent(t *testing.T) {
	a := report.DeviceRecord("10.0.0.1", map[string]any{"name": "sw"}, report.DeviceStatusReachable, nil)
	b := report.DeviceRecord("10.0.0.1", map[string]any{"name": "sw"}, report.DeviceStatusReachable, nil)
	if a.Hash() != b.Hash() || a.Key() != b.Key() {
		t.Error("identical records must hash the same")
	}
	down := report.DeviceRecord("10.0.0.1", map[string]any{"name": "sw"}, report.DeviceStatusUnreachable,
		[]report.Diagnosis{{Severity: "error", Code: report.DiagFailedToPollDevice}})
	if a.Hash() == down.Hash() {
		t.Error("a status change must change the hash")
	}
	if a.Key() != down.Key() {
		t.Error("the key must not depend on the content")
	}
}
