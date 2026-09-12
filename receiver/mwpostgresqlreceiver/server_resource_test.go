// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver/receivertest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

// serverWideMetrics describe the instance rather than any one database. Their
// resource must therefore carry no postgresql.database.name; which resource
// they landed on used to depend on the order a map was iterated in.
var serverWideMetrics = []string{
	"postgresql.database.count",
	"postgresql.connection.max",
	"postgresql.bgwriter.buffers.allocated",
	"postgresql.bgwriter.buffers.writes",
	"postgresql.bgwriter.checkpoint.count",
	"postgresql.bgwriter.duration",
	"postgresql.bgwriter.maxwritten",
}

// TestServerWideMetricsLandOnServerResource fails when any server-wide metric
// is emitted under a database's resource. The mock returns connection
// statistics for one database, so the misattribution is deterministic here:
// before the fix every metric listed above was emitted under `otel`, because
// postgresql.connection.count emitted that database's resource after the
// server-wide points had been recorded and before the server resource was.
func TestServerWideMetricsLandOnServerResource(t *testing.T) {
	factory := new(mockClientFactory)
	factory.initMocks([]string{"otel"})

	cfg := createDefaultConfig().(*Config)
	cfg.Databases = []string{"otel"}
	scraper := newPostgreSQLScraper(receivertest.NewNopSettings(metadata.Type), cfg, factory,
		newStatementStateCache(1), newTTLCache[queryPlanKey, string](1, time.Second))

	actual, err := scraper.scrape(t.Context())
	require.NoError(t, err)

	seen := map[string]bool{}
	connectionCountResources := 0
	for _, rm := range resourceMetricsOf(actual) {
		_, hasDatabase := rm.Resource().Attributes().Get("postgresql.database.name")
		for _, name := range metricNamesOf(rm) {
			for _, serverWide := range serverWideMetrics {
				if name == serverWide {
					seen[name] = true
					require.Falsef(t, hasDatabase, "%s is server-wide but was emitted under a database resource", name)
				}
			}
			if name == "postgresql.connection.count" {
				connectionCountResources++
				require.True(t, hasDatabase, "postgresql.connection.count must be emitted per database")
			}
		}
	}
	for _, name := range serverWideMetrics {
		require.Truef(t, seen[name], "%s was not emitted at all", name)
	}
	require.Equal(t, 1, connectionCountResources)
}

func resourceMetricsOf(m pmetric.Metrics) []pmetric.ResourceMetrics {
	out := make([]pmetric.ResourceMetrics, 0, m.ResourceMetrics().Len())
	for i := 0; i < m.ResourceMetrics().Len(); i++ {
		out = append(out, m.ResourceMetrics().At(i))
	}
	return out
}

func metricNamesOf(rm pmetric.ResourceMetrics) []string {
	var names []string
	for i := 0; i < rm.ScopeMetrics().Len(); i++ {
		ms := rm.ScopeMetrics().At(i).Metrics()
		for j := 0; j < ms.Len(); j++ {
			names = append(names, ms.At(j).Name())
		}
	}
	return names
}
