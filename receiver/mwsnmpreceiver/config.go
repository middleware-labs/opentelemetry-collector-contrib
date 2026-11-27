// Package mwsnmpreceiver is an OpenTelemetry Collector receiver that
// discovers SNMP devices on configured subnets, selects a device profile from
// sysObjectID, and reports metrics under OpenTelemetry semantic conventions.
//
// It exists because the collection model of the existing snmpreceiver -- one
// receiver block per device, every metric hand-declared -- does not reach fleet
// scale. Here a subnet and a credential set are enough, and the ~240 embedded
// device profiles supply the OIDs.
package mwsnmpreceiver

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/collector/component"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/discovery"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/naming"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/report"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwsnmpreceiver/internal/snmp"
)

// Config is the receiver's configuration.
type Config struct {
	// Namespace is part of every device's ID (<namespace>:<ip>, as in Datadog
	// NDM), so two networks that reuse the same private addresses stay two
	// sets of devices. Give each site or collector its own. A subnet or
	// device can override it.
	Namespace string `mapstructure:"namespace"`

	// HostName controls host.name on the device's resource: "none" (default,
	// the device is identified by host.id and named by snmp.device.name),
	// "sys_name" (host.name = sysName, so each device may show up as a host),
	// or "device_id" (host.name = device:<namespace>:<ip>, like the Datadog
	// agent's use_device_id_as_hostname).
	HostName string `mapstructure:"host_name"`

	// CollectionInterval is how often each device is polled. Polls are spread
	// across the interval rather than issued together.
	CollectionInterval time.Duration `mapstructure:"collection_interval"`

	// Timeout and Retries apply to each SNMP request. Every retry waits the
	// same timeout, so one unanswered request costs Timeout x (Retries+1):
	// 8 s with the defaults, which are the Datadog agent's.
	Timeout time.Duration `mapstructure:"timeout"`
	Retries int           `mapstructure:"retries"`

	// PollTimeout caps one device's whole poll. 0 means collection_interval,
	// so a poll never overruns the device's next slot. A poll is also cut short
	// after 3 failed requests in a row.
	PollTimeout time.Duration `mapstructure:"poll_timeout"`

	// Pollers bounds how many devices are polled concurrently.
	Pollers int `mapstructure:"pollers"`

	Discovery DiscoveryConfig `mapstructure:"discovery"`

	// Subnets are scanned for devices.
	Subnets []SubnetConfig `mapstructure:"subnets"`
	// Devices are polled without discovery.
	Devices []DeviceConfig `mapstructure:"devices"`

	Profiles ProfilesConfig   `mapstructure:"profiles"`
	Naming   NamingConfig     `mapstructure:"naming"`
	Fetch    snmp.FetchConfig `mapstructure:"fetch"`

	// Storage names a storage extension used to persist the discovered device
	// set. Without it, a restart rescans every subnet before monitoring resumes.
	Storage *component.ID `mapstructure:"storage"`

	// Metadata tunes the inventory records (device, interface, IP address)
	// sent as entity events when the receiver is used in a logs pipeline. It
	// has no effect in a metrics-only pipeline.
	Metadata MetadataConfig `mapstructure:"metadata"`
}

// MetadataConfig tunes the inventory flow.
type MetadataConfig struct {
	// ResendInterval is how often every record is sent again even when nothing
	// changed. Between full sends only changed records go out, such as a port
	// going down. The value is also put on each record as otel.entity.interval
	// so the backend can mark a record stale when it stops arriving. 0 sends
	// every record on every poll, which is what the Datadog agent does.
	ResendInterval time.Duration `mapstructure:"resend_interval"`

	// Interfaces sends one record per interface, from the profile's
	// metadata.interface section. It adds the interface metadata columns
	// (ifName, ifAlias, ifDescr, ifPhysAddress, ...) to each poll.
	Interfaces bool `mapstructure:"interfaces"`

	// IPAddresses sends one record per IP address configured on the device,
	// from IP-MIB ipAddrTable.
	IPAddresses bool `mapstructure:"ip_addresses"`

	// Topology sends one record per LLDP neighbour (CDP when the device has
	// no LLDP neighbours). On by default, as Datadog's collect_topology.
	Topology bool `mapstructure:"topology"`

	// VPN sends one record per Cisco IPsec tunnel with its routed subnets.
	// Off by default, as Datadog's collect_vpn: it walks the device's route
	// tables, which are large on a router carrying a full routing table
	// (fetch.max_rows_per_column caps the walk).
	VPN bool `mapstructure:"vpn"`
}

// DiscoveryConfig tunes subnet scanning.
type DiscoveryConfig struct {
	// RediscoveryInterval is how often subnets are rescanned, which is how new
	// devices are found and dead ones age out.
	RediscoveryInterval time.Duration `mapstructure:"rediscovery_interval"`
	// Workers bounds concurrent probes during a scan.
	Workers int `mapstructure:"workers"`
	// AllowedFailures is how many consecutive failures a device tolerates before
	// being forgotten.
	AllowedFailures int `mapstructure:"allowed_failures"`
}

// SubnetConfig is one subnet to scan. A single credential set can be given
// inline; several are given under authentications and tried in order.
type SubnetConfig struct {
	Network string `mapstructure:"network"`
	Port    uint16 `mapstructure:"port"`

	snmp.Credentials `mapstructure:",squash"`

	// Authentications are tried in order per device, so one subnet can hold
	// devices with different credentials.
	Authentications []snmp.Credentials `mapstructure:"authentications"`

	// IgnoredAddresses are never probed.
	IgnoredAddresses []string `mapstructure:"ignored_addresses"`
	// MaxHosts overrides the guard against over-wide subnets.
	MaxHosts int `mapstructure:"max_hosts"`

	// Namespace overrides the receiver-level namespace for this subnet.
	Namespace string `mapstructure:"namespace"`
}

// DeviceConfig is one explicitly configured device.
type DeviceConfig struct {
	// Endpoint is host, host:port, or udp://host:port.
	Endpoint string `mapstructure:"endpoint"`

	snmp.Credentials `mapstructure:",squash"`

	// Profile pins a profile by name instead of detecting one from sysObjectID.
	Profile string `mapstructure:"profile"`

	// Namespace overrides the receiver-level namespace for this device.
	Namespace string `mapstructure:"namespace"`
}

// ProfilesConfig locates user profiles.
type ProfilesConfig struct {
	// UserDir holds profiles that shadow the embedded ones by name. A Datadog
	// snmp.d/profiles directory can be pointed at directly.
	UserDir string `mapstructure:"user_dir"`
}

// NamingConfig selects how metrics are named.
type NamingConfig struct {
	// Scheme is semconv, datadog_compat or both.
	Scheme string `mapstructure:"scheme"`
	// FallbackNamespace prefixes generated names for unmodelled symbols.
	FallbackNamespace string `mapstructure:"fallback_namespace"`
	// SystemNamespaceForDeviceOS puts device cpu and memory metrics under
	// system.*, which is the default. Set false to send them to the fallback
	// namespace instead. See the README for why system.* is the right home.
	SystemNamespaceForDeviceOS bool `mapstructure:"system_namespace_for_device_os"`
}

// Defaults chosen to be safe on a large fleet rather than fastest on one device.
const (
	defaultCollectionInterval  = 60 * time.Second
	defaultRediscoveryInterval = time.Hour
	defaultTimeout             = snmp.DefaultTimeout
	defaultRetries             = 3
	maxRetries                 = 10
	defaultPollers             = 50
	defaultDiscoveryWorkers    = 10
	defaultAllowedFailures     = 3
	defaultPort                = 161
	defaultMetadataResend      = 10 * time.Minute
	defaultNamespace           = "default"
	maxNamespaceLength         = 100
)

func createDefaultConfig() component.Config {
	return &Config{
		Namespace:          defaultNamespace,
		HostName:           report.HostNameNone,
		CollectionInterval: defaultCollectionInterval,
		Timeout:            defaultTimeout,
		Retries:            defaultRetries,
		Pollers:            defaultPollers,
		Discovery: DiscoveryConfig{
			RediscoveryInterval: defaultRediscoveryInterval,
			Workers:             defaultDiscoveryWorkers,
			AllowedFailures:     defaultAllowedFailures,
		},
		Naming: NamingConfig{
			Scheme:                     string(naming.SchemeSemconv),
			FallbackNamespace:          "snmp",
			SystemNamespaceForDeviceOS: true,
		},
		Fetch: snmp.FetchConfig{
			OIDBatchSize:       snmp.DefaultOIDBatchSize,
			BulkMaxRepetitions: snmp.DefaultBulkMaxRepetitions,
			MaxRowsPerColumn:   snmp.DefaultMaxRowsPerColumn,
		},
		Metadata: MetadataConfig{
			ResendInterval: defaultMetadataResend,
			Interfaces:     true,
			IPAddresses:    true,
			Topology:       true,
			VPN:            false,
		},
	}
}

// Validate reports every configuration problem at once, so a user fixing a
// config sees the whole list rather than one error per run.
func (c *Config) Validate() error {
	var errs []error

	if len(c.Subnets) == 0 && len(c.Devices) == 0 {
		errs = append(errs, errors.New("configure at least one entry under subnets or devices"))
	}
	if c.CollectionInterval <= 0 {
		errs = append(errs, errors.New("collection_interval must be positive"))
	}
	if c.Timeout < 0 {
		errs = append(errs, errors.New("timeout cannot be negative"))
	}
	if c.Retries < 0 || c.Retries > maxRetries {
		errs = append(errs, fmt.Errorf("retries must be between 0 and %d", maxRetries))
	}
	switch c.HostName {
	case report.HostNameNone, report.HostNameSysName, report.HostNameDeviceID, "":
	default:
		errs = append(errs, fmt.Errorf("host_name %q is not one of none, sys_name, device_id", c.HostName))
	}
	if c.PollTimeout < 0 {
		errs = append(errs, errors.New("poll_timeout cannot be negative"))
	}
	if c.Pollers < 0 {
		errs = append(errs, errors.New("pollers cannot be negative"))
	}
	if c.Discovery.RediscoveryInterval < 0 {
		errs = append(errs, errors.New("discovery.rediscovery_interval cannot be negative"))
	}
	if c.Discovery.AllowedFailures < 0 {
		errs = append(errs, errors.New("discovery.allowed_failures cannot be negative"))
	}
	if c.Metadata.ResendInterval < 0 {
		errs = append(errs, errors.New("metadata.resend_interval cannot be negative"))
	}
	if c.Namespace == "" {
		errs = append(errs, errors.New("namespace cannot be empty"))
	} else if err := validateNamespace(c.Namespace); err != nil {
		errs = append(errs, fmt.Errorf("namespace: %w", err))
	}

	switch naming.Scheme(c.Naming.Scheme) {
	case naming.SchemeSemconv, naming.SchemeDatadogCompat, naming.SchemeBoth, "":
	default:
		errs = append(errs, fmt.Errorf(
			"naming.scheme %q is not one of semconv, datadog_compat, both", c.Naming.Scheme))
	}

	for i, subnet := range c.Subnets {
		if err := subnet.validate(); err != nil {
			errs = append(errs, fmt.Errorf("subnets[%d]: %w", i, err))
		}
	}
	seen := map[string]int{}
	for i, device := range c.Devices {
		if err := device.validate(); err != nil {
			errs = append(errs, fmt.Errorf("devices[%d]: %w", i, err))
			continue
		}
		// The same address twice would be polled twice under one host.id.
		host, _, _ := parseEndpoint(device.Endpoint)
		if first, dup := seen[host]; dup {
			errs = append(errs, fmt.Errorf("devices[%d]: %s is already configured as devices[%d]", i, host, first))
			continue
		}
		seen[host] = i
	}
	return errors.Join(errs...)
}

func (s SubnetConfig) validate() error {
	var errs []error
	if s.Network == "" {
		errs = append(errs, errors.New("network is required"))
	} else if _, err := discovery.ExpandCIDR(s.Network, s.MaxHosts); err != nil {
		errs = append(errs, err)
	}

	credentials := s.credentials()
	if len(credentials) == 0 {
		errs = append(errs, errors.New("set a community or user inline, or list authentications"))
	}
	for i, creds := range credentials {
		if err := creds.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("authentications[%d]: %w", i, err))
		}
	}
	if s.Namespace != "" {
		if err := validateNamespace(s.Namespace); err != nil {
			errs = append(errs, fmt.Errorf("namespace: %w", err))
		}
	}
	for _, address := range s.IgnoredAddresses {
		if net.ParseIP(address) == nil {
			errs = append(errs, fmt.Errorf("ignored_addresses: %q is not an IP address", address))
		}
	}
	return errors.Join(errs...)
}

// credentials returns the credential sets to try, inline form first.
func (s SubnetConfig) credentials() []snmp.Credentials {
	var out []snmp.Credentials
	if s.Community != "" || s.User != "" {
		out = append(out, s.Credentials)
	}
	out = append(out, s.Authentications...)
	return out
}

func (d DeviceConfig) validate() error {
	var errs []error
	if d.Endpoint == "" {
		errs = append(errs, errors.New("endpoint is required"))
	} else if _, _, err := parseEndpoint(d.Endpoint); err != nil {
		errs = append(errs, err)
	}
	if err := d.Credentials.Validate(); err != nil {
		errs = append(errs, err)
	}
	if d.Namespace != "" {
		if err := validateNamespace(d.Namespace); err != nil {
			errs = append(errs, fmt.Errorf("namespace: %w", err))
		}
	}
	return errors.Join(errs...)
}

// parseEndpoint accepts host, host:port and udp://host:port.
func parseEndpoint(endpoint string) (host string, port uint16, err error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", 0, errors.New("endpoint is empty")
	}

	if strings.Contains(endpoint, "://") {
		parsed, parseErr := url.Parse(endpoint)
		if parseErr != nil {
			return "", 0, fmt.Errorf("parse endpoint %q: %w", endpoint, parseErr)
		}
		if parsed.Scheme != "udp" {
			return "", 0, fmt.Errorf("endpoint %q: only the udp scheme is supported", endpoint)
		}
		endpoint = parsed.Host
	}

	if !strings.Contains(endpoint, ":") {
		return endpoint, defaultPort, nil
	}
	// A bare IPv6 address ("2001:db8::1") or a bracketed one without a port
	// ("[2001:db8::1]") uses the default port; only "[addr]:port" carries one.
	if ip := net.ParseIP(strings.Trim(endpoint, "[]")); ip != nil && ip.To4() == nil &&
		!strings.Contains(endpoint, "]:") {
		return strings.Trim(endpoint, "[]"), defaultPort, nil
	}
	hostPart, portPart, splitErr := net.SplitHostPort(endpoint)
	if splitErr != nil {
		return "", 0, fmt.Errorf("parse endpoint %q: %w", endpoint, splitErr)
	}
	parsedPort, convErr := strconv.ParseUint(portPart, 10, 16)
	if convErr != nil {
		return "", 0, fmt.Errorf("endpoint %q: invalid port: %w", endpoint, convErr)
	}
	if hostPart == "" {
		return "", 0, fmt.Errorf("endpoint %q: host is empty", endpoint)
	}
	return hostPart, uint16(parsedPort), nil
}

// namingOptions converts config to registry options. Empty fields are left
// empty: naming.New applies the defaults, so they live in one place.
func (c *Config) namingOptions() naming.Options {
	return naming.Options{
		Scheme:                     naming.Scheme(c.Naming.Scheme),
		FallbackNamespace:          c.Naming.FallbackNamespace,
		SystemNamespaceForDeviceOS: c.Naming.SystemNamespaceForDeviceOS,
	}
}

// subnetConfigs converts config subnets to discovery subnets.
func (c *Config) subnetConfigs() []discovery.SubnetConfig {
	out := make([]discovery.SubnetConfig, 0, len(c.Subnets))
	for _, subnet := range c.Subnets {
		port := subnet.Port
		if port == 0 {
			port = defaultPort
		}
		out = append(out, discovery.SubnetConfig{
			Network:          subnet.Network,
			Port:             port,
			Credentials:      subnet.credentials(),
			IgnoredAddresses: subnet.IgnoredAddresses,
			Timeout:          c.Timeout,
			Retries:          c.Retries,
			MaxHosts:         subnet.MaxHosts,
		})
	}
	return out
}

// validateNamespace applies the Datadog agent's namespace rules
// (pkg/snmp/utils.NormalizeNamespace), as errors rather than silent rewrites:
// at most 100 characters, no control characters, no '<' or '>'.
func validateNamespace(ns string) error {
	if len(ns) > maxNamespaceLength {
		return fmt.Errorf("%q is longer than %d characters", ns, maxNamespaceLength)
	}
	if strings.ContainsAny(ns, "\x00\n\r\t<>") {
		return fmt.Errorf("%q contains a control character, '<' or '>'", ns)
	}
	if strings.TrimSpace(ns) == "" {
		return errors.New("cannot be blank")
	}
	return nil
}

// pollTimeout is the deadline for one device's poll.
func (c *Config) pollTimeout() time.Duration {
	if c.PollTimeout > 0 {
		return c.PollTimeout
	}
	return c.CollectionInterval
}
