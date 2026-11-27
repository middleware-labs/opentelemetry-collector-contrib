package report_test

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/naming"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/profile"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/report"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/snmp"
)

func firstValue(t *testing.T, m pmetric.Metric, hwOrIndex map[string]string) float64 {
	t.Helper()
	var dps pmetric.NumberDataPointSlice
	switch m.Type() {
	case pmetric.MetricTypeGauge:
		dps = m.Gauge().DataPoints()
	case pmetric.MetricTypeSum:
		dps = m.Sum().DataPoints()
	default:
		t.Fatalf("%s: unexpected type %s", m.Name(), m.Type())
	}
	for i := 0; i < dps.Len(); i++ {
		match := true
		for k, v := range hwOrIndex {
			if got, ok := dps.At(i).Attributes().Get(k); !ok || got.AsString() != v {
				match = false
			}
		}
		if match {
			return dps.At(i).DoubleValue()
		}
	}
	t.Fatalf("%s: no point with %v", m.Name(), hwOrIndex)
	return 0
}

// Datadog-compatible names carry the raw SNMP value, as the Datadog agent
// sends it, in both datadog_compat and both modes.
func TestCompatNamesCarryRawValues(t *testing.T) {
	for _, scheme := range []naming.Scheme{naming.SchemeDatadogCompat, naming.SchemeBoth} {
		compiled := compileProfile(t, "_generic-if")
		opts := naming.DefaultOptions()
		opts.Scheme = scheme
		b := newBuilder(t, opts)

		dev := twoInterfaceDevice(1, 1)
		dev.SetInt(ifOperStatus+".2", 7) // lowerLayerDown
		metrics := metricsByName(poll(t, b, dev, compiled, time.Now()))

		port2 := map[string]string{"interface_index": "2"}
		cases := map[string]float64{
			"snmp.ifOperStatus":  7,     // the enum, not a 1/0 value map
			"snmp.ifAdminStatus": 1,     // one point with the enum, not a state set
			"snmp.ifHighSpeed":   10000, // Mbit/s, not bytes per second
		}
		for name, want := range cases {
			m, ok := metrics[name]
			if !ok {
				t.Errorf("%s: %s not emitted", scheme, name)
				continue
			}
			if got := firstValue(t, m, port2); got != want {
				t.Errorf("%s: %s = %v, want raw %v", scheme, name, got, want)
			}
			if m.Type() == pmetric.MetricTypeGauge && m.Gauge().DataPoints().Len() != 2 {
				t.Errorf("%s: %s has %d points, want one per port", scheme, name, m.Gauge().DataPoints().Len())
			}
		}
	}
}

func TestHostNameModes(t *testing.T) {
	compiled := compileInventoryProfile(t, "generic-device", profile.CompileOptions{})
	dev := twoInterfaceDevice(1, 1)
	dev.SetString(sysName, "Switch")

	cases := map[string]string{
		report.HostNameNone:     "",
		report.HostNameSysName:  "Switch",
		report.HostNameDeviceID: "device:10.0.0.1",
	}
	for mode, want := range cases {
		info := testDevice()
		info.HostName = mode
		b := newBuilder(t, naming.DefaultOptions())
		scalars, columns := compiled.FetchOIDs()
		store, _, err := snmp.Fetch(context.Background(), dev, scalars, columns, snmp.FetchConfig{})
		if err != nil {
			t.Fatal(err)
		}
		md, _, _ := b.Build(info, compiled, store, time.Now())
		res := md.ResourceMetrics().At(0).Resource().Attributes()
		got, ok := res.Get("host.name")
		if want == "" && ok {
			t.Errorf("%s: host.name = %q, want unset", mode, got.Str())
		}
		if want != "" && got.Str() != want {
			t.Errorf("%s: host.name = %q, want %q", mode, got.Str(), want)
		}
		if v, _ := res.Get("snmp.device.name"); v.Str() != "Switch" {
			t.Errorf("%s: snmp.device.name = %q, want sysName", mode, v.Str())
		}
	}
}
