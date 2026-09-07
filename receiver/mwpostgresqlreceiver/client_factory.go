// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/lib/pq"
	"go.opentelemetry.io/collector/featuregate"
	"go.uber.org/multierr"
)

const connectionPoolGateID = "receiver.postgresql.connectionPool"

var connectionPoolGate = featuregate.GlobalRegistry().MustRegister(
	connectionPoolGateID,
	featuregate.StageBeta,
	featuregate.WithRegisterDescription("Use of connection pooling"),
	featuregate.WithRegisterFromVersion("0.96.0"),
	featuregate.WithRegisterReferenceURL("https://github.com/open-telemetry/opentelemetry-collector-contrib/issues/30831"),
)

// defaultMaxPooledDatabases bounds how many per-database pools a factory keeps
// open at once. See poolClientFactory for why a bound is needed at all.
const defaultMaxPooledDatabases = 16

var errFactoryClosed = errors.New("postgresql client factory is closed")

// resolveTimeouts turns the user's optional overrides into the effective
// per-connection timeouts. An unset field takes the default; an explicitly
// configured non-positive value disables that guard, which is why the config
// fields are pointers — "not set" and "set to zero" must mean different things.
func resolveTimeouts(cfg *Config) connectionTimeouts {
	t := defaultConnectionTimeouts
	if cfg.ConnectionTimeouts.StatementTimeout != nil {
		t.statement = *cfg.ConnectionTimeouts.StatementTimeout
	}
	if cfg.ConnectionTimeouts.LockTimeout != nil {
		t.lock = *cfg.ConnectionTimeouts.LockTimeout
	}
	if cfg.ConnectionTimeouts.IdleSessionTimeout != nil {
		t.idleSession = *cfg.ConnectionTimeouts.IdleSessionTimeout
	}
	return t
}

type postgreSQLClientFactory interface {
	getClient(database string) (client, error)
	close() error
}

// defaultClientFactory creates one PG connection per call
type defaultClientFactory struct {
	baseConfig postgreSQLConfig
}

func newDefaultClientFactory(cfg *Config) *defaultClientFactory {
	return &defaultClientFactory{
		baseConfig: postgreSQLConfig{
			username: cfg.Username,
			password: string(cfg.Password),
			address:  cfg.AddrConfig,
			tls:      cfg.ClientConfig,
			timeouts: resolveTimeouts(cfg),
		},
	}
}

func (d *defaultClientFactory) getClient(database string) (client, error) {
	db, err := getDB(d.baseConfig, database)
	if err != nil {
		return nil, err
	}
	return &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: db.Close}, nil
}

func (*defaultClientFactory) close() error {
	return nil
}

// pooledDB is one database's connection pool together with the bookkeeping
// needed to decide when it may be closed.
type pooledDB struct {
	db *sql.DB
	// refs counts clients handed out for this pool and not yet closed. A pool
	// with refs > 0 is in use and must never be evicted.
	refs int
	// releasedAt is when refs last dropped to zero; the idle pool released
	// longest ago is the first evicted.
	releasedAt time.Time
}

// poolClientFactory keeps a connection pool per database so that consecutive
// queries against the same database reuse a connection instead of paying for a
// fresh TCP, TLS and authentication handshake each.
//
// The number of pools is bounded. Each pool holds its idle connections open for
// the configured max_idle_time, so an unbounded set of pools against a server
// with many databases retains one or more idle server backends per database,
// indefinitely — enough to exhaust the monitoring role's connection limit, at
// which point every further query fails with "too many connections" and the
// scrape cannot complete at all. The bound turns that into graceful
// degradation: the hot pools stay warm, and databases beyond the budget are
// opened, used and closed, which is what this receiver did before pooling.
type poolClientFactory struct {
	sync.Mutex
	baseConfig         postgreSQLConfig
	poolConfig         *ConnectionPool
	pool               map[string]*pooledDB
	closed             bool
	maxPooledDatabases int
	now                func() time.Time
}

func newPoolClientFactory(cfg *Config) *poolClientFactory {
	poolCfg := cfg.ConnectionPool
	maxPooled := defaultMaxPooledDatabases
	if poolCfg.MaxDatabases != nil && *poolCfg.MaxDatabases > 0 {
		maxPooled = *poolCfg.MaxDatabases
	}
	return &poolClientFactory{
		baseConfig: postgreSQLConfig{
			username: cfg.Username,
			password: string(cfg.Password),
			address:  cfg.AddrConfig,
			tls:      cfg.ClientConfig,
			timeouts: resolveTimeouts(cfg),
		},
		poolConfig:         &poolCfg,
		pool:               make(map[string]*pooledDB),
		closed:             false,
		maxPooledDatabases: maxPooled,
		now:                time.Now,
	}
}

func (p *poolClientFactory) getClient(database string) (client, error) {
	p.Lock()
	defer p.Unlock()

	if p.closed {
		return nil, errFactoryClosed
	}

	entry, ok := p.pool[database]
	if !ok {
		db, err := getDB(p.baseConfig, database)
		if err != nil {
			return nil, err
		}
		p.setPoolSettings(db, database)
		entry = &pooledDB{db: db}
		p.pool[database] = entry
	}
	entry.refs++
	p.evictLocked()

	return &postgreSQLClient{
		client:  WrapDBWithIgnore(entry.db),
		closeFn: func() error { p.release(database); return nil },
	}, nil
}

// release records that a client for database has been closed. It does not close
// any connection itself: the pool keeps its idle connections so the next client
// for the same database can reuse them, unless the pool is over budget.
func (p *poolClientFactory) release(database string) {
	p.Lock()
	defer p.Unlock()

	entry, ok := p.pool[database]
	if !ok {
		return
	}
	if entry.refs > 0 {
		entry.refs--
	}
	if entry.refs == 0 {
		entry.releasedAt = p.now()
	}
	p.evictLocked()
}

// evictLocked closes idle pools until the factory is within budget. The default
// database is never evicted: every scraper uses it every cycle, so it is the one
// pool that is always worth keeping warm. A pool with clients outstanding is
// never evicted either, so the bound is soft while many databases are in use at
// once. Callers must hold the lock.
func (p *poolClientFactory) evictLocked() {
	for len(p.pool) > p.maxPooledDatabases {
		var victim string
		var victimAt time.Time
		for name, entry := range p.pool {
			if name == defaultPostgreSQLDatabase || entry.refs > 0 {
				continue
			}
			if victim == "" || entry.releasedAt.Before(victimAt) {
				victim, victimAt = name, entry.releasedAt
			}
		}
		if victim == "" {
			return
		}
		// Close never blocks on idle connections, and there are no in-use ones
		// to wait for since refs is zero.
		_ = p.pool[victim].db.Close()
		delete(p.pool, victim)
	}
}

func (p *poolClientFactory) close() error {
	p.Lock()
	defer p.Unlock()

	if p.closed {
		return nil
	}

	var err error
	for _, entry := range p.pool {
		if closeErr := entry.db.Close(); closeErr != nil {
			err = multierr.Append(err, closeErr)
		}
	}
	if err != nil {
		return err
	}

	p.pool = make(map[string]*pooledDB)
	p.closed = true
	return nil
}

func (p *poolClientFactory) setPoolSettings(db *sql.DB, database string) {
	if p.poolConfig == nil {
		return
	}
	if p.poolConfig.MaxIdleTime != nil {
		db.SetConnMaxIdleTime(*p.poolConfig.MaxIdleTime)
	}
	if p.poolConfig.MaxLifetime != nil {
		db.SetConnMaxLifetime(*p.poolConfig.MaxLifetime)
	}
	if p.poolConfig.MaxOpen != nil {
		db.SetMaxOpenConns(*p.poolConfig.MaxOpen)
	}
	switch {
	case database != defaultPostgreSQLDatabase:
		// A per-database pool exists so that the handful of queries one scrape
		// runs against that database share a connection. Holding more than one
		// idle connection per database multiplies the idle footprint by the
		// number of databases for no benefit, since scrapes visit databases
		// one at a time. Only the default database is hot enough to warrant
		// the configured idle count.
		db.SetMaxIdleConns(1)
	case p.poolConfig.MaxIdle != nil:
		db.SetMaxIdleConns(*p.poolConfig.MaxIdle)
	}
}

func getDB(cfg postgreSQLConfig, database string) (*sql.DB, error) {
	if database != "" {
		cfg.database = database
	}
	connectionString, err := cfg.ConnectionString()
	if err != nil {
		return nil, err
	}
	conn, err := pq.NewConnector(connectionString)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(conn), nil
}
