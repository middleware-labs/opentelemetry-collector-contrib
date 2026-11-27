package mwsnmpreceiver

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/receiver/receivertest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/discovery"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/report"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/snmp"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/snmptest"
)

// entityEvent is one decoded inventory record.
type entityEvent struct {
	eventType  string
	entityType string
	id         map[string]any
	attrs      map[string]any
	resource   map[string]any
	scopeMark  bool
}

func entityEvents(sink *consumertest.LogsSink) []entityEvent {
	var out []entityEvent
	for _, logs := range sink.AllLogs() {
		for i := 0; i < logs.ResourceLogs().Len(); i++ {
			rl := logs.ResourceLogs().At(i)
			for j := 0; j < rl.ScopeLogs().Len(); j++ {
				sl := rl.ScopeLogs().At(j)
				mark, _ := sl.Scope().Attributes().Get("otel.entity.event_as_log")
				for k := 0; k < sl.LogRecords().Len(); k++ {
					a := sl.LogRecords().At(k).Attributes()
					ev := entityEvent{
						resource:  rl.Resource().Attributes().AsRaw(),
						scopeMark: mark.Bool(),
					}
					if v, ok := a.Get("otel.entity.event.type"); ok {
						ev.eventType = v.Str()
					}
					if v, ok := a.Get("otel.entity.type"); ok {
						ev.entityType = v.Str()
					}
					if v, ok := a.Get("otel.entity.id"); ok {
						ev.id = v.Map().AsRaw()
					}
					if v, ok := a.Get("otel.entity.attributes"); ok {
						ev.attrs = v.Map().AsRaw()
					}
					out = append(out, ev)
				}
			}
		}
	}
	return out
}

func countEvents(events []entityEvent, match func(entityEvent) bool) int {
	n := 0
	for _, ev := range events {
		if match(ev) {
			n++
		}
	}
	return n
}

func startInventoryReceiver(t *testing.T, cfg *Config, f *fleet) (*snmpReceiver, *consumertest.LogsSink) {
	t.Helper()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config invalid: %v", err)
	}
	r, err := newReceiver(cfg, receivertest.NewNopSettings(componentType), new(consumertest.MetricsSink))
	if err != nil {
		t.Fatal(err)
	}
	logs := new(consumertest.LogsSink)
	r.logsConsumer = logs
	r.dial = f.dial
	if err := r.Start(context.Background(), componenttest.NewNopHost()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Shutdown(context.Background()) })
	return r, logs
}

func TestInventoryFlow(t *testing.T) {
	f := newFleet("public", "10.0.0.1")
	cfg := subnetTestConfig("10.0.0.0/30", "public")
	cfg.Discovery.AllowedFailures = 3
	_, sink := startInventoryReceiver(t, cfg, f)

	isDevice := func(status int64) func(entityEvent) bool {
		return func(ev entityEvent) bool {
			return ev.eventType == "entity_state" && ev.entityType == report.EntityDevice && ev.attrs["status"] == status
		}
	}
	isInterface := func(ev entityEvent) bool { return ev.entityType == report.EntityInterface }

	waitFor(t, 5*time.Second, "device and interface records", func() bool {
		events := entityEvents(sink)
		return countEvents(events, isDevice(report.DeviceStatusReachable)) >= 1 && countEvents(events, isInterface) >= 2
	})

	events := entityEvents(sink)
	for _, ev := range events {
		if !ev.scopeMark {
			t.Fatal("scope is missing otel.entity.event_as_log")
		}
		if ev.resource["host.id"] == nil {
			t.Fatal("resource is missing host.id")
		}
	}
	var device entityEvent
	for _, ev := range events {
		if isDevice(report.DeviceStatusReachable)(ev) {
			device = ev
			break
		}
	}
	if device.id["host.id"] != "default:10.0.0.1" {
		t.Errorf("device id = %v", device.id)
	}
	if device.attrs["name"] != "switch-10.0.0.1" {
		t.Errorf("device name = %v, want sysName", device.attrs["name"])
	}
	if diags, ok := device.attrs["diagnoses"].([]any); !ok || len(diags) != 0 {
		t.Errorf("healthy device diagnoses = %#v, want an empty list", device.attrs["diagnoses"])
	}
	for _, ev := range events {
		if isInterface(ev) {
			if ev.id["interface_index"] == nil || ev.attrs["name"] == nil || ev.attrs["oper_status"] != int64(1) {
				t.Errorf("interface record incomplete: id=%v attrs=%v", ev.id, ev.attrs)
			}
		}
	}

	// Nothing changes on the following polls, so nothing is resent before
	// resend_interval (10m by default).
	time.Sleep(500 * time.Millisecond)
	if n := countEvents(entityEvents(sink), isInterface); n != 2 {
		t.Errorf("interface records sent %d times, want 2 (no resend while unchanged)", n)
	}

	// The device stops answering: status 2 with a diagnosis, then a delete once
	// the failure budget is spent.
	f.kill("10.0.0.1")
	waitFor(t, 5*time.Second, "unreachable device record", func() bool {
		return countEvents(entityEvents(sink), isDevice(report.DeviceStatusUnreachable)) >= 1
	})
	for _, ev := range entityEvents(sink) {
		if !isDevice(report.DeviceStatusUnreachable)(ev) {
			continue
		}
		diags, _ := ev.attrs["diagnoses"].([]any)
		if len(diags) != 1 || diags[0].(map[string]any)["code"] != report.DiagFailedToPollDevice {
			t.Errorf("diagnoses = %#v, want one %s", ev.attrs["diagnoses"], report.DiagFailedToPollDevice)
		}
		if ev.attrs["name"] != "switch-10.0.0.1" {
			t.Errorf("failed-poll record lost the device name: %v", ev.attrs)
		}
	}
	waitFor(t, 5*time.Second, "entity_delete", func() bool {
		return countEvents(entityEvents(sink), func(ev entityEvent) bool {
			return ev.eventType == "entity_delete" && ev.entityType == report.EntityDevice
		}) == 1
	})
}

func TestInventoryTracker(t *testing.T) {
	now := time.Now()
	port := func(status int64) report.Record {
		return report.Record{
			EntityType: report.EntityInterface,
			ID:         []report.KV{{Key: "host.id", Value: "d"}, {Key: "interface_index", Value: int64(1)}},
			Attributes: map[string]any{"oper_status": status},
		}
	}
	other := report.Record{
		EntityType: report.EntityInterface,
		ID:         []report.KV{{Key: "host.id", Value: "d"}, {Key: "interface_index", Value: int64(2)}},
		Attributes: map[string]any{"oper_status": int64(1)},
	}

	var tr inventoryTracker
	sent, full := tr.pending([]report.Record{port(1), other}, now, time.Minute)
	if !full || len(sent) != 2 {
		t.Fatalf("first send: full=%v n=%d, want a full send of 2", full, len(sent))
	}
	tr.commit(sent, full, now)

	if sent, _ := tr.pending([]report.Record{port(1), other}, now.Add(time.Second), time.Minute); len(sent) != 0 {
		t.Errorf("unchanged records resent: %d", len(sent))
	}
	sent, full = tr.pending([]report.Record{port(2), other}, now.Add(time.Second), time.Minute)
	if full || len(sent) != 1 || sent[0].Attributes["oper_status"] != int64(2) {
		t.Errorf("changed record: full=%v sent=%v, want only the changed port", full, sent)
	}
	// Not committed (pipeline refused it), so it is still pending.
	if sent, _ := tr.pending([]report.Record{port(2), other}, now.Add(2*time.Second), time.Minute); len(sent) != 1 {
		t.Errorf("uncommitted change not retried: %d", len(sent))
	}
	if _, full := tr.pending([]report.Record{port(2), other}, now.Add(time.Minute), time.Minute); !full {
		t.Error("resend_interval elapsed but no full send")
	}
	if _, full := tr.pending([]report.Record{port(1)}, now.Add(time.Second), 0); !full {
		t.Error("resend_interval 0 must send everything every poll")
	}
}

func TestFactorySharesOnePoller(t *testing.T) {
	factory := NewFactory()
	cfg := subnetTestConfig("10.0.0.0/30", "public")
	set := receivertest.NewNopSettings(componentType)

	metrics, err := factory.CreateMetrics(context.Background(), set, cfg, consumertest.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	logs, err := factory.CreateLogs(context.Background(), set, cfg, consumertest.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if any(metrics) != any(logs) {
		t.Error("metrics and logs pipelines with one config must share a receiver")
	}
	r := receivers.GetOrAdd(cfg, nil).Unwrap().(*snmpReceiver)
	if r.metricsConsumer == nil || r.logsConsumer == nil {
		t.Error("shared receiver is missing a consumer")
	}
	if err := metrics.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// A device that never answered still gets a device record, identified without
// a host.name (which would otherwise be the IP and create a second host).
func TestMinimalResourceHasNoHostName(t *testing.T) {
	m := minimalResource(report.DeviceInfo{ID: "10.0.0.9", Address: "10.0.0.9"})
	if _, ok := m.Get("host.name"); ok {
		t.Error("minimal resource must not set host.name")
	}
	if v, _ := m.Get("host.id"); v.Str() != "10.0.0.9" {
		t.Errorf("host.id = %v", v.AsString())
	}
}

// Two sites reusing the same private range stay apart through the namespace,
// and the metrics and the inventory records carry the same device ID.
func TestNamespaceInDeviceID(t *testing.T) {
	f := newFleet("public", "10.0.0.1")
	cfg := subnetTestConfig("10.0.0.0/30", "public")
	cfg.Namespace = "site-a"
	r, err := newReceiver(cfg, receivertest.NewNopSettings(componentType), new(consumertest.MetricsSink))
	if err != nil {
		t.Fatal(err)
	}
	metrics := r.metricsConsumer.(*consumertest.MetricsSink)
	logs := new(consumertest.LogsSink)
	r.logsConsumer = logs
	r.dial = f.dial
	if err := r.Start(context.Background(), componenttest.NewNopHost()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Shutdown(context.Background()) })

	waitFor(t, 5*time.Second, "metrics and records", func() bool {
		return len(metrics.AllMetrics()) > 0 && len(logs.AllLogs()) > 0
	})
	res := metrics.AllMetrics()[0].ResourceMetrics().At(0).Resource().Attributes()
	if v, _ := res.Get("host.id"); v.Str() != "site-a:10.0.0.1" {
		t.Errorf("metric host.id = %q, want site-a:10.0.0.1", v.Str())
	}
	if v, _ := res.Get("snmp.device.namespace"); v.Str() != "site-a" {
		t.Errorf("metric snmp.device.namespace = %q", v.Str())
	}
	for _, ev := range entityEvents(logs) {
		if ev.id["host.id"] != "site-a:10.0.0.1" {
			t.Errorf("record host.id = %v, want site-a:10.0.0.1", ev.id["host.id"])
		}
		if ev.entityType == report.EntityDevice && ev.attrs["namespace"] != "site-a" {
			t.Errorf("device record namespace = %v", ev.attrs["namespace"])
		}
	}
}

func TestNamespaceOverrides(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Namespace = "hq"
	cfg.Subnets = []SubnetConfig{
		{Network: "10.0.0.0/24", Namespace: "branch"},
		{Network: "10.1.0.0/24"},
	}
	cfg.Devices = []DeviceConfig{{Endpoint: "10.9.9.1", Namespace: "dc"}}
	r := &snmpReceiver{cfg: cfg}

	cases := map[string]discovery.Device{
		"branch:10.0.0.5": {Address: "10.0.0.5", Subnet: "10.0.0.0/24"},
		"hq:10.1.0.5":     {Address: "10.1.0.5", Subnet: "10.1.0.0/24"},
		"dc:10.9.9.1":     {Address: "10.9.9.1", Static: true},
	}
	for want, device := range cases {
		if got := r.deviceInfo(device, "").ID; got != want {
			t.Errorf("device %s: ID %q, want %q", device.Address, got, want)
		}
	}
}

func TestNamespaceValidation(t *testing.T) {
	for _, ns := range []string{"", "a<b", "tab\there", strings.Repeat("x", 101)} {
		cfg := subnetTestConfig("10.0.0.0/30", "public")
		cfg.Namespace = ns
		if cfg.Validate() == nil {
			t.Errorf("namespace %q accepted", ns)
		}
	}
	cfg := subnetTestConfig("10.0.0.0/30", "public")
	cfg.Subnets[0].Namespace = "bad>"
	if cfg.Validate() == nil {
		t.Error("bad subnet namespace accepted")
	}
	if createDefaultConfig().(*Config).Namespace != "default" {
		t.Error("default namespace must be \"default\", as in Datadog")
	}
}

// An unreachable device costs one request per poll, as in the Datadog agent,
// and is reported unreachable.
func TestUnreachableDeviceCostsOneRequest(t *testing.T) {
	var mu sync.Mutex
	var sessions []*snmptest.Fake
	dial := func(snmp.ConnectionConfig) (snmp.Session, error) {
		dev := snmptest.New()
		dev.FailNext = 1000
		mu.Lock()
		sessions = append(sessions, dev)
		mu.Unlock()
		return dev, nil
	}
	cfg := createDefaultConfig().(*Config)
	cfg.CollectionInterval = 100 * time.Millisecond
	cfg.Devices = []DeviceConfig{{Endpoint: "10.0.0.9", Credentials: snmp.Credentials{Version: "v2c", Community: "public"}, Profile: "generic-device"}}
	r, err := newReceiver(cfg, receivertest.NewNopSettings(componentType), new(consumertest.MetricsSink))
	if err != nil {
		t.Fatal(err)
	}
	logs := new(consumertest.LogsSink)
	r.logsConsumer = logs
	r.dial = dial
	if err := r.Start(context.Background(), componenttest.NewNopHost()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Shutdown(context.Background()) })

	waitFor(t, 5*time.Second, "an unreachable device record", func() bool {
		for _, ev := range entityEvents(logs) {
			if ev.entityType == report.EntityDevice && ev.attrs["status"] == report.DeviceStatusUnreachable {
				return true
			}
		}
		return false
	})
	mu.Lock()
	defer mu.Unlock()
	for _, s := range sessions {
		if total := s.Gets + s.GetNexts + s.GetBulks; total > 1 {
			t.Fatalf("an unreachable device got %d requests in one poll, want 1", total)
		}
	}
}

func TestPollTimeoutDefaultsToInterval(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	if cfg.pollTimeout() != cfg.CollectionInterval {
		t.Errorf("poll timeout = %s, want collection_interval %s", cfg.pollTimeout(), cfg.CollectionInterval)
	}
	cfg.PollTimeout = 20 * time.Second
	if cfg.pollTimeout() != 20*time.Second {
		t.Errorf("poll timeout = %s, want 20s", cfg.pollTimeout())
	}
	cfg.Subnets = []SubnetConfig{{Network: "10.0.0.0/30", Credentials: snmp.Credentials{Version: "v2c", Community: "public"}}}
	for _, bad := range []func(*Config){
		func(c *Config) { c.Retries = -1 },
		func(c *Config) { c.Retries = 11 },
		func(c *Config) { c.Timeout = -time.Second },
		func(c *Config) { c.PollTimeout = -time.Second },
	} {
		c := *cfg
		bad(&c)
		if c.Validate() == nil {
			t.Error("invalid timeout/retries accepted")
		}
	}
}

// Two overlapping subnets in one namespace find the same device; it must be
// registered and polled once, not once per subnet under one host.id.
func TestOverlappingSubnetsPollDeviceOnce(t *testing.T) {
	f := newFleet("public", "10.0.0.1")
	cfg := subnetTestConfig("10.0.0.0/29", "public")
	cfg.Subnets = append(cfg.Subnets, SubnetConfig{
		Network:     "10.0.0.0/30",
		Credentials: snmp.Credentials{Version: "v2c", Community: "public"},
	})
	r, _ := startReceiver(t, cfg, f.dial)

	waitFor(t, 5*time.Second, "discovery", func() bool { return r.devices.Len() >= 1 })
	time.Sleep(500 * time.Millisecond) // a few rediscovery rounds of both subnets
	if n := r.devices.Len(); n != 1 {
		t.Errorf("registry holds %d entries for one device in two overlapping subnets, want 1", n)
	}

	// In different namespaces they are different devices, by design.
	cfg2 := subnetTestConfig("10.0.0.0/29", "public")
	cfg2.Subnets = append(cfg2.Subnets, SubnetConfig{
		Network:     "10.0.0.0/30",
		Namespace:   "other-site",
		Credentials: snmp.Credentials{Version: "v2c", Community: "public"},
	})
	r2, _ := startReceiver(t, cfg2, f.dial)
	waitFor(t, 5*time.Second, "both namespaces", func() bool { return r2.devices.Len() == 2 })
}

func TestDuplicateDevicesRejected(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	creds := snmp.Credentials{Version: "v2c", Community: "public"}
	cfg.Devices = []DeviceConfig{
		{Endpoint: "10.0.0.1", Credentials: creds},
		{Endpoint: "udp://10.0.0.1:161", Credentials: creds},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "already configured") {
		t.Errorf("duplicate device accepted: %v", err)
	}
}

// timeoutSession answers nothing, as a connected socket does when the device
// replies from another address.
type timeoutSession struct{ snmptest.Fake }

func (s *timeoutSession) GetNext([]string) (*gosnmp.SnmpPacket, error) {
	return nil, errors.New("request timeout (after 3 retries)")
}

// A device behind a virtual IP (VRRP/HSRP) or NAT replies from another
// address. The first poll falls back to an unconnected socket and keeps it,
// as the Datadog agent does.
func TestUnconnectedSocketFallback(t *testing.T) {
	f := newFleet("public", "10.0.0.1")
	var mu sync.Mutex
	connected, unconnected := 0, 0
	dial := func(cfg snmp.ConnectionConfig) (snmp.Session, error) {
		mu.Lock()
		defer mu.Unlock()
		if !cfg.UnconnectedUDP {
			connected++
			return &timeoutSession{}, nil
		}
		unconnected++
		return f.dial(cfg)
	}
	cfg := createDefaultConfig().(*Config)
	cfg.CollectionInterval = 100 * time.Millisecond
	cfg.Devices = []DeviceConfig{{Endpoint: "10.0.0.1", Credentials: snmp.Credentials{Version: "v2c", Community: "public"}}}
	r, sink := startReceiver(t, cfg, dial)

	waitFor(t, 5*time.Second, "metrics over the unconnected socket", func() bool { return len(sink.AllMetrics()) >= 2 })
	mu.Lock()
	defer mu.Unlock()
	if connected != 1 {
		t.Errorf("connected socket tried %d times, want once (the mode is kept)", connected)
	}
	if unconnected < 2 {
		t.Errorf("unconnected socket used %d times, want every poll", unconnected)
	}
	_ = r
}
