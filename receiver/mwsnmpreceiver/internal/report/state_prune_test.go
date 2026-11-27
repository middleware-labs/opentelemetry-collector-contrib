package report_test

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/naming"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/snmp"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/snmptest"
)

func startTimeOf(md pmetric.Metrics, metric, hwID string) (uint64, bool) {
	ms := md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics()
	for i := 0; i < ms.Len(); i++ {
		m := ms.At(i)
		if m.Name() != metric || m.Type() != pmetric.MetricTypeSum {
			continue
		}
		dps := m.Sum().DataPoints()
		for j := 0; j < dps.Len(); j++ {
			if v, ok := dps.At(j).Attributes().Get("hw.id"); ok && v.Str() == hwID {
				return uint64(dps.At(j).StartTimestamp()), true
			}
		}
	}
	return 0, false
}

// A stream that stops appearing loses its state after a few polls, so the
// builder does not grow forever; streams still present keep their start time.
func TestBuilderPrunesStaleStreams(t *testing.T) {
	compiled := compileProfile(t, "_generic-if")
	b := newBuilder(t, naming.Options{})
	t0 := time.Unix(1_700_000_000, 0)

	build := func(dev *snmptest.Fake, at time.Time) pmetric.Metrics {
		scalars, columns := compiled.FetchOIDs()
		store, _, err := snmp.Fetch(context.Background(), dev, scalars, columns, snmp.FetchConfig{})
		if err != nil {
			t.Fatal(err)
		}
		md, _, _ := b.Build(testDevice(), compiled, store, at)
		return md
	}

	first := build(twoInterfaceDevice(1000, 1000), t0)
	start1, ok := startTimeOf(first, "hw.network.io", "if_1")
	if !ok {
		t.Fatal("no hw.network.io for if_1")
	}
	full, _ := b.StateSize()

	// Port 2 disappears.
	onePort := twoInterfaceDevice(2000, 2000)
	for _, oid := range []string{ifName, ifAlias, ifHCInOctets, ifHCOutOctets, ifInErrors, ifInDiscards,
		ifOutErrors, ifOutDiscards, ifAdminStatus, ifOperStatus, ifSpeed, ifHighSpeedOID,
		ifHCInUcast, ifHCInMulti, ifHCInBroad, ifHCOutUcast, ifHCOutMulti, ifHCOutBroad} {
		onePort.Delete(oid + ".2")
	}
	var last pmetric.Metrics
	for i := 1; i <= 7; i++ {
		last = build(onePort, t0.Add(time.Duration(i)*time.Minute))
	}
	pruned, _ := b.StateSize()
	if pruned >= full {
		t.Errorf("state did not shrink after port 2 went away: %d -> %d streams", full, pruned)
	}
	if start, _ := startTimeOf(last, "hw.network.io", "if_1"); start != start1 {
		t.Errorf("port 1 start time changed from %d to %d; a live stream must keep it", start1, start)
	}
}

// A counter that goes down (reboot, wrap) gets a new start time, so the
// series is a valid cumulative and consumers do not compute a negative rate.
func TestCounterResetGetsNewStartTime(t *testing.T) {
	compiled := compileProfile(t, "_generic-if")
	b := newBuilder(t, naming.Options{})
	t0 := time.Unix(1_700_000_000, 0)
	build := func(octets uint64, at time.Time) pmetric.Metrics {
		scalars, columns := compiled.FetchOIDs()
		store, _, err := snmp.Fetch(context.Background(), twoInterfaceDevice(octets, octets), scalars, columns, snmp.FetchConfig{})
		if err != nil {
			t.Fatal(err)
		}
		md, _, _ := b.Build(testDevice(), compiled, store, at)
		return md
	}
	startOf := func(md pmetric.Metrics) uint64 {
		v, ok := startTimeOf(md, "hw.network.io", "if_1")
		if !ok {
			t.Fatal("no hw.network.io for if_1")
		}
		return v
	}

	s1 := startOf(build(1000, t0))
	if s2 := startOf(build(2000, t0.Add(time.Minute))); s2 != s1 {
		t.Fatalf("start time moved without a reset: %d -> %d", s1, s2)
	}
	// Counter reset: 2000 -> 50.
	s3 := startOf(build(50, t0.Add(2*time.Minute)))
	if want := uint64(t0.Add(time.Minute).UnixNano()); s3 != want {
		t.Errorf("after a reset start = %d, want the previous point's time %d", s3, want)
	}
	if s4 := startOf(build(80, t0.Add(3*time.Minute))); s4 != s3 {
		t.Errorf("start time moved again without a reset: %d -> %d", s3, s4)
	}
}

// An UpDownCounter (link speed here) may go down; that is not a reset, so the
// start time must stay.
func TestUpDownCounterDecreaseIsNotAReset(t *testing.T) {
	compiled := compileProfile(t, "_generic-if")
	b := newBuilder(t, naming.Options{})
	t0 := time.Unix(1_700_000_000, 0)
	build := func(speed uint, at time.Time) pmetric.Metrics {
		dev := twoInterfaceDevice(1000, 1000)
		dev.SetGauge(ifHighSpeedOID+".1", speed)
		scalars, columns := compiled.FetchOIDs()
		store, _, err := snmp.Fetch(context.Background(), dev, scalars, columns, snmp.FetchConfig{})
		if err != nil {
			t.Fatal(err)
		}
		md, _, _ := b.Build(testDevice(), compiled, store, at)
		return md
	}
	s1, ok := startTimeOf(build(10000, t0), "hw.network.bandwidth.limit", "if_1")
	if !ok {
		t.Fatal("no hw.network.bandwidth.limit for if_1")
	}
	s2, _ := startTimeOf(build(1000, t0.Add(time.Minute)), "hw.network.bandwidth.limit", "if_1")
	if s2 != s1 {
		t.Errorf("link speed going down moved the start time: %d -> %d", s1, s2)
	}
}
