// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver/receivertest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

const deallocSQL = "/* otel-collector-ignore */ SELECT dealloc FROM public.pg_stat_statements_info"

// deallocScraper builds a metrics scraper whose only enabled family is
// postgresql.query.deallocations, over a sqlmock connection, and with an
// allowlist so no discovery query runs. Every other family is disabled, so the
// scrape issues exactly the queries the deallocation read needs and nothing
// else, which is what lets sqlmock's expectations pin the behaviour.
func deallocScraper(t *testing.T, enabled bool) (*postgreSQLScraper, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	cfg := createDefaultConfig().(*Config)
	cfg.Databases = []string{"postgres"}
	cfg.Metrics = metadata.MetricsConfig{}
	cfg.Metrics.PostgresqlQueryDeallocations.Enabled = enabled

	scraper := newPostgreSQLScraper(receivertest.NewNopSettings(metadata.Type), cfg, mockSimpleClientFactory{db: db},
		newStatementStateCache(1), newTTLCache[queryPlanKey, string](1, time.Second))
	return scraper, mock
}

func deallocPoint(t *testing.T, m pmetric.Metrics) (value int64, found bool) {
	t.Helper()
	for _, rm := range resourceMetricsOf(m) {
		for i := 0; i < rm.ScopeMetrics().Len(); i++ {
			ms := rm.ScopeMetrics().At(i).Metrics()
			for j := 0; j < ms.Len(); j++ {
				if ms.At(j).Name() != "postgresql.query.deallocations" {
					continue
				}
				_, hasDatabase := rm.Resource().Attributes().Get("postgresql.database.name")
				require.False(t, hasDatabase, "deallocations is server-wide and must not carry a database name")
				require.True(t, ms.At(j).Sum().IsMonotonic())
				return ms.At(j).Sum().DataPoints().At(0).IntValue(), true
			}
		}
	}
	return 0, false
}

func TestDeallocationsReadOnExtension19(t *testing.T) {
	scraper, mock := deallocScraper(t, true)
	expectPgStatStatementsVersion(mock, "1.9")
	mock.ExpectQuery(deallocSQL).WillReturnRows(sqlmock.NewRows([]string{"dealloc"}).AddRow(7))

	metrics, err := scraper.scrape(t.Context())
	require.NoError(t, err)
	value, found := deallocPoint(t, metrics)
	require.True(t, found)
	require.Equal(t, int64(7), value)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeallocationsSkippedBelowExtension19(t *testing.T) {
	scraper, mock := deallocScraper(t, true)
	expectPgStatStatementsVersion(mock, "1.8")
	// No dealloc expectation is queued: on 1.8 the view does not exist, so
	// the read must be skipped rather than attempted and swallowed.

	metrics, err := scraper.scrape(t.Context())
	require.NoError(t, err)
	_, found := deallocPoint(t, metrics)
	require.False(t, found)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeallocationsDisabledIssuesNoQuery(t *testing.T) {
	scraper, mock := deallocScraper(t, false)
	// Nothing queued at all: with the metric off, the scrape must not even
	// resolve the extension version on its account.

	metrics, err := scraper.scrape(t.Context())
	require.NoError(t, err)
	_, found := deallocPoint(t, metrics)
	require.False(t, found)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeallocationsReadErrorIsPartial(t *testing.T) {
	scraper, mock := deallocScraper(t, true)
	expectPgStatStatementsVersion(mock, "1.9")
	mock.ExpectQuery(deallocSQL).WillReturnError(errors.New("permission denied for view pg_stat_statements_info"))

	metrics, err := scraper.scrape(t.Context())
	require.Error(t, err)
	_, found := deallocPoint(t, metrics)
	require.False(t, found)
	require.NoError(t, mock.ExpectationsWereMet())
}
