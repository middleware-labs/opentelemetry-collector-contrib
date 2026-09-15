// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/config/confignet"
)

func TestConnectionBudgetAdmitsUpToMax(t *testing.T) {
	b := newConnectionBudget(3)

	for i := range 3 {
		assert.True(t, b.tryAcquire(), "acquire %d must succeed within budget", i)
	}
	assert.False(t, b.tryAcquire(), "acquire beyond the budget must be refused")
	assert.Equal(t, 0, b.available())
	assert.Equal(t, 3, b.used())
}

func TestConnectionBudgetReleaseFreesCapacity(t *testing.T) {
	b := newConnectionBudget(1)

	require.True(t, b.tryAcquire())
	require.False(t, b.tryAcquire())

	b.release()
	assert.True(t, b.tryAcquire(), "released budget must become available again")
}

func TestConnectionBudgetReleaseIsClamped(t *testing.T) {
	// A double release must not manufacture budget that does not exist:
	// over-releasing would let the receiver exceed the role's connection limit,
	// which is the exact failure the budget prevents.
	b := newConnectionBudget(1)

	require.True(t, b.tryAcquire())
	b.release()
	b.release()
	b.release()

	assert.Equal(t, 0, b.used())
	require.True(t, b.tryAcquire())
	assert.False(t, b.tryAcquire(), "budget must still cap at max after extra releases")
}

func TestConnectionBudgetDefaultsWhenNonPositive(t *testing.T) {
	// A misconfigured zero or negative must not mean "no connections allowed",
	// which would silently disable the receiver.
	for _, max := range []int{0, -1} {
		b := newConnectionBudget(max)
		assert.Equal(t, defaultMaxTotalConnections, b.available())
	}
}

func TestConnectionBudgetIsConcurrencySafe(t *testing.T) {
	const max = 10
	b := newConnectionBudget(max)

	var wg sync.WaitGroup
	var mu sync.Mutex
	granted := 0

	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b.tryAcquire() {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// The whole point is that the cap holds under concurrency: PostgreSQL's own
	// enforcement is racy, so ours must not be.
	assert.Equal(t, max, granted, "exactly max acquisitions may be granted")
	assert.Equal(t, max, b.used())
}

// testPoolConfig builds a config pointing at an address nothing listens on.
// sql.OpenDB does not dial, so pools are created without any server, which is
// what lets these tests exercise budget accounting rather than connectivity.
func testPoolConfig() *Config {
	cfg := createDefaultConfig().(*Config)
	cfg.Username = "u"
	cfg.Password = "p"
	cfg.AddrConfig = confignet.AddrConfig{
		Endpoint:  "localhost:5432",
		Transport: confignet.TransportTypeTCP,
	}
	cfg.Insecure = true
	return cfg
}

func TestPoolFactoryRefusesBeyondBudget(t *testing.T) {
	cfg := testPoolConfig()
	f := newPoolClientFactory(cfg, newConnectionBudget(2))
	defer func() { _ = f.close() }()

	// Two distinct databases fit the budget.
	c1, err := f.getClient("db1")
	require.NoError(t, err)
	c2, err := f.getClient("db2")
	require.NoError(t, err)

	// A third has nowhere to come from: both existing pools are in use, so
	// eviction cannot free anything.
	_, err = f.getClient("db3")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errConnectionBudgetExhausted),
		"want errConnectionBudgetExhausted, got %v", err)

	// Releasing one lets the next database in, by evicting the now-idle pool.
	require.NoError(t, c1.Close())
	c3, err := f.getClient("db3")
	require.NoError(t, err, "an idle pool must be evicted to admit a new database")
	require.NoError(t, c3.Close())
	require.NoError(t, c2.Close())
}

func TestPoolFactoryReusesPoolWithoutSpendingBudget(t *testing.T) {
	cfg := testPoolConfig()
	budget := newConnectionBudget(2)
	f := newPoolClientFactory(cfg, budget)
	defer func() { _ = f.close() }()

	c1, err := f.getClient("db1")
	require.NoError(t, err)
	require.Equal(t, 1, budget.used())

	// A second client for the SAME database shares the existing pool, so it
	// must not consume more budget. Otherwise concurrent scrapers of one
	// database would exhaust the budget against a single server backend.
	c2, err := f.getClient("db1")
	require.NoError(t, err)
	assert.Equal(t, 1, budget.used(), "reusing a pool must not spend more budget")

	require.NoError(t, c1.Close())
	require.NoError(t, c2.Close())
}

func TestPoolFactoryReturnsBudgetOnClose(t *testing.T) {
	cfg := testPoolConfig()
	budget := newConnectionBudget(4)
	f := newPoolClientFactory(cfg, budget)

	for _, db := range []string{"db1", "db2", "db3"} {
		c, err := f.getClient(db)
		require.NoError(t, err)
		require.NoError(t, c.Close())
	}
	require.Positive(t, budget.used())

	require.NoError(t, f.close())
	assert.Equal(t, 0, budget.used(),
		"closing the factory must return all budget, or it leaks for the process lifetime")
}

func TestBudgetIsSharedAcrossSignals(t *testing.T) {
	// The property that makes the budget correspond to what PostgreSQL
	// enforces. The metrics and logs receivers are built by separate calls,
	// each constructing its own client factory; if they held separate budgets
	// the receiver could open twice the configured maximum against a role
	// limit the server applies across both.
	cfg := testPoolConfig()

	b1 := budgetFor(cfg)
	b2 := budgetFor(cfg)
	assert.Same(t, b1, b2, "both signals must share one budget instance")

	// A different receiver instance is entitled to its own allowance.
	other := testPoolConfig()
	assert.NotSame(t, b1, budgetFor(other),
		"separately configured receivers must not share a budget")
}

func TestBudgetForHonoursConfiguredMaximum(t *testing.T) {
	cfg := testPoolConfig()
	maxTotal := 5
	cfg.ConnectionPool.MaxTotalConnections = &maxTotal

	assert.Equal(t, 5, budgetFor(cfg).available())
}

func TestBudgetForFallsBackToDefault(t *testing.T) {
	cfg := testPoolConfig()
	assert.Equal(t, defaultMaxTotalConnections, budgetFor(cfg).available())
}
