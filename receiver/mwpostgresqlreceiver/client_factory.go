// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"database/sql"
	"errors"
	"fmt"
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

// errConnectionBudgetExhausted is returned when no budget remains for a new
// database pool. Callers treat it as "skip this database this cycle", not as a
// collection failure: the data will still be there next scrape.
var errConnectionBudgetExhausted = errors.New("postgresql connection budget exhausted")

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
	// budget bounds connections across BOTH signals. The metrics and logs
	// receivers each build their own factory, so a bound held here alone would
	// only ever see half of what the receiver opens against the server; the
	// same budget instance is handed to both.
	budget *connectionBudget
	now    func() time.Time
}

func newPoolClientFactory(cfg *Config, budget *connectionBudget) *poolClientFactory {
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
		budget:             budget,
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
		// Opening a pool for a new database costs budget. Try to make room by
		// closing an idle pool before giving up, so a steady rotation through
		// many databases keeps working instead of stalling once the budget is
		// first reached.
		if !p.budget.tryAcquire() {
			p.evictIdleLocked(1)
			if !p.budget.tryAcquire() {
				return nil, fmt.Errorf(
					"%w: %d connections already in use", errConnectionBudgetExhausted, p.budget.used())
			}
		}

		db, err := getDB(p.baseConfig, database)
		if err != nil {
			p.budget.release()
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
		victim := p.idlestVictimLocked()
		if victim == "" {
			return
		}
		p.closePoolLocked(victim)
	}
}

// evictIdleLocked closes up to n idle pools, oldest-released first, to free
// budget for a database that has none. It returns how many it closed, which may
// be fewer than n when every remaining pool is in use. Callers must hold the
// lock.
func (p *poolClientFactory) evictIdleLocked(n int) int {
	closed := 0
	for closed < n {
		victim := p.idlestVictimLocked()
		if victim == "" {
			return closed
		}
		p.closePoolLocked(victim)
		closed++
	}
	return closed
}

// idlestVictimLocked names the idle pool released longest ago, or "" when every
// pool is either in use or the default database. The default database is never
// a victim: every scraper uses it every cycle, so it is the one pool always
// worth keeping warm. Callers must hold the lock.
func (p *poolClientFactory) idlestVictimLocked() string {
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
	return victim
}

// closePoolLocked closes one pool and returns its budget. Every path that
// removes a pool goes through here, so the budget cannot drift from the set of
// live pools. Callers must hold the lock.
func (p *poolClientFactory) closePoolLocked(database string) {
	entry, ok := p.pool[database]
	if !ok {
		return
	}
	// Close never blocks on idle connections, and an evicted pool has no in-use
	// ones to wait for since refs is zero.
	_ = entry.db.Close()
	delete(p.pool, database)
	p.budget.release()
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
		// Released even when Close reports an error: the pool is being
		// discarded either way, so holding its budget would leak it for the
		// life of the process.
		p.budget.release()
	}

	p.pool = make(map[string]*pooledDB)
	p.closed = true
	return err
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
