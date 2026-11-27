package mwsnmpreceiver

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/experimentalmetricmetadata"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/naming"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/profile"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/report"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/snmp"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/snmptest"
)

// TestInventoryMemory measures what the receiver keeps in memory per device:
// the state it always had (compiled profile, metric builder) and the state the
// inventory flow adds (what was last sent, and the last device record). It
// also measures the garbage one poll creates.
//
// It is a measurement, not a check, so it only runs on request:
//
//	MWSNMP_MEMORY=100x48,1000x48 go test -run TestInventoryMemory -v .
func TestInventoryMemory(t *testing.T) {
	spec := os.Getenv("MWSNMP_MEMORY")
	if spec == "" {
		t.Skip("set MWSNMP_MEMORY=<devices>x<ports>[,...] to run")
	}
	for _, scenario := range splitScenarios(t, spec) {
		measureScenario(t, scenario[0], scenario[1])
	}
}

func splitScenarios(t *testing.T, spec string) [][2]int {
	var out [][2]int
	for _, part := range splitComma(spec) {
		var d, p int
		if _, err := fmt.Sscanf(part, "%dx%d", &d, &p); err != nil {
			t.Fatalf("bad scenario %q: %v", part, err)
		}
		out = append(out, [2]int{d, p})
	}
	return out
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}

// switchWithPorts scripts a switch: system group, ports with IF-MIB metrics and
// metadata, one IP and four LLDP neighbours.
func switchWithPorts(ports int) *snmptest.Fake {
	dev := snmptest.New()
	dev.SetString("1.3.6.1.2.1.1.5.0", "core-sw-01")
	dev.SetString("1.3.6.1.2.1.1.1.0", "Cisco IOS Software, C2960X Software, Version 15.2(7)E3")
	dev.SetString("1.3.6.1.2.1.1.6.0", "dc1 rack 4")
	dev.SetOID("1.3.6.1.2.1.1.2.0", "1.3.6.1.4.1.9.1.1208")
	dev.SetInt("1.3.6.1.2.1.2.1.0", ports)
	for i := 1; i <= ports; i++ {
		idx := "." + strconv.Itoa(i)
		dev.SetString("1.3.6.1.2.1.31.1.1.1.1"+idx, "Gi1/0/"+strconv.Itoa(i))
		dev.SetString("1.3.6.1.2.1.31.1.1.1.18"+idx, fmt.Sprintf("desk-%03d", i))
		dev.SetString("1.3.6.1.2.1.2.2.1.2"+idx, "GigabitEthernet1/0/"+strconv.Itoa(i))
		dev.SetInt("1.3.6.1.2.1.2.2.1.3"+idx, 6)
		dev.SetBytes("1.3.6.1.2.1.2.2.1.6"+idx, []byte{0, 0x1b, 0x54, byte(i >> 8), byte(i), 1})
		dev.SetInt("1.3.6.1.2.1.2.2.1.7"+idx, 1)
		dev.SetInt("1.3.6.1.2.1.2.2.1.8"+idx, 1)
		dev.SetGauge("1.3.6.1.2.1.31.1.1.1.15"+idx, 1000)
		for _, col := range []string{"6", "7", "8", "9", "10", "11", "12", "13"} {
			dev.SetCounter64("1.3.6.1.2.1.31.1.1.1."+col+idx, 123456789)
		}
		for _, col := range []string{"13", "14", "19", "20"} {
			dev.SetCounter32("1.3.6.1.2.1.2.2.1."+col+idx, 5)
		}
	}
	dev.SetInt(profile.OIDIPAdEntIfIndex+".10.0.0.1", 1)
	for i := 1; i <= 4; i++ {
		row := fmt.Sprintf(".0.%d.1", i)
		dev.SetString(profile.OIDLldpLocPortID+"."+strconv.Itoa(i), "Gi1/0/"+strconv.Itoa(i))
		dev.SetInt(profile.OIDLldpRemChassisIDSubtype+row, 4)
		dev.SetBytes(profile.OIDLldpRemChassisID+row, []byte{0x82, 0x8a, 0x8c, 0x2f, 0xf8, byte(i)})
		dev.SetInt(profile.OIDLldpRemPortIDSubtype+row, 5)
		dev.SetString(profile.OIDLldpRemPortID+row, "eth0")
		dev.SetString(profile.OIDLldpRemSysName+row, "ap-"+strconv.Itoa(i))
	}
	return dev
}

func heapNow() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func totalAlloc() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.TotalAlloc
}

func measureScenario(t *testing.T, devices, ports int) {
	t.Helper()
	opts := profile.CompileOptions{InterfaceMetadata: true, IPAddresses: true, Topology: true}
	invOpts := report.InventoryOptions{Interfaces: true, IPAddresses: true, Topology: true}

	store, err := profile.NewStore("")
	if err != nil {
		t.Fatal(err)
	}
	def, err := store.Resolve("generic-device")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := naming.New(naming.Options{})
	if err != nil {
		t.Fatal(err)
	}
	probe, err := profile.CompileWith(def, opts)
	if err != nil {
		t.Fatal(err)
	}
	scalars, columns := probe.FetchOIDs()
	values, _, err := snmp.Fetch(context.Background(), switchWithPorts(ports), scalars, columns, snmp.FetchConfig{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	info := func(i int) report.DeviceInfo {
		addr := fmt.Sprintf("10.%d.%d.%d", i>>16&255, i>>8&255, i&255)
		return report.DeviceInfo{ID: "default:" + addr, Address: addr, Namespace: "default", ProfileName: "generic-device"}
	}

	// 1. State the receiver always kept: a compiled profile and a metric
	//    builder per device, after one poll.
	base := heapNow()
	states := make([]*deviceState, devices)
	for i := range states {
		compiled, err := profile.CompileWith(def, opts)
		if err != nil {
			t.Fatal(err)
		}
		states[i] = &deviceState{deviceID: info(i).ID, compiled: compiled}
	}
	afterProfiles := heapNow()
	for i, state := range states {
		state.builder = report.NewBuilder(registry)
		_, _, _ = state.builder.Build(info(i), state.compiled, values, now)
	}
	afterMetrics := heapNow()

	// 2. State the inventory flow adds: what was last sent per record, and the
	//    last device record kept for failed polls.
	records := 0
	for i, state := range states {
		inv := state.builder.BuildInventory(info(i), state.compiled, values, invOpts)
		recs := append([]report.Record{report.DeviceRecord(state.deviceID, inv.DeviceAttributes, report.DeviceStatusReachable, nil)}, inv.Interfaces...)
		recs = append(recs, inv.IPAddresses...)
		recs = append(recs, inv.Links...)
		records = len(recs)
		state.inventory.commit(recs, true, now)
		state.lastDeviceAttrs = inv.DeviceAttributes
		state.lastResource = pcommon.NewMap()
		inv.Resource.CopyTo(state.lastResource)
	}
	afterInventory := heapNow()
	runtime.KeepAlive(states)

	// 3. Garbage per poll (allocated, then freed): metrics build vs inventory
	//    build + change check + conversion to OTLP logs.
	s := states[0]
	a0 := totalAlloc()
	var md pmetric.Metrics
	md, _, _ = s.builder.Build(info(0), s.compiled, values, now)
	a1 := totalAlloc()
	inv := s.builder.BuildInventory(info(0), s.compiled, values, invOpts)
	recs := append([]report.Record{report.DeviceRecord(s.deviceID, inv.DeviceAttributes, 1, nil)}, inv.Interfaces...)
	recs = append(recs, inv.IPAddresses...)
	recs = append(recs, inv.Links...)
	pending, _ := s.inventory.pending(recs, now.Add(time.Minute), 10*time.Minute)
	a2 := totalAlloc()
	events := experimentalmetricmetadata.NewEntityEventsSlice()
	for _, rec := range recs {
		ev := events.AppendEmpty()
		rec.PutID(ev.ID())
		d := ev.SetEntityState()
		d.SetEntityType(rec.EntityType)
		rec.PutAttributes(d.Attributes())
	}
	_ = events.ConvertAndMoveToLogs()
	a3 := totalAlloc()

	perDev := func(bytes uint64) string { return fmt.Sprintf("%.1f KB/device", float64(bytes)/1024/float64(devices)) }
	mb := func(bytes uint64) string { return fmt.Sprintf("%.1f MB", float64(bytes)/1024/1024) }
	t.Logf("scenario %d devices x %d ports (%d records per device, %d datapoints per poll)",
		devices, ports, records, md.DataPointCount())
	t.Logf("  kept: compiled profile per device        %s  (%s)", mb(afterProfiles-base), perDev(afterProfiles-base))
	t.Logf("  kept: metric builder (start times etc.)  %s  (%s)", mb(afterMetrics-afterProfiles), perDev(afterMetrics-afterProfiles))
	t.Logf("  kept: inventory state (tracker + last)   %s  (%s)", mb(afterInventory-afterMetrics), perDev(afterInventory-afterMetrics))
	t.Logf("  garbage per poll: metrics build %s, inventory build+check %s (changed=%d), full OTLP conversion %s",
		kb(a1-a0), kb(a2-a1), len(pending), kb(a3-a2))
}

func kb(bytes uint64) string { return fmt.Sprintf("%.0f KB", float64(bytes)/1024) }
