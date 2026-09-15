// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func arbitrationConfig() *Config {
	cfg := createDefaultConfig().(*Config)
	cfg.Username = "otel"
	cfg.Password = "otel"
	return cfg
}

// TestBudgetEvictsIdlePoolsAcrossFactories is the failure seen on the
// 52-database rig: the metrics factory fills the shared budget with pools it
// keeps warm, and the logs factory, asking for a database of its own, must be
// able to reclaim one of those idle pools instead of failing.
func TestBudgetEvictsIdlePoolsAcrossFactories(t *testing.T) {
	budget := newConnectionBudget(3)
	metrics := newPoolClientFactory(arbitrationConfig(), budget)
	logs := newPoolClientFactory(arbitrationConfig(), budget)
	t.Cleanup(func() { _ = metrics.close(); _ = logs.close() })

	// The metrics factory takes the whole budget: the default database plus
	// two others, all released and therefore idle.
	for _, db := range []string{defaultPostgreSQLDatabase, "db1", "db2"} {
		c, err := metrics.getClient(db)
		require.NoError(t, err)
		require.NoError(t, c.Close())
	}
	require.Equal(t, 0, budget.available())

	c, err := logs.getClient("schema-target")
	require.NoError(t, err, "the logs factory must reclaim an idle pool held by the metrics factory")
	require.NoError(t, c.Close())

	require.Equal(t, 0, budget.available())
	require.Len(t, metrics.pool, 2, "one of the metrics factory's idle pools was evicted")
	_, keptDefault := metrics.pool[defaultPostgreSQLDatabase]
	require.True(t, keptDefault, "the default database pool is never the victim")
	require.Contains(t, logs.pool, "schema-target")
}

// TestBudgetEvictsTheOldestIdleAcrossFactories: with idle pools in both
// factories, the one released longest ago goes first, whichever factory owns it.
func TestBudgetEvictsTheOldestIdleAcrossFactories(t *testing.T) {
	budget := newConnectionBudget(2)
	a := newPoolClientFactory(arbitrationConfig(), budget)
	b := newPoolClientFactory(arbitrationConfig(), budget)
	t.Cleanup(func() { _ = a.close(); _ = b.close() })

	clock := &fakeClock{}
	a.now, b.now = clock.now, clock.now

	ca, err := a.getClient("a-old")
	require.NoError(t, err)
	require.NoError(t, ca.Close()) // released at t0
	clock.t = clock.t.Add(1)
	cb, err := b.getClient("b-new")
	require.NoError(t, err)
	require.NoError(t, cb.Close()) // released at t0+1
	require.Equal(t, 0, budget.available())

	c, err := b.getClient("b-next")
	require.NoError(t, err)
	require.NoError(t, c.Close())
	require.NotContains(t, a.pool, "a-old", "the older idle pool, in the other factory, is the victim")
	require.Contains(t, b.pool, "b-new")
}

// TestBudgetDoesNotEvictPoolsInUse: a pool with a client outstanding is never
// reclaimed, so an exhausted budget with nothing idle is still refused rather
// than pulling a connection out from under a running scrape.
func TestBudgetDoesNotEvictPoolsInUse(t *testing.T) {
	budget := newConnectionBudget(2)
	a := newPoolClientFactory(arbitrationConfig(), budget)
	b := newPoolClientFactory(arbitrationConfig(), budget)
	t.Cleanup(func() { _ = a.close(); _ = b.close() })

	c1, err := a.getClient("busy1")
	require.NoError(t, err)
	c2, err := a.getClient("busy2")
	require.NoError(t, err)
	defer c1.Close()
	defer c2.Close()

	_, err = b.getClient("wanted")
	require.Error(t, err)
	require.True(t, errors.Is(err, errConnectionBudgetExhausted))
	require.Len(t, a.pool, 2)
}

// TestBudgetArbitrationUnderContention hammers two factories from many
// goroutines. It exists for the race detector and for deadlock detection: the
// factories lock each other's pools through the budget, and the lock ordering
// must never let a factory wait on the arbiter while holding its own lock.
func TestBudgetArbitrationUnderContention(t *testing.T) {
	budget := newConnectionBudget(3)
	a := newPoolClientFactory(arbitrationConfig(), budget)
	b := newPoolClientFactory(arbitrationConfig(), budget)
	t.Cleanup(func() { _ = a.close(); _ = b.close() })

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		for _, f := range []*poolClientFactory{a, b} {
			wg.Add(1)
			go func(f *poolClientFactory, g int) {
				defer wg.Done()
				for i := 0; i < 200; i++ {
					c, err := f.getClient(fmt.Sprintf("db%d", (g+i)%6))
					if err != nil {
						require.True(t, errors.Is(err, errConnectionBudgetExhausted), "only budget exhaustion is an acceptable failure: %v", err)
						continue
					}
					require.NoError(t, c.Close())
				}
			}(f, g)
		}
	}
	wg.Wait()

	a.Lock()
	b.Lock()
	live := len(a.pool) + len(b.pool)
	a.Unlock()
	b.Unlock()
	require.Equal(t, live, budget.used(), "budget must track the set of live pools exactly")
	require.LessOrEqual(t, live, 3)
}
