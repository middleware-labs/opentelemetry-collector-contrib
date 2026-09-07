// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"errors"
	"fmt"
	"net"
	"time"

	"go.opentelemetry.io/collector/config/confignet"
	"go.opentelemetry.io/collector/config/configopaque"
	"go.opentelemetry.io/collector/config/configtls"
	"go.opentelemetry.io/collector/scraper/scraperhelper"
	"go.uber.org/multierr"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

// Errors for missing required config parameters.
const (
	ErrNoUsername          = "invalid config: missing username"
	ErrNoPassword          = "invalid config: missing password" // #nosec G101 - not hardcoded credentials
	ErrNotSupported        = "invalid config: field '%s' not supported"
	ErrTransportsSupported = "invalid config: 'transport' must be 'tcp' or 'unix'"
	ErrHostPort            = "invalid config: 'endpoint' must be in the form <host>:<port> no matter what 'transport' is configured"
)

type TopQueryCollection struct {
	MaxRowsPerQuery        int64         `mapstructure:"max_rows_per_query"`
	TopNQuery              int64         `mapstructure:"top_n_query"`
	MaxExplainEachInterval int64         `mapstructure:"max_explain_each_interval"`
	QueryPlanCacheSize     int           `mapstructure:"query_plan_cache_size"`
	QueryPlanCacheTTL      time.Duration `mapstructure:"query_plan_cache_ttl"`
	// prevent unkeyed literal initialization
	_ struct{}
}

type QuerySampleCollection struct {
	MaxRowsPerQuery int64 `mapstructure:"max_rows_per_query"`
	// prevent unkeyed literal initialization
	_ struct{}
}

type SchemaCollectionConfig struct {
	Enabled            bool          `mapstructure:"enabled"`
	CollectionInterval time.Duration `mapstructure:"collection_interval"`
	RefreshInterval    time.Duration `mapstructure:"refresh_interval"`
	CollectExtensions  bool          `mapstructure:"collect_extensions"`
	CollectSettings    bool          `mapstructure:"collect_settings"`
	ContinueOnError    bool          `mapstructure:"continue_on_error"`
	ExcludeSchemas     []string      `mapstructure:"exclude_schemas"`
	IncludeSchemas     []string      `mapstructure:"include_schemas"`
	ExcludeTables      []string      `mapstructure:"exclude_tables"`
	IncludeTables      []string      `mapstructure:"include_tables"`
	// prevent unkeyed literal initialization
	_ struct{}
}

type Config struct {
	scraperhelper.ControllerConfig `mapstructure:",squash"`
	Username                       string                         `mapstructure:"username"`
	Password                       configopaque.String            `mapstructure:"password"`
	Databases                      []string                       `mapstructure:"databases"`
	ExcludeDatabases               []string                       `mapstructure:"exclude_databases"`
	confignet.AddrConfig           `mapstructure:",squash"`       // provides Endpoint and Transport
	configtls.ClientConfig         `mapstructure:"tls,omitempty"` // provides SSL details
	ConnectionPool                 `mapstructure:"connection_pool,omitempty"`
	ConnectionTimeouts             `mapstructure:"connection_timeouts,omitempty"`
	metadata.MetricsBuilderConfig  `mapstructure:",squash"`
	metadata.LogsBuilderConfig     `mapstructure:",squash"`
	QuerySampleCollection          `mapstructure:"query_sample_collection,omitempty"`
	TopQueryCollection             `mapstructure:"top_query_collection,omitempty"`
	SchemaCollection               SchemaCollectionConfig `mapstructure:"schema_collection,omitempty"`
}

// ConnectionTimeouts configures the per-connection guards applied through the
// DSN. Zero or unset means the built-in default; a negative value disables the
// guard entirely, which is not recommended and is why the fields are pointers.
type ConnectionTimeouts struct {
	// StatementTimeout bounds any single statement issued by the receiver.
	StatementTimeout *time.Duration `mapstructure:"statement_timeout,omitempty"`
	// LockTimeout bounds how long a receiver statement waits for a lock. Keep
	// this small: while we wait in a lock queue, the customer's own queries can
	// be queued behind us.
	LockTimeout *time.Duration `mapstructure:"lock_timeout,omitempty"`
	// IdleSessionTimeout terminates receiver sessions left idle outside a
	// transaction. Requires PostgreSQL 14 or later; ignored on older servers.
	IdleSessionTimeout *time.Duration `mapstructure:"idle_session_timeout,omitempty"`
}

type ConnectionPool struct {
	MaxIdleTime *time.Duration `mapstructure:"max_idle_time,omitempty"`
	MaxLifetime *time.Duration `mapstructure:"max_lifetime,omitempty"`
	MaxIdle     *int           `mapstructure:"max_idle,omitempty"`
	MaxOpen     *int           `mapstructure:"max_open,omitempty"`
	// MaxDatabases bounds how many per-database connection pools are kept open
	// at once. Pools for databases beyond this budget are closed once idle, so
	// a server with many databases does not retain an idle backend per
	// database indefinitely. The default database is always kept.
	MaxDatabases *int `mapstructure:"max_databases,omitempty"`
	// MaxTotalConnections caps connections across every database AND across
	// both the metrics and logs signals. PostgreSQL enforces a role's
	// CONNECTION LIMIT cluster-wide, counting every backend authenticated as
	// that role whatever database it reached, so this — not MaxDatabases — is
	// the setting that corresponds to the limit the server actually applies.
	//
	// Size it well below the role's limit. PostgreSQL's own enforcement is
	// approximate (concurrent connection attempts can each see a count under
	// the limit and all be admitted), so aiming at the limit exactly risks the
	// FATAL that this bound exists to prevent.
	MaxTotalConnections *int `mapstructure:"max_total_connections,omitempty"`
}

func (cfg *Config) Validate() error {
	var err error
	if cfg.Username == "" {
		err = multierr.Append(err, errors.New(ErrNoUsername))
	}
	if cfg.Password == "" {
		err = multierr.Append(err, errors.New(ErrNoPassword))
	}

	// The lib/pq module does not support overriding ServerName or specifying supported TLS versions
	if cfg.ServerName != "" {
		err = multierr.Append(err, fmt.Errorf(ErrNotSupported, "ServerName"))
	}
	if cfg.MaxVersion != "" {
		err = multierr.Append(err, fmt.Errorf(ErrNotSupported, "MaxVersion"))
	}
	if cfg.MinVersion != "" {
		err = multierr.Append(err, fmt.Errorf(ErrNotSupported, "MinVersion"))
	}

	switch cfg.Transport {
	case confignet.TransportTypeTCP, confignet.TransportTypeUnix:
		_, _, endpointErr := net.SplitHostPort(cfg.Endpoint)
		if endpointErr != nil {
			err = multierr.Append(err, errors.New(ErrHostPort))
		}
	default:
		err = multierr.Append(err, errors.New(ErrTransportsSupported))
	}

	return err
}
