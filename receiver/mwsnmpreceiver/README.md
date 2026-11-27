# Network Device Receiver

| Status    |                       |
| --------- | --------------------- |
| Stability | alpha: metrics, logs  |
| Type      | `mwsnmp`              |

Polls SNMP network devices at fleet scale. Declare subnets and credentials; the
receiver discovers devices, selects a device profile from `sysObjectID`, and
emits metrics under OpenTelemetry semantic conventions.

## Why this exists

The existing `snmpreceiver` models one receiver instance per device, with every
metric hand-declared. Monitoring two thousand devices means two thousand
configuration blocks and no SNMP probing. This receiver instead takes the three
things that make the Datadog agent's SNMP integration usable at scale — subnet
autodiscovery, `sysObjectID`-based profiles, and near-zero per-device
configuration — and puts an OpenTelemetry-native output stage behind them.

Roughly 25 lines of configuration covers two subnets and thousands of devices.
See [`examples/two-subnets.yaml`](./examples/two-subnets.yaml).

## Configuration

```yaml
receivers:
  mwsnmp:
    collection_interval: 60s
    subnets:
      - network: 10.10.0.0/24
        version: v2c
        community: ${env:SNMP_COMMUNITY}
```

| Field | Default | Description |
| --- | --- | --- |
| `namespace` | `default` | Part of every device ID, `<namespace>:<ip>` (as in Datadog NDM). Give each site or collector its own, so networks that reuse private addresses stay separate. `subnets[].namespace` and `devices[].namespace` override it. At most 100 characters, no control characters, `<` or `>`. A device found by two overlapping subnets of one namespace is polled once (the first subnet to find it keeps it). |
| `host_name` | `none` | Whether the device's resource gets `host.name`. `none`: no, the device is identified by `host.id` and named by `snmp.device.name` (as the Datadog agent does by default). `sys_name`: host.name = sysName (falls back to the IP), so each device can appear as a host; beware default sysNames such as `Switch` colliding. `device_id`: host.name = `device:<namespace>:<ip>`, like Datadog's `use_device_id_as_hostname`. |
| `collection_interval` | `60s` | How often each device is polled. Polls are spread across the interval rather than issued together. |
| `timeout` / `retries` | `2s` / `3` | Per-request SNMP timeout and retry count (the Datadog agent's defaults). Every retry waits the same timeout, so one unanswered request costs `timeout x (retries+1)`: 8 s by default. `retries` is 0 to 10. |
| `poll_timeout` | `collection_interval` | Deadline for one device's whole poll, also applied to the request in flight. Independently, a poll stops after 3 failed requests in a row, and each poll starts with one reachability request (GETNEXT on the system group); a device that does not answer it is reported unreachable without further requests. |
| `pollers` | `50` | Maximum devices polled concurrently. An unreachable device holds a poller for at most `timeout x (retries+1)` (8 s by default), a device that goes quiet mid-poll for about 3 times that. |
| `discovery.rediscovery_interval` | `1h` | How often subnets are rescanned. Set to `0` to scan once at startup only. |
| `discovery.workers` | `10` | Concurrent probes during a scan. |
| `discovery.allowed_failures` | `3` | Consecutive failures tolerated before a device is forgotten. |
| `subnets[]` | — | CIDR plus credentials. See below. |
| `devices[]` | — | Explicitly configured devices, never aged out. |
| `profiles.user_dir` | — | Profiles that shadow the embedded ones by name. |
| `naming.scheme` | `semconv` | `semconv`, `datadog_compat` or `both`. The Datadog names (`snmp.<symbol>`) carry the raw SNMP value, as the Datadog agent sends it: `snmp.ifOperStatus` is the 1–7 enum, `snmp.ifHighSpeed` is Mbit/s, `snmp.cpu.usage` a percent. Only the semconv names get the registry's scale, value maps and state sets. |
| `naming.fallback_namespace` | `snmp` | Prefix for generated names of unmodelled symbols. |
| `naming.system_namespace_for_device_os` | `true` | Emit device cpu/memory as `system.*`. Set `false` for the fallback namespace. |
| `fetch.oid_batch_size` | `10` | OIDs per GET or GETBULK. Shrinks automatically if a device rejects a request. |
| `fetch.bulk_max_repetitions` | `10` | GETBULK max-repetitions. |
| `fetch.max_rows_per_column` | `10000` | Cap on one table walk; truncation is logged, never silent. |
| `storage` | — | A storage extension used to persist the discovered device set. |

### Credentials

A single set can be given inline on a subnet. Several go under
`authentications` and are tried in order per device; the set that answers is
remembered, so later polls do not retry the others.

SNMPv1, v2c and v3 are supported. For v3, the security level is derived from
which secrets are present — supplying an `auth_key` gives authNoPriv, adding a
`priv_key` gives authPriv — so it cannot be stated inconsistently. Auth
protocols: MD5, SHA, SHA224, SHA256, SHA384, SHA512. Privacy protocols: DES,
AES, AES192, AES256, and the Cisco `AES192C`/`AES256C` key-extension variants.

### Persistence

Without a `storage` extension the device set is in-memory, and a restart
rescans every subnet before monitoring resumes. With one, devices come back
immediately along with the credential index and profile that already worked.
Statically configured devices are deliberately not persisted, so removing one
from the configuration actually removes it.

## Profiles

Profiles are read in Datadog's SNMP profile format, which is parsed natively
rather than converted. About 240 upstream profiles are embedded (Cisco, Juniper,
Arista, Fortinet, Palo Alto, F5, APC, NetApp, Meraki and more), including the
abstract ones such as `_base.yaml` and `_generic-if.yaml` that vendor profiles
extend. An existing Datadog `snmp.d/profiles` directory can be pointed at
unchanged via `profiles.user_dir`; a user profile shadows an embedded one of the
same name entirely.

Profile selection reads `sysObjectID` on first contact and picks the profile
whose `sysobjectid` glob is the most specific match. `generic-device.yaml`
claims `1.3.6.1.4.*`, so an unrecognised enterprise device still gets basic
monitoring rather than nothing.

### Custom profiles

`profiles.user_dir` is loaded on top of the embedded set, and a user profile
shadows an embedded one of the same name entirely. So correcting a shipped
profile means copying it out of `internal/profile/default_profiles/`, editing it,
and dropping it in `user_dir` under the same filename — there is no merge to
reason about.

A minimal profile for a device the library does not cover:

```yaml
# /etc/otelcol/snmp.d/profiles/acme-switch.yaml
extends:
  - _base.yaml          # sysName, sysDescr, sysObjectID, uptime
  - _generic-if.yaml    # IF-MIB interface metrics and metadata

sysobjectid: 1.3.6.1.4.1.99999.1.*

metadata:
  device:
    fields:
      vendor:
        value: "acme"

metrics:
  # A scalar OID.
  - MIB: ACME-MIB
    symbol:
      OID: 1.3.6.1.4.1.99999.2.1.0
      name: cpu.usage
  # A table: one datapoint per row, tagged from the row index.
  - MIB: ACME-MIB
    table:
      OID: 1.3.6.1.4.1.99999.3.1
      name: acmePsuTable
    symbols:
      - OID: 1.3.6.1.4.1.99999.3.1.1.4
        name: psu.temperature
    metric_tags:
      - index: 1
        tag: psu
```

Choose symbol names deliberately, because the naming registry is keyed by symbol
name and ignores the MIB. Reusing a name the registry already models is how a
custom profile inherits a semantic-convention mapping for free: `cpu.usage`
above is emitted as `system.cpu.utilization`, rescaled from percent. A name the
registry does not know is never dropped — it gets a deterministic fallback
derived from the MIB and symbol, so `psu.temperature` under `ACME-MIB` arrives as
`snmp.acme.psu.temperature`. Grep `internal/naming/registry.yaml` for the
symbols already mapped.

Validate before deploying — an unresolvable `extends`, a malformed table or a
`sysobjectid` that collides with a shipped profile all surface here rather than
as quietly missing metrics:

```console
$ go run ./cmd/snmpprofilecheck -user-dir /etc/otelcol/snmp.d/profiles
$ go run ./cmd/snmpprofilecheck -profile acme-switch -coverage
$ go run ./cmd/snmpprofilecheck -sysobjectid 1.3.6.1.4.1.99999.1.7
```

`snmpprofilecheck` is not in the release archives, so validating against a
released binary needs this repository checked out at the matching tag.

## Metric naming

Profiles say *what to collect*; a separate naming registry says *what to call
it*. This split is deliberate: it means the upstream profile library can be
resynced without touching naming, and naming can follow the semantic
conventions without forking 240 files.

Three tiers:

1. **`hw.*` — the hardware semantic conventions.** The right home for interface,
   sensor, fan, PSU, voltage, temperature and status metrics; `hw.network.*` is
   explicitly defined as covering a physical interface on a switch, router or
   firewall. Device identity lives on the **resource**, and component identity
   in the `hw.id` / `hw.name` / `hw.type` datapoint attributes, as the
   conventions require. One resource per device.
2. **`system.*` — device-OS metrics, on by default.** `cpu.usage` and `memory.*`
   are the two most-referenced metric families in the profile library, and `hw.*`
   defines nothing for either: `hw.cpu.*` is only `speed` and `speed.limit`,
   `hw.memory.*` only `size`. Meanwhile `system.cpu.utilization`,
   `system.memory.usage` (with `system.memory.state`) and
   `system.memory.utilization` are exactly the right shapes.

   The objection is that the system conventions say that namespace is for metrics
   collected from *within* the target system, and SNMP polling is external. But an
   SNMP agent **is** in-system instrumentation merely transported over the wire —
   the same reading by which a remotely scraped node_exporter yields `system.*` —
   and the alternative is inventing three `hw.*` metrics that do not exist. So
   `system.*` is the default; `naming.system_namespace_for_device_os: false` opts
   out.

   This costs little to adopt because Datadog already normalises every vendor's
   CPU and memory OIDs onto the same symbol names — Cisco's `cpmCPUTotal5minRev`
   and HOST-RESOURCES' `hrProcessorLoad` are both `cpu.usage` — so a handful of
   entries covers the whole library.
3. **`snmp.*` — generated fallback.** For the long tail of vendor symbols, a
   deterministic `snmp.<mib>.<symbol>` derived only from the MIB and symbol
   names, so it is stable across profile resyncs. Naming a namespace after the
   transport is against semconv guidance, but it honestly signals "raw
   MIB-derived, not yet modelled" and is configurable.

Current coverage, measurable with `snmpprofilecheck`: **94%** of `_generic-if`'s
symbols (the profile 136 of 240 others inherit) and about **21%** of symbol
references across the whole corpus. The remaining tail resolves to tier 3.

Every mapping and every transformation is documented per metric in
[`documentation.md`](./documentation.md).

### Worked IF-MIB mapping

| Profile symbol | Metric | Instrument | Unit | Notes |
| --- | --- | --- | --- | --- |
| `ifHCInOctets` / `ifHCOutOctets` | `hw.network.io` | Counter | `By` | `network.io.direction` distinguishes them |
| `ifHCIn*Pkts` / `ifHCOut*Pkts` | `hw.network.packets` | Counter | `{packet}` | `network.io.cast` for unicast/multicast/broadcast is ours; no convention models it |
| `ifInErrors` / `ifOutErrors` | `hw.errors` | Counter | `{error}` | `error.type=error` |
| `ifInDiscards` / `ifOutDiscards` | `system.network.packet.dropped` | Counter | `{packet}` | A discard is not an error; `hw.*` has no dropped-packet metric |
| `ifOperStatus` | `hw.network.up` | UpDownCounter | `1` | `up(1)` maps to 1, everything else 0 |
| `ifAdminStatus` | `hw.status` | UpDownCounter | `1` | State-set: one datapoint per state |
| `ifHighSpeed` | `hw.network.bandwidth.limit` | UpDownCounter | `By/s` | Mbit/s × 125000 |
| `ifSpeed` | `hw.network.bandwidth.limit` | UpDownCounter | `By/s` | bit/s ÷ 8; lower priority than `ifHighSpeed` |
| derived | `hw.network.bandwidth.utilization` | Gauge | `1` | A **fraction**, not a percentage |

Three mechanics beyond a flat symbol-to-name table:

- **State-set fan-out.** `hw.status` follows the OpenMetrics StateSet pattern, so
  an SNMP enum becomes one datapoint per possible state (1 for the active state,
  0 for the others) rather than one enum-valued gauge.
- **Type-dispatched sensors.** `entPhySensorValue` is one column whose meaning
  depends on the sibling `entPhySensorType` column, so it routes per row to
  `hw.temperature`, `hw.voltage`, `hw.fan.speed` or `hw.power`.
- **Priority.** When two symbols map to the same metric *and* the same
  attributes — a 32-bit and a 64-bit form of one counter, or `ifSpeed` alongside
  `ifHighSpeed` — priority picks one instead of emitting conflicting duplicate
  series.
- **Component identity outside `hw.*`.** A component metric with no `hw.*` home
  still needs to be joinable to the ones that have it, so an entry can opt into
  emitting `hw.id`, `hw.name` and `network.interface.name`. Interface discards use
  this.

### Sensor scaling

ENTITY-SENSOR-MIB publishes each sensor's exponent in `entPhySensorScale` (an
SI-prefix enum where `units(9)` is 10⁰ and each step is three orders of
magnitude) and `entPhySensorPrecision` (decimal places already folded into the
integer). Both are applied, so a reading of 425 with precision 1 is 42.5 °C and
334 with precision 2 is 3.34 V — correct per device, with no divisor to configure.

Cisco's older CISCO-ENTITY-SENSOR-MIB is deliberately **not** mapped. Its
`entSensorValue` and `entSensorType` columns are collected by profiles, but
`entSensorScale` and `entSensorPrecision` are not, so the magnitude cannot be
derived and a device reporting deci-celsius would read ten times high. A wrong
number under a conventional metric name is trusted; the generated `snmp.*` name is
not, which makes it the safer place for the value until a profile collects those
two columns.

### Rates

Datadog computes rates agent-side. This receiver emits cumulative Sums and lets
the pipeline derive rates, so `monotonic_count_and_rate` collapses to a single
Sum. Dashboards ported from a Datadog agent need `rate()`-style queries, or a
`cumulativetodelta` + `deltatorate` processor pair.

The one exception is `hw.network.bandwidth.utilization`, which stays
receiver-side because it needs a join between the traffic counters and
`ifHighSpeed` that a backend cannot easily express. It requires two polls before
it reports, and is suppressed across a counter reset rather than reporting a
false spike.

## Inventory (logs pipeline)

Put the receiver in a logs pipeline as well and it also sends the device
inventory: one record per device, interface, IP address, LLDP/CDP neighbour
link and Cisco IPsec tunnel. This is the
equivalent of the Datadog agent's NDM metadata payload. Metrics carry the
history; these records carry the current state of each item, which the backend
upserts as one row per item.

```yaml
service:
  pipelines:
    metrics:
      receivers: [mwsnmp]
      exporters: [otlp]
    logs/inventory:
      receivers: [mwsnmp]   # same config: one shared poller, devices polled once
      exporters: [otlp]
```

| Option | Default | Meaning |
| --- | --- | --- |
| `metadata.resend_interval` | `10m` | Every record is sent in full this often. In between, only records that changed are sent (a port going down, a new IP). `0` sends everything on every poll, like Datadog. |
| `metadata.interfaces` | `true` | Interface records, from the profile's `metadata.interface` section. Adds the ifName, ifAlias, ifDescr, ifPhysAddress, ifAdminStatus, ifOperStatus and ifType walks to each poll. |
| `metadata.ip_addresses` | `true` | IP address records, from IP-MIB `ipAddrTable`. |
| `metadata.topology` | `true` | Neighbour link records from LLDP-MIB `lldpRemTable`, or CISCO-CDP-MIB `cdpCacheTable` when there are no LLDP neighbours. Same default as Datadog's `collect_topology`. |
| `metadata.vpn` | `false` | Cisco IPsec tunnel records from `cipSecTunnelTable`, with routed subnets from the route tables and the tunnel interface from TUNNEL-MIB. Off by default like Datadog's `collect_vpn`: it walks the route table, which is large on a router with a full table (`fetch.max_rows_per_column` caps it). |

The inventory columns are only fetched when a logs pipeline uses the receiver,
so a metrics-only setup polls exactly what it did before.

### Record format

Each record is a log record shaped as an OpenTelemetry entity event, the same
format `k8sclusterreceiver` uses for Kubernetes objects. The scope has
`otel.entity.event_as_log: true`, and the resource attributes are the device's
metric resource attributes.

| Attribute | Value |
| --- | --- |
| `otel.entity.event.type` | `entity_state`, or `entity_delete` when a device is dropped after `discovery.allowed_failures` failed polls |
| `otel.entity.type` | `network.device`, `network.interface`, `network.ip_address`, `network.link` or `network.vpn_tunnel` |
| `otel.entity.id` | the item key, using the names the metrics already carry, so records join to metrics directly. The device ID `host.id` is `<namespace>:<ip>`, the same value as the metrics' `host.id` |
| `otel.entity.interval` | `metadata.resend_interval` in ms; the backend can mark an item stale when it stops arriving |
| `otel.entity.attributes` | the fields below |

| Entity type | `otel.entity.id` | `otel.entity.attributes` |
| --- | --- | --- |
| `network.device` | `host.id` | `integration`, `ip_address`, `port`, `subnet`, `sys_object_id`, `profile`, the profile's `metadata.device` fields (`name`, `description`, `location`, `vendor`, `model`, `serial_number`, `os_name`, `os_version`, ...), `device_type`, `status` (1 reachable, 2 unreachable), `diagnoses` |
| `network.interface` | `host.id`, `interface_index` | the profile's `metadata.interface` fields (`name`, `alias`, `description`, `mac_address`, `admin_status`, `oper_status`, `type`), the id tags (`interface`), `is_physical` |
| `network.ip_address` | `host.id`, `interface_index`, `ip_address` | `prefixlen` |
| `network.link` | `host.id`, `link_id` (`<local port>.<remote index>`) | `source_type` (`lldp`/`cdp`), `local.interface_index` (resolved local port), `local.interface.id`, `local.interface.id_type`, `remote.device.id`, `remote.device.id_type`, `remote.device.name`, `remote.device.description`, `remote.device.ip_address`, `remote.interface.id`, `remote.interface.id_type`, `remote.interface.description` |
| `network.vpn_tunnel` | `host.id`, `local_outside_ip`, `remote_outside_ip` | `protocol` (`ipsec`), `status` (`active`/`destroy`/`unknown`), `interface_index`, `route_addresses`, `life_size`, `life_time` |

`diagnoses` is a list of `{severity, code, message}` using the Datadog agent's
codes: `SNMP_FAILED_TO_OPEN_CONNECTION`, `SNMP_FAILED_TO_POLL_DEVICE`,
`SNMP_FAILED_TO_DETECT_PROFILE`, `SNMP_HIGH_CHECK_DURATION` (poll over 30 s). An
empty list means no problem, which is how an earlier problem is cleared.

On a failed poll only the device record is sent, with `status: 2` and the
diagnosis. It repeats the device fields from the last successful poll, so the
row is not blanked. Interface and IP records are left as last sent.

Example interface record (attributes of the log record):

```json
{
  "otel.entity.event.type": "entity_state",
  "otel.entity.type": "network.interface",
  "otel.entity.id": {"host.id": "default:10.0.0.1", "interface_index": 7},
  "otel.entity.interval": 600000,
  "otel.entity.attributes": {
    "name": "te1/0/7", "interface": "te1/0/7", "alias": "uplink-core",
    "description": "TGE1/0/7", "mac_address": "82:8a:8c:2f:f8:36",
    "admin_status": 1, "oper_status": 1, "type": 6, "is_physical": true
  }
}
```

### Sending and limits

Each device's records are sent in their own `ConsumeLogs` call right after that
device is polled, and polls are spread across the collection interval, so there
is no single startup batch of every device. A 500-port switch is about 500
records, well under the 4 MiB OTLP gRPC default. Use the exporter's
`sending_queue` and the `memory_limiter` processor to bound memory. A rejected
batch is not marked as sent, so it goes out again on the next poll.

The link's remote side is what the neighbour advertises. Matching it to a
monitored device (by `remote.device.ip_address` or the chassis MAC) is the
backend's job, as in Datadog; an unmatched neighbour is an unmonitored node.

Topology and VPN are ported from the Datadog agent, which defines their OIDs in
code rather than in the profiles
(`pkg/collector/corechecks/snmp/internal/checkconfig/config_metadata.go`,
`report/report_device_metadata.go`). Here they are in
`internal/profile/inventory_oids.go`, `internal/report/topology.go` and
`internal/report/vpn.go`.

### Not yet sent

Ping status.

## Counter resets

Cumulative series (`hw.network.io`, errors, packets) carry a start timestamp.
When a counter goes down -- a device reboot, a 32-bit wrap, an interface
re-created -- the series gets a new start timestamp (the previous point's time),
and when `sysUpTime` goes backwards every series of the device does. Consumers
therefore see a reset, never a negative rate. The Datadog agent handles the same
case by dropping negative deltas, since it sends deltas rather than cumulatives.

## Self-telemetry

| Metric | Description |
| --- | --- |
| `otelcol_networkdevice_devices_monitored` | Devices currently in the registry |
| `otelcol_networkdevice_discovered_devices` | Devices found by scans |
| `otelcol_networkdevice_poll_duration` | Per-device poll duration, seconds |
| `otelcol_networkdevice_poll_errors` | Failed polls |
| `otelcol_networkdevice_poll_skipped` | Poll slots skipped because every poller was busy; raise `pollers` if this grows |
| `otelcol_networkdevice_pdus_sent` | SNMP request packets sent |
| `otelcol_networkdevice_inventory_records` | Inventory records sent on the logs pipeline |

## Scale notes

- Memory kept per device, measured with `MWSNMP_MEMORY=1000x48,100x500 go test -run TestInventoryMemory -v .`:
  a 48-port switch is about 52 KB (profile 3 KB, per-series state 47 KB,
  inventory 2.6 KB); a 500-port switch is about 780 KB. Measured end to end
  against simulated 48-port switches, the whole collector's RSS grows by about
  0.17 MB per device (382 MB at 2,000 devices), against about 0.86 MB per
  device for the Datadog agent 7.86 (1.9 GB at 2,000). Per-series state is keyed by a 64-bit hash and dropped after
  5 polls without the series, as in the Datadog agent.

- Column walks request only the columns a profile needs, never the table root.
- GETBULK batches several columns into one PDU, falling back to GETNEXT for
  SNMPv1 and for devices whose GETBULK is broken.
- Batch sizes shrink when a device answers `tooBig` and recover afterwards.
- OIDs a device does not implement are pruned after the first poll, so an
  unsupported OID is asked for once rather than forever.
- Polls are spread deterministically across the collection interval, keeping PDU
  rate flat instead of bursting at the top of each cycle. A device keeps its slot
  across restarts.
- Subnets wider than 65536 addresses are refused rather than attempted; raise
  `max_hosts` per subnet to override.

## Known gaps

- The `hw.*` conventions are Development status and `network.io.direction` is
  Release Candidate. Expect at least one breaking rename before they stabilise.
- Registry coverage beyond IF-MIB, ENTITY-SENSOR-MIB and the common UPS symbols
  is incomplete; the remaining vendor symbols resolve to tier 3.
- `hw.cpu.utilization`, `hw.memory.usage` and `hw.memory.utilization` do not
  exist upstream, so the two most-referenced metric families in the profile
  library are reported under `system.*` instead. Proposing them for `hw.*` is
  still worthwhile; if accepted, this becomes a rename behind a feature gate.
- `network.io.cast` is ours: no convention models the packet-class split. It is a
  candidate for an upstream proposal.
- Only standard MIBs are mapped. Vendor MIBs -- firewall session counts, wireless
  client counts, vendor CPU and sensor tables -- resolve to generated names, since
  a vendor-specific metric name would be no more conventional than the generated
  one.
- Cisco entity sensors and CISCO-ENVMON-MIB states resolve to generated names
  rather than `hw.*`, as described under Sensor scaling.
- The registry maps only symbols the shipped profiles actually collect, enforced by
  a test. A user profile collecting something else -- the 32-bit `ifInOctets`, say
  -- lands in the fallback tier until an entry is added.
- A multi-homed device answering on several addresses is reported as several
  devices. Collapsing them needs a serial or engine-ID based identity, since
  sysObjectID is shared across every unit of a model.
- SNMP traps are out of scope.

## Attribution

This receiver started as a copy of
[tsuga-dev/networkdevicereceiver](https://github.com/tsuga-dev/networkdevicereceiver)
(Apache-2.0) at commit `0c2876d`. The inventory flow, the logs pipeline and the
`mwsnmp` type were added here.


The profile schema and fetch strategy are ported from
[DataDog/datadog-agent](https://github.com/DataDog/datadog-agent) (Apache-2.0).
The embedded profile library is copied unmodified from
[DataDog/integrations-core](https://github.com/DataDog/integrations-core)
(BSD-3-Clause; see `internal/profile/default_profiles/LICENSE`), at the commit
recorded in `internal/profile/default_profiles/UPSTREAM-SHA`. See `NOTICE` for
the full attribution.

Datadog does not endorse this receiver.
