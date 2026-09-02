// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newTestPoolFactory builds a pool factory with a bounded number of pools.
// sql.OpenDB does not dial, so pools can be opened and closed without a server.
func newTestPoolFactory(t *testing.T, maxDatabases int) *poolClientFactory {
	t.Helper()

	cfg := createDefaultConfig().(*Config)
	cfg.Username = "otel"
	cfg.Password = "otel"
	cfg.Endpoint = "localhost:5432"
	cfg.ConnectionPool.MaxDatabases = &maxDatabases

	f := newPoolClientFactory(cfg)
	t.Cleanup(func() { _ = f.close() })

	// A deterministic clock so eviction order does not depend on how fast
	// the test runs.
	tick := time.Unix(0, 0)
	f.now = func() time.Time {
		tick = tick.Add(time.Second)
		return tick
	}
	return f
}

func (p *poolClientFactory) pooledNames() []string {
	p.Lock()
	defer p.Unlock()
	names := make([]string, 0, len(p.pool))
	for name := range p.pool {
		names = append(names, name)
	}
	return names
}

// TestPoolFactoryBoundsPools is the core of the connection fix. Without a bound
// the factory held one pool — and so at least one idle server backend — per
// database ever visited, for the life of the process, which on a server with
// many databases exhausted the monitoring role's connection limit.
func TestPoolFactoryBoundsPools(t *testing.T) {
	f := newTestPoolFactory(t, 2)

	for _, db := range []string{"a", "b", "c", "d", "e"} {
		c, err := f.getClient(db)
		require.NoError(t, err)
		require.NoError(t, c.Close())
	}

	require.Len(t, f.pooledNames(), 2, "idle pools must be evicted down to the budget")
	// The most recently released survive.
	require.ElementsMatch(t, []string{"d", "e"}, f.pooledNames())
}

// TestPoolFactoryNeverEvictsDefaultDatabase: every scraper uses the default
// database every cycle, so it is the one pool always worth keeping warm.
func TestPoolFactoryNeverEvictsDefaultDatabase(t *testing.T) {
	f := newTestPoolFactory(t, 1)

	c, err := f.getClient(defaultPostgreSQLDatabase)
	require.NoError(t, err)
	require.NoError(t, c.Close())

	for _, db := range []string{"a", "b", "c"} {
		c, err := f.getClient(db)
		require.NoError(t, err)
		require.NoError(t, c.Close())
	}

	require.Contains(t, f.pooledNames(), defaultPostgreSQLDatabase,
		"the default database must survive eviction")
}

// TestPoolFactoryNeverEvictsHeldPool guards the hazard that makes reference
// counting necessary: scrapers run concurrently, so one may be mid-query on a
// database while another opens a pool that pushes the factory over budget.
// Closing a pool that is in use would fail that scrape with "database is
// closed".
func TestPoolFactoryNeverEvictsHeldPool(t *testing.T) {
	f := newTestPoolFactory(t, 1)

	held, err := f.getClient("held")
	require.NoError(t, err)

	for _, db := range []string{"a", "b", "c"} {
		c, err := f.getClient(db)
		require.NoError(t, err)
		require.NoError(t, c.Close())
	}

	require.Contains(t, f.pooledNames(), "held", "a pool with a client outstanding must not be evicted")

	// Once released it becomes eligible and the next pressure evicts it.
	require.NoError(t, held.Close())
	c, err := f.getClient("z")
	require.NoError(t, err)
	require.NoError(t, c.Close())
	require.NotContains(t, f.pooledNames(), "held")
}

// TestPoolFactoryReferenceCounting checks that a pool is only considered idle
// once every client for it has been closed, not just the first.
func TestPoolFactoryReferenceCounting(t *testing.T) {
	f := newTestPoolFactory(t, 1)

	first, err := f.getClient("shared")
	require.NoError(t, err)
	second, err := f.getClient("shared")
	require.NoError(t, err)

	f.Lock()
	require.Equal(t, 2, f.pool["shared"].refs)
	f.Unlock()

	require.NoError(t, first.Close())

	// Still held by second, so pressure must not evict it.
	c, err := f.getClient("other")
	require.NoError(t, err)
	require.NoError(t, c.Close())
	require.Contains(t, f.pooledNames(), "shared")

	require.NoError(t, second.Close())
	f.Lock()
	require.Equal(t, 0, f.pool["shared"].refs)
	f.Unlock()
}

// TestPoolFactoryEvictsLeastRecentlyReleased: when several pools are idle, the
// one released longest ago goes first, so hot databases stay warm.
func TestPoolFactoryEvictsLeastRecentlyReleased(t *testing.T) {
	f := newTestPoolFactory(t, 2)

	for _, db := range []string{"old", "new"} {
		c, err := f.getClient(db)
		require.NoError(t, err)
		require.NoError(t, c.Close())
	}
	// Touch "old" again so it is now the most recently released.
	c, err := f.getClient("old")
	require.NoError(t, err)
	require.NoError(t, c.Close())

	c, err = f.getClient("third")
	require.NoError(t, err)
	require.NoError(t, c.Close())

	require.ElementsMatch(t, []string{"old", "third"}, f.pooledNames())
}

// TestPoolFactoryDefaultBound: with no explicit configuration the bound is the
// documented default rather than unlimited.
func TestPoolFactoryDefaultBound(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Username = "otel"
	cfg.Password = "otel"
	cfg.Endpoint = "localhost:5432"

	f := newPoolClientFactory(cfg)
	t.Cleanup(func() { _ = f.close() })

	require.Equal(t, defaultMaxPooledDatabases, f.maxPooledDatabases)
}

// TestPoolFactoryCloseReleasesEverything: shutdown must not leave pools behind.
func TestPoolFactoryCloseReleasesEverything(t *testing.T) {
	f := newTestPoolFactory(t, 10)

	for _, db := range []string{"a", "b", defaultPostgreSQLDatabase} {
		c, err := f.getClient(db)
		require.NoError(t, err)
		require.NoError(t, c.Close())
	}
	require.Len(t, f.pooledNames(), 3)

	require.NoError(t, f.close())
	require.Empty(t, f.pooledNames())
	require.NoError(t, f.close(), "closing twice is harmless")

	// A closed factory must not quietly open a fresh pool that nothing will
	// ever close.
	_, err := f.getClient("late")
	require.ErrorIs(t, err, errFactoryClosed)
	require.Empty(t, f.pooledNames())
}
