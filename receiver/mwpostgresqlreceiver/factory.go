// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"context"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confignet"
	"go.opentelemetry.io/collector/config/configtls"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/scraper"
	"go.opentelemetry.io/collector/scraper/scraperhelper"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

// newCache creates a new cache with the given size.
// If the size is less or equal to 0, it will be set to 1.
// It will never return an error.
func newCache(size int) *lru.Cache[string, float64] {
	if size <= 0 {
		size = 1
	}
	// lru will only return error when the size is less than 0
	cache, _ := lru.New[string, float64](size)
	return cache
}

func newTTLCache[v any](size int, ttl time.Duration) *expirable.LRU[string, v] {
	if size <= 0 {
		size = 1
	}
	cache := expirable.NewLRU[string, v](size, nil, ttl)
	return cache
}

// sharedBudgets hands the metrics and logs receivers built from the same
// configuration the same connection budget.
//
// The collector calls createMetricsReceiver and createLogsReceiver separately,
// each building its own client factory, so without this the two would hold
// independent budgets and the receiver could open twice what the operator
// configured — against a role limit that PostgreSQL enforces cluster-wide
// across both.
//
// Keyed by the *Config pointer, which the collector creates once per configured
// receiver instance and passes to both calls. Two receiver instances pointed at
// the same server therefore get separate budgets, which is correct: they are
// separately configured and each is entitled to its own allowance.
//
// Entries are never removed. One small struct per configured receiver lives for
// the process lifetime; receivers are created at startup, not per scrape, so
// this does not grow.
var sharedBudgets sync.Map

func budgetFor(cfg *Config) *connectionBudget {
	maxTotal := defaultMaxTotalConnections
	if cfg.ConnectionPool.MaxTotalConnections != nil && *cfg.ConnectionPool.MaxTotalConnections > 0 {
		maxTotal = *cfg.ConnectionPool.MaxTotalConnections
	}
	budget, _ := sharedBudgets.LoadOrStore(cfg, newConnectionBudget(maxTotal))
	return budget.(*connectionBudget)
}

func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		metadata.Type,
		createDefaultConfig,
		receiver.WithMetrics(createMetricsReceiver, metadata.MetricsStability),
		receiver.WithLogs(createLogsReceiver, metadata.LogsStability),
	)
}

func createDefaultConfig() component.Config {
	cfg := scraperhelper.NewDefaultControllerConfig()
	cfg.CollectionInterval = 10 * time.Second

	return &Config{
		ControllerConfig: cfg,
		AddrConfig: confignet.AddrConfig{
			Endpoint:  "localhost:5432",
			Transport: confignet.TransportTypeTCP,
		},
		ClientConfig: configtls.ClientConfig{
			Insecure:           false,
			InsecureSkipVerify: true,
		},
		MetricsBuilderConfig: metadata.DefaultMetricsBuilderConfig(),
		LogsBuilderConfig:    metadata.DefaultLogsBuilderConfig(),
		QuerySampleCollection: QuerySampleCollection{
			MaxRowsPerQuery: 1000,
		},
		TopQueryCollection: TopQueryCollection{
			TopNQuery:              1000,
			MaxRowsPerQuery:        1000,
			MaxExplainEachInterval: 1000,
			QueryPlanCacheSize:     1000,
			QueryPlanCacheTTL:      time.Hour,
		},
	}
}

func createMetricsReceiver(
	_ context.Context,
	params receiver.Settings,
	rConf component.Config,
	consumer consumer.Metrics,
) (receiver.Metrics, error) {
	cfg := rConf.(*Config)

	var clientFactory postgreSQLClientFactory
	if connectionPoolGate.IsEnabled() {
		clientFactory = newPoolClientFactory(cfg, budgetFor(cfg))
	} else {
		clientFactory = newDefaultClientFactory(cfg)
	}

	ns := newPostgreSQLScraper(params, cfg, clientFactory, newCache(1), newTTLCache[string](1, time.Second))
	s, err := scraper.NewMetrics(ns.scrape, scraper.WithShutdown(ns.shutdown))
	if err != nil {
		return nil, err
	}

	return scraperhelper.NewMetricsController(
		&cfg.ControllerConfig, params, consumer,
		scraperhelper.AddScraper(metadata.Type, s),
	)
}

// createLogsReceiver create a logs receiver based on provided config.
func createLogsReceiver(
	_ context.Context,
	params receiver.Settings,
	receiverCfg component.Config,
	logsConsumer consumer.Logs,
) (receiver.Logs, error) {
	cfg := receiverCfg.(*Config)

	var clientFactory postgreSQLClientFactory
	if connectionPoolGate.IsEnabled() {
		clientFactory = newPoolClientFactory(cfg, budgetFor(cfg))
	} else {
		clientFactory = newDefaultClientFactory(cfg)
	}

	opts := make([]scraperhelper.ControllerOption, 0)

	if cfg.Events.DbServerQuerySample.Enabled {
		// query sample collection does not need cache, but we do not want to make it
		// nil, so create one size 1 cache as a placeholder.
		ns := newPostgreSQLScraper(params, cfg, clientFactory, newCache(1), newTTLCache[string](1, time.Second))
		s, err := scraper.NewLogs(func(ctx context.Context) (plog.Logs, error) {
			return ns.scrapeQuerySamples(ctx, cfg.QuerySampleCollection.MaxRowsPerQuery)
		}, scraper.WithShutdown(ns.shutdown))
		if err != nil {
			return nil, err
		}
		opt := scraperhelper.AddFactoryWithConfig(
			scraper.NewFactory(metadata.Type, nil,
				scraper.WithLogs(func(context.Context, scraper.Settings, component.Config) (scraper.Logs, error) {
					return s, nil
				}, component.StabilityLevelAlpha)), nil)
		opts = append(opts, opt)
	}

	if cfg.Events.DbServerTopQuery.Enabled {
		// The cache holds one entry per counter per candidate statement, and
		// every candidate row is traversed on every scrape - not just the
		// top_n_query rows that are emitted. Sizing it from the output count
		// makes a small top_n_query evict the whole candidate set each scrape,
		// so every row looks like a first observation and never produces a
		// delta. Size it from the candidate count instead.
		//
		// There are 12 counters (see updatedOnly in collectTopQuery); the
		// factor of 2 is headroom for the candidate set shifting between
		// scrapes.
		ns := newPostgreSQLScraper(params, cfg, clientFactory, newCache(int(cfg.TopQueryCollection.MaxRowsPerQuery*topQueryCounterCount*2)), newTTLCache[string](cfg.QueryPlanCacheSize, cfg.QueryPlanCacheTTL))
		s, err := scraper.NewLogs(func(ctx context.Context) (plog.Logs, error) {
			return ns.scrapeTopQuery(ctx, cfg.TopQueryCollection.MaxRowsPerQuery, cfg.TopNQuery, cfg.MaxExplainEachInterval)
		}, scraper.WithShutdown(ns.shutdown))
		if err != nil {
			return nil, err
		}
		opt := scraperhelper.AddFactoryWithConfig(
			scraper.NewFactory(metadata.Type, nil,
				scraper.WithLogs(func(context.Context, scraper.Settings, component.Config) (scraper.Logs, error) {
					return s, nil
				}, component.StabilityLevelAlpha)), nil)
		opts = append(opts, opt)
	}

	if cfg.SchemaCollection.Enabled {
		// schema collection does not need cache, but we do not want to make it
		// nil, so create one size 1 cache as a placeholder.
		ns := newPostgreSQLScraper(params, cfg, clientFactory, newCache(1), newTTLCache[string](1, time.Second))
		s, err := scraper.NewLogs(func(ctx context.Context) (plog.Logs, error) {
			return ns.scrapeSchemaCollection(ctx)
		}, scraper.WithShutdown(ns.shutdown))
		if err != nil {
			return nil, err
		}
		opt := scraperhelper.AddFactoryWithConfig(
			scraper.NewFactory(metadata.Type, nil,
				scraper.WithLogs(func(context.Context, scraper.Settings, component.Config) (scraper.Logs, error) {
					return s, nil
				}, component.StabilityLevelAlpha)), nil)
		opts = append(opts, opt)
	}

	return scraperhelper.NewLogsController(
		&cfg.ControllerConfig, params, logsConsumer, opts...,
	)
}
