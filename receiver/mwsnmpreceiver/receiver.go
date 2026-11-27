package mwsnmpreceiver

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/extension/xextension/storage"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/discovery"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/naming"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/profile"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/report"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/snmp"
)

// schedulerTick is how often the scheduler looks for due devices. It bounds the
// timing error of a poll, and is capped so a short collection interval still
// gets reasonable resolution.
const schedulerTick = time.Second

// snmpReceiver polls a fleet of SNMP devices.
type snmpReceiver struct {
	cfg    *Config
	logger *zap.Logger

	// metricsConsumer and logsConsumer are set by the factory for the
	// pipelines that use this receiver; either may be nil. A logs consumer
	// turns on the inventory flow (inventory.go).
	metricsConsumer consumer.Metrics
	logsConsumer    consumer.Logs

	profiles *profile.Store
	naming   *naming.Registry
	devices  *discovery.Registry
	scanner  *discovery.Scanner
	fetchCfg snmp.FetchConfig

	// dial is injectable so tests can drive the receiver without a network.
	dial discovery.Dialer

	// staticHosts are the addresses of the devices: entries, so a subnet scan
	// does not register a second identity for a device the operator already
	// configured explicitly.
	staticHosts map[string]struct{}

	mu    sync.Mutex
	state map[string]*deviceState

	telemetry telemetry

	cancel      context.CancelFunc
	wg          sync.WaitGroup
	storageDone func(context.Context) error
}

// deviceState is the per-device state that survives between polls: the compiled
// profile, the metric builder holding cumulative start times, and what the
// inventory flow last sent.
type deviceState struct {
	deviceID    string
	builder     *report.Builder
	compiled    *profile.Compiled
	profileName string

	// socketMode is connected or unconnected UDP, decided on the first poll.
	socketMode int

	inventory inventoryTracker
	// lastDeviceAttrs and lastResource are from the last successful poll, so a
	// failed poll can still send a complete device record. nil until then.
	lastDeviceAttrs map[string]any
	lastResource    pcommon.Map
}

// telemetry holds the receiver's own instruments, mirroring the Datadog agent's
// snmp.devices_monitored and check-duration metrics.
type telemetry struct {
	devicesMonitored  metric.Int64Gauge
	discoveredDevices metric.Int64Counter
	pollDuration      metric.Float64Histogram
	pollErrors        metric.Int64Counter
	pdusSent          metric.Int64Counter
	inventoryRecords  metric.Int64Counter
	// pollSkipped counts slots skipped because every poller was busy. It is
	// separate from pollErrors so starvation (too few pollers for the fleet,
	// or many slow devices) is visible rather than hidden among failures.
	pollSkipped metric.Int64Counter
}

func newReceiver(cfg *Config, set receiver.Settings, next consumer.Metrics) (*snmpReceiver, error) {
	meter := set.TelemetrySettings.MeterProvider.Meter(report.ScopeName)

	var tel telemetry
	var err error
	if tel.devicesMonitored, err = meter.Int64Gauge("otelcol_networkdevice_devices_monitored"); err != nil {
		return nil, err
	}
	if tel.discoveredDevices, err = meter.Int64Counter("otelcol_networkdevice_discovered_devices"); err != nil {
		return nil, err
	}
	if tel.pollDuration, err = meter.Float64Histogram("otelcol_networkdevice_poll_duration",
		metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if tel.pollErrors, err = meter.Int64Counter("otelcol_networkdevice_poll_errors"); err != nil {
		return nil, err
	}
	if tel.pdusSent, err = meter.Int64Counter("otelcol_networkdevice_pdus_sent"); err != nil {
		return nil, err
	}
	if tel.inventoryRecords, err = meter.Int64Counter("otelcol_networkdevice_inventory_records"); err != nil {
		return nil, err
	}
	if tel.pollSkipped, err = meter.Int64Counter("otelcol_networkdevice_poll_skipped"); err != nil {
		return nil, err
	}

	return &snmpReceiver{
		cfg:             cfg,
		logger:          set.TelemetrySettings.Logger,
		metricsConsumer: next,
		fetchCfg:        cfg.Fetch,
		dial:            snmp.NewSession,
		state:           map[string]*deviceState{},
		telemetry:       tel,
	}, nil
}

// Start prepares the profile library and device registry, then begins scanning
// and polling.
func (r *snmpReceiver) Start(ctx context.Context, host component.Host) error {
	profiles, err := profile.NewStore(r.cfg.Profiles.UserDir)
	if err != nil {
		return fmt.Errorf("load profiles: %w", err)
	}
	r.profiles = profiles

	registry, err := naming.New(r.cfg.namingOptions())
	if err != nil {
		return fmt.Errorf("build naming registry: %w", err)
	}
	r.naming = registry

	persistence, err := r.storageClient(ctx, host)
	if err != nil {
		return err
	}
	r.devices = discovery.NewRegistry(r.cfg.Discovery.AllowedFailures, persistence)

	if restored, err := r.devices.Load(ctx); err != nil {
		// A corrupt or unreadable registry must not stop the receiver: the next
		// scan repopulates it.
		r.logger.Warn("could not restore the device registry; will rescan", zap.Error(err))
	} else if restored > 0 {
		r.logger.Info("restored devices from storage", zap.Int("devices", restored))
	}

	if err := r.seedStaticDevices(); err != nil {
		return err
	}
	// A registry persisted before overlapping subnets were de-duplicated may
	// hold the same device twice.
	r.removeDuplicateDevices()

	r.scanner = &discovery.Scanner{
		Workers: r.cfg.Discovery.Workers,
		Dial:    r.dial,
	}

	// The run context is deliberately independent of the start context, which is
	// cancelled once startup finishes.
	runCtx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel

	if len(r.cfg.Subnets) > 0 {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.discoveryLoop(runCtx)
		}()
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.pollLoop(runCtx)
	}()

	r.logger.Info("network device receiver started",
		zap.Int("subnets", len(r.cfg.Subnets)),
		zap.Int("static_devices", len(r.cfg.Devices)),
		zap.Int("profiles", len(profiles.Names())),
		zap.Int("curated_symbols", registry.Curated()))
	return nil
}

// Shutdown stops the loops and persists the device set so the next start does
// not have to rescan.
func (r *snmpReceiver) Shutdown(ctx context.Context) error {
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()

	var err error
	if r.devices != nil {
		if saveErr := r.devices.Save(ctx); saveErr != nil {
			err = saveErr
		}
	}
	if r.storageDone != nil {
		if closeErr := r.storageDone(ctx); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}

// storageClient resolves the configured storage extension, if any.
func (r *snmpReceiver) storageClient(ctx context.Context, host component.Host) (discovery.Persistence, error) {
	if r.cfg.Storage == nil {
		return nil, nil
	}
	ext, ok := host.GetExtensions()[*r.cfg.Storage]
	if !ok {
		return nil, fmt.Errorf("storage extension %q is not configured in the service", r.cfg.Storage)
	}
	provider, ok := ext.(storage.Extension)
	if !ok {
		return nil, fmt.Errorf("extension %q is not a storage extension", r.cfg.Storage)
	}
	client, err := provider.GetClient(ctx, component.KindReceiver, *r.cfg.Storage, "")
	if err != nil {
		return nil, fmt.Errorf("get storage client: %w", err)
	}
	r.storageDone = client.Close
	return client, nil
}

// seedStaticDevices registers the explicitly configured devices.
func (r *snmpReceiver) seedStaticDevices() error {
	r.staticHosts = make(map[string]struct{}, len(r.cfg.Devices))
	for i, device := range r.cfg.Devices {
		host, port, err := parseEndpoint(device.Endpoint)
		if err != nil {
			return fmt.Errorf("devices[%d]: %w", i, err)
		}
		r.staticHosts[host] = struct{}{}
		r.devices.Upsert(discovery.Device{
			ID:          discovery.DeviceID(host, ""),
			Address:     host,
			Port:        port,
			ProfileName: device.Profile,
			Static:      true,
			LastSeen:    time.Now(),
		})
	}

	// A static device that also lives inside a scanned subnet may have been
	// discovered (and persisted) under a second, subnet-scoped identity. Two
	// entries mean the device is polled twice per interval and its metrics are
	// emitted under two different resources, so the static entry wins.
	for _, dev := range r.devices.Devices() {
		if dev.Static {
			continue
		}
		if _, isStatic := r.staticHosts[dev.Address]; isStatic {
			r.devices.Remove(dev.ID)
		}
	}
	return nil
}

// discoveryLoop scans every subnet immediately, then on the rediscovery
// interval, which is how new devices appear and dead ones age out.
func (r *snmpReceiver) discoveryLoop(ctx context.Context) {
	r.scanAll(ctx)

	interval := r.cfg.Discovery.RediscoveryInterval
	if interval <= 0 {
		// Discovery explicitly disabled after the initial scan.
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.scanAll(ctx)
		}
	}
}

func (r *snmpReceiver) scanAll(ctx context.Context) {
	for _, subnet := range r.cfg.subnetConfigs() {
		if ctx.Err() != nil {
			return
		}
		started := time.Now()
		found, err := r.scanner.Scan(ctx, subnet)
		if err != nil && ctx.Err() == nil {
			r.logger.Error("subnet scan failed",
				zap.String("network", subnet.Network), zap.Error(err))
		}

		// Exported IDs (<namespace>:<ip>) already owned by another registry
		// entry. Overlapping subnets would otherwise register the same device
		// once per subnet and poll it twice under one host.id, with
		// conflicting cumulative series. The first subnet to find it keeps it;
		// if that entry ages out, the next scan of the other subnet takes over.
		owners := r.exportedOwners()

		registered := 0
		for _, device := range found {
			// A statically configured device is already polled via its static
			// entry; registering the scanned duplicate would poll it twice
			// under a second resource identity.
			if _, isStatic := r.staticHosts[device.Address]; isStatic {
				continue
			}
			exported := r.deviceInfo(device, "").ID
			if owner, taken := owners[exported]; taken && owner != device.ID {
				continue
			}
			owners[exported] = device.ID
			registered++
			r.devices.Upsert(device)

			// Resolve the profile now rather than on first poll. Matching is a
			// local lookup against the embedded library, so this costs nothing
			// and makes the persisted registry self-describing: a restart knows
			// each device's profile without re-deriving it.
			if device.SysObjectID == "" {
				continue
			}
			matched, err := r.profiles.Match(device.SysObjectID)
			if err != nil {
				r.logger.Warn("no profile matches device",
					zap.String("device", device.Address),
					zap.String("sys_object_id", device.SysObjectID),
					zap.Error(err))
				continue
			}
			r.devices.SetProfile(device.ID, device.SysObjectID, matched)
		}
		r.telemetry.discoveredDevices.Add(ctx, int64(registered))
		r.logger.Info("subnet scanned",
			zap.String("network", subnet.Network),
			zap.Int("found", len(found)),
			zap.Int("registered", registered),
			zap.Duration("took", time.Since(started)))
	}

	// Persist after each round so a crash does not lose a completed scan.
	if err := r.devices.Save(ctx); err != nil {
		r.logger.Warn("could not persist the device registry", zap.Error(err))
	}
}

// pollLoop schedules device polls, spreading them across the collection interval
// rather than issuing them all at once.
//
// Datadog polls every device of a subnet when the check runs, which produces a
// burst; spreading the load keeps PDU rate and CPU flat, which matters at
// thousands of devices.
func (r *snmpReceiver) pollLoop(ctx context.Context) {
	pollers := r.cfg.Pollers
	if pollers <= 0 {
		pollers = defaultPollers
	}
	interval := r.cfg.CollectionInterval

	// inFlight tracks devices whose poll is still running, so a device that
	// takes longer than the interval is not dispatched to a second worker: two
	// concurrent polls would race on the device's shared builder and profile
	// state, and emit duplicate cumulative streams.
	var inFlightMu sync.Mutex
	inFlight := map[string]struct{}{}

	jobs := make(chan discovery.Device)
	var workers sync.WaitGroup
	for range pollers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for device := range jobs {
				r.pollDevice(ctx, device)
				inFlightMu.Lock()
				delete(inFlight, device.ID)
				inFlightMu.Unlock()
			}
		}()
	}

	tick := schedulerTick
	if interval < tick {
		tick = interval
	}
	ticker := time.NewTicker(tick)

	defer func() {
		ticker.Stop()
		close(jobs)
		workers.Wait()
	}()

	nextDue := map[string]time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			devices := r.devices.Devices()
			r.telemetry.devicesMonitored.Record(ctx, int64(len(devices)))

			live := make(map[string]struct{}, len(devices))
			for _, device := range devices {
				live[device.ID] = struct{}{}

				due, known := nextDue[device.ID]
				if !known {
					// Offset the first poll deterministically so devices spread
					// across the interval and stay spread across restarts.
					nextDue[device.ID] = now.Add(offsetFor(device.ID, interval))
					continue
				}
				if now.Before(due) {
					continue
				}

				inFlightMu.Lock()
				_, busy := inFlight[device.ID]
				if !busy {
					inFlight[device.ID] = struct{}{}
				}
				inFlightMu.Unlock()
				if busy {
					// The previous poll is still running. Leave the slot due so
					// the device is dispatched at the first tick after the poll
					// returns, rather than concurrently with it.
					continue
				}

				// Advance in whole intervals to keep the device's phase, even if
				// a poll was delayed past its slot.
				for !due.After(now) {
					due = due.Add(interval)
				}
				nextDue[device.ID] = due

				select {
				case jobs <- device:
				case <-ctx.Done():
					return
				default:
					// Every poller is busy. Skipping is better than queueing
					// without bound; the next slot will try again.
					inFlightMu.Lock()
					delete(inFlight, device.ID)
					inFlightMu.Unlock()
					r.logger.Debug("pollers saturated, skipping this slot",
						zap.String("device", device.Address))
					r.telemetry.pollSkipped.Add(ctx, 1)
				}
			}

			for id := range nextDue {
				if _, still := live[id]; !still {
					delete(nextDue, id)
				}
			}
		}
	}
}

// offsetFor spreads a device's first poll deterministically within the interval,
// so the same device keeps its slot across restarts.
func offsetFor(id string, interval time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return time.Duration(h.Sum64() % uint64(interval))
}

// pollDevice performs one device's collection cycle.
func (r *snmpReceiver) pollDevice(ctx context.Context, device discovery.Device) {
	if ctx.Err() != nil {
		return
	}
	started := time.Now()

	credentials, err := r.credentialsFor(device)
	if err != nil {
		// Counted as a failed poll so a device whose subnet was removed from
		// the config is aged out by its failure budget instead of being
		// re-polled and re-persisted forever.
		r.failPoll(ctx, device, fmt.Errorf("no credentials: %w", err), diagOpenConnection, report.DeviceStatusUnreachable)
		return
	}

	// One deadline for the SNMP side of the poll, also handed to gosnmp so an
	// in-flight request is abandoned rather than waited out. Handing the
	// results to the pipeline uses ctx, so a poll that used its whole budget
	// still delivers what it collected.
	pollCtx, cancel := context.WithTimeout(ctx, r.cfg.pollTimeout())
	defer cancel()

	session, reachErr, err := r.openSession(pollCtx, device, credentials)
	if err != nil {
		r.failPoll(ctx, device, err, diagOpenConnection, report.DeviceStatusUnreachable)
		return
	}
	defer func() { _ = session.Close() }()

	// One cheap request first, as the Datadog agent does: a device that does
	// not answer it is reported unreachable at once, instead of being tried
	// request by request through its whole OID list.
	if reachErr != nil {
		r.failPoll(ctx, device, fmt.Errorf("not reachable: %w", reachErr), diagPollDevice, report.DeviceStatusUnreachable)
		return
	}

	state, err := r.ensureProfile(pollCtx, session, device)
	if err != nil {
		if errors.Is(err, errNoSysObjectID) {
			// The device did not answer at all.
			r.failPoll(ctx, device, err, diagPollDevice, report.DeviceStatusUnreachable)
		} else {
			// It answered, but no profile fits.
			r.failPoll(ctx, device, err, diagDetectProfile, report.DeviceStatusReachable)
		}
		return
	}

	scalars, columns := state.compiled.FetchOIDs()
	values, fetchReport, fetchErr := snmp.Fetch(pollCtx, session, scalars, columns, r.fetchCfg)
	if fetchReport != nil {
		r.telemetry.pdusSent.Add(ctx, int64(fetchReport.PDUs))
		// Stop asking for OIDs this device does not implement.
		state.compiled.MarkMissing(fetchReport.MissingOIDs...)
		for _, column := range fetchReport.TruncatedColumns {
			r.logger.Warn("table walk truncated by max_rows_per_column",
				zap.String("device", device.Address), zap.String("column", column))
		}
	}
	if fetchErr != nil {
		// A partial fetch still yields metrics, so this is logged rather than
		// treated as a failed poll.
		r.logger.Debug("partial fetch", zap.String("device", device.Address), zap.Error(fetchErr))
	}
	// A device that answered nothing is not healthy, whatever the transport did.
	// Absent OIDs are individually tolerated, so this total is the only signal
	// distinguishing an unreachable device from a sparsely populated one.
	if values == nil || values.Count() == 0 {
		if fetchErr == nil {
			fetchErr = fmt.Errorf("device answered none of the %d scalar and %d column OIDs",
				len(scalars), len(columns))
		}
		r.failPoll(ctx, device, fetchErr, diagPollDevice, report.DeviceStatusUnreachable)
		return
	}

	info := r.deviceInfo(device, state.profileName)
	now := time.Now()
	metrics, buildReport, buildErr := state.builder.Build(info, state.compiled, values, now)
	if buildErr != nil {
		r.logger.Debug("build reported problems",
			zap.String("device", device.Address), zap.Error(buildErr))
	}

	var metricsErr error
	if r.metricsConsumer != nil && metrics.DataPointCount() > 0 {
		metricsErr = r.metricsConsumer.ConsumeMetrics(ctx, metrics)
	}

	// The inventory goes out even if the metrics pipeline refused this poll:
	// the device answered, and its status and ports are still worth knowing.
	if r.inventoryEnabled() {
		inv := state.builder.BuildInventory(info, state.compiled, values, r.inventoryOptions())
		r.reportInventory(ctx, state, inv, time.Since(started), now)
	}

	if metricsErr != nil {
		r.telemetry.pollErrors.Add(ctx, 1)
		r.logger.Error("pipeline rejected metrics",
			zap.String("device", device.Address), zap.Error(metricsErr))
		return
	}

	r.devices.MarkSuccess(device.ID, time.Now())
	r.telemetry.pollDuration.Record(ctx, time.Since(started).Seconds())
	r.logger.Debug("polled device",
		zap.String("device", device.Address),
		zap.String("profile", state.profileName),
		zap.Int("datapoints", buildReport.DataPoints),
		zap.Int("unmapped_metrics", len(buildReport.GeneratedMetrics)))
}

// failPoll records a failed poll and drops the device once its budget is spent.
// With the inventory flow on, it also sends the device's status and diagnosis,
// or an entity_delete once the device is dropped.
func (r *snmpReceiver) failPoll(ctx context.Context, device discovery.Device, cause error,
	diag report.Diagnosis, status int64) {

	r.telemetry.pollErrors.Add(ctx, 1)
	if dropped := r.devices.MarkFailure(device.ID); dropped {
		r.logger.Info("device dropped after repeated failures",
			zap.String("device", device.Address), zap.Error(cause))
		if r.inventoryEnabled() {
			r.reportDeleted(ctx, device, r.stateFor(device).lastResource)
		}
		r.forgetState(device.ID)
		return
	}
	r.logger.Debug("poll failed", zap.String("device", device.Address), zap.Error(cause))
	if r.inventoryEnabled() {
		r.reportFailure(ctx, device, diag, status)
	}
}

// credentialsFor returns the credential set that answered for this device.
func (r *snmpReceiver) credentialsFor(device discovery.Device) (snmp.Credentials, error) {
	if device.Static {
		for _, configured := range r.cfg.Devices {
			host, _, err := parseEndpoint(configured.Endpoint)
			if err == nil && host == device.Address {
				return configured.Credentials, nil
			}
		}
		return snmp.Credentials{}, fmt.Errorf("device %s is no longer configured", device.Address)
	}

	for _, subnet := range r.cfg.Subnets {
		if subnet.Network != device.Subnet {
			continue
		}
		credentials := subnet.credentials()
		if device.AuthIndex < len(credentials) {
			return credentials[device.AuthIndex], nil
		}
		// The credential list shrank since discovery; fall back to the first.
		if len(credentials) > 0 {
			return credentials[0], nil
		}
	}
	return snmp.Credentials{}, fmt.Errorf("subnet %s is no longer configured", device.Subnet)
}

// ensureProfile resolves and compiles the device's profile, caching the result.
// The profile is re-detected if the device's sysObjectID changes, which happens
// when hardware is replaced at the same address.
func (r *snmpReceiver) ensureProfile(ctx context.Context, session snmp.Session, device discovery.Device) (*deviceState, error) {
	state := r.stateFor(device)

	wanted := device.ProfileName
	if wanted == "" {
		sysObjectID := device.SysObjectID
		if sysObjectID == "" {
			// A static device has not been probed, so identify it now.
			detected, ok := discovery.ProbeSysObjectID(ctx, session)
			if !ok {
				return nil, errNoSysObjectID
			}
			sysObjectID = detected
		}
		matched, err := r.profiles.Match(sysObjectID)
		if err != nil {
			return nil, fmt.Errorf("select profile: %w", err)
		}
		wanted = matched
		r.devices.SetProfile(device.ID, sysObjectID, wanted)
	}

	if state.compiled != nil && state.profileName == wanted {
		return state, nil
	}

	def, err := r.profiles.Resolve(wanted)
	if err != nil {
		return nil, fmt.Errorf("resolve profile %q: %w", wanted, err)
	}
	compiled, err := profile.CompileWith(def, r.compileOptions())
	if err != nil {
		return nil, fmt.Errorf("compile profile %q: %w", wanted, err)
	}
	state.compiled = compiled
	state.profileName = wanted
	r.logger.Info("profile selected",
		zap.String("device", device.Address),
		zap.String("profile", wanted),
		zap.Int("scalar_oids", len(compiled.ScalarOIDs)),
		zap.Int("column_oids", len(compiled.ColumnOIDs)))
	return state, nil
}

// errNoSysObjectID means a device with no known profile did not answer the
// sysObjectID probe, i.e. it is unreachable rather than unsupported.
var errNoSysObjectID = errors.New("could not read sysObjectID to select a profile")

// stateFor returns the device's state, creating it on first use. State is keyed
// by the registry ID; deviceID is the exported <namespace>:<ip> ID.
func (r *snmpReceiver) stateFor(device discovery.Device) *deviceState {
	exported := r.deviceInfo(device, "").ID
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.state[device.ID]
	if !ok {
		state = &deviceState{deviceID: exported, builder: report.NewBuilder(r.naming)}
		r.state[device.ID] = state
	}
	return state
}

func (r *snmpReceiver) forgetState(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.state, id)
}

// Socket modes, decided on a device's first poll and then kept, as in the
// Datadog agent (devicecheck/connection_manager.go, firstConnect).
const (
	socketUndecided = iota
	socketConnected
	socketUnconnected
)

// openSession dials and connects a session and checks the device answers. On
// the first poll, a reachability timeout on a connected socket is retried once
// on an unconnected socket: a device answering from another address (VRRP or
// HSRP virtual IP, NAT, multi-homed) is invisible to a connected socket. The
// mode that is decided is kept for later polls.
func (r *snmpReceiver) openSession(ctx context.Context, device discovery.Device,
	credentials snmp.Credentials) (snmp.Session, error, error) {

	state := r.stateFor(device)
	dial := func(unconnected bool) (snmp.Session, error) {
		session, err := r.dial(snmp.ConnectionConfig{
			Host:           device.Address,
			Port:           device.Port,
			Credentials:    credentials,
			Timeout:        r.cfg.Timeout,
			Retries:        r.cfg.Retries,
			Context:        ctx,
			UnconnectedUDP: unconnected,
		})
		if err != nil {
			return nil, fmt.Errorf("open session: %w", err)
		}
		if err := session.Connect(); err != nil {
			return nil, fmt.Errorf("connect: %w", err)
		}
		return session, nil
	}

	session, err := dial(state.socketMode == socketUnconnected)
	if err != nil {
		return nil, nil, err
	}
	reachErr := checkReachable(session)
	if state.socketMode != socketUndecided {
		return session, reachErr, nil
	}
	if reachErr == nil || !isTimeout(reachErr) {
		// Working, or failing for a reason another socket cannot fix (an
		// authentication error, say).
		state.socketMode = socketConnected
		return session, reachErr, nil
	}

	alt, err := dial(true)
	if err != nil {
		state.socketMode = socketConnected
		return session, reachErr, nil
	}
	if altErr := checkReachable(alt); altErr == nil {
		r.logger.Info("device answers only on an unconnected socket; using one from now on",
			zap.String("device", device.Address))
		state.socketMode = socketUnconnected
		_ = session.Close()
		return alt, nil, nil
	}
	_ = alt.Close()
	state.socketMode = socketConnected
	return session, reachErr, nil
}

// isTimeout matches gosnmp's timeout errors, which are not net.Errors.
func isTimeout(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "timeout")
}

// reachabilityOID is walked once per poll to see whether the device answers at
// all. GETNEXT on the system group's root always has a successor on a working
// SNMP agent.
const reachabilityOID = ".1.3.6.1.2.1.1"

func checkReachable(session snmp.Session) error {
	packet, err := session.GetNext([]string{reachabilityOID})
	if err != nil {
		return err
	}
	if packet == nil || len(packet.Variables) == 0 {
		return errors.New("empty response")
	}
	return nil
}

// exportedOwners maps each exported device ID to the registry entry polling it.
func (r *snmpReceiver) exportedOwners() map[string]string {
	owners := map[string]string{}
	for _, dev := range r.devices.Devices() {
		exported := r.deviceInfo(dev, "").ID
		if _, taken := owners[exported]; !taken || dev.Static {
			owners[exported] = dev.ID
		}
	}
	return owners
}

// removeDuplicateDevices keeps one registry entry per exported device ID.
func (r *snmpReceiver) removeDuplicateDevices() {
	owners := r.exportedOwners()
	for _, dev := range r.devices.Devices() {
		if owners[r.deviceInfo(dev, "").ID] != dev.ID {
			r.devices.Remove(dev.ID)
		}
	}
}
