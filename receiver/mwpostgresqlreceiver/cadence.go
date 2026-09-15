// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import "time"

// familiesDue records, for one metrics scrape, which of the throttled query
// families are to be collected. The database-level and server-wide families
// are not represented here because they run on every scrape.
type familiesDue struct {
	// relations covers the per-relation families: table details and per-table
	// block reads (collectTables), indexes (collectIndexes) and functions
	// (collectFunctions).
	relations bool
	// bloat covers collectTableBloat and collectIndexBloat.
	bloat bool
}

// familiesDue decides which throttled families this scrape collects and marks
// the ones that run as having run now. It is called once per scrape, before
// the per-database loop, so every database in the scrape sees the same
// decision.
//
// This is the same mechanism scrapeSchemaCollection uses with lastSchemaCheck:
// the controller keeps calling the scraper every collection_interval, and the
// scraper keeps its own per-family timestamp and declines the work when it is
// not yet due. One receiver instance therefore keeps one controller and one
// connection budget regardless of how many cadences are configured.
func (p *postgreSQLScraper) familiesDue() familiesDue {
	now := p.now()
	d := familiesDue{
		relations: p.isDue(p.lastRelationRun, p.config.RelationMetrics.CollectionInterval, now),
		bloat:     p.isDue(p.lastBloatRun, p.config.BloatCollectionInterval, now),
	}
	if d.relations {
		p.lastRelationRun = now
		// The relation families are about to re-enumerate every database in
		// scope, so counts remembered for databases that have since left the
		// selection can go. clear keeps the map's storage, so an unthrottled
		// scraper pays nothing for this beyond the writes it already makes.
		clear(p.tableCounts)
	}
	if d.bloat {
		p.lastBloatRun = now
	}
	return d
}

// topQueryDue is the same decision for the top-query log scraper.
func (p *postgreSQLScraper) topQueryDue() bool {
	now := p.now()
	if !p.isDue(p.lastTopQueryRun, p.config.TopQueryCollection.Interval, now) {
		return false
	}
	p.lastTopQueryRun = now
	return true
}

// isDue reports whether a family last run at last, on a cadence of interval,
// is to run again at now. A zero interval means every scrape, and a family
// that has never run is always due.
//
// The comparison tolerates a tenth of a scrape interval of jitter. The
// controller's ticks are evenly spaced but a scrape observes the clock a
// little after its tick, so the gap between the scrape that ran a family and
// the scrape that falls exactly interval later can come out a few
// milliseconds short. Without the tolerance a 60s cadence on a 10s scrape
// would run on every seventh scrape and then slip to every eighth, which is
// not what anyone configuring 60s means. The tolerance is deliberately far
// smaller than a tick, so an interval that is not a multiple of the scrape
// interval still rounds up to the next tick rather than down.
func (p *postgreSQLScraper) isDue(last time.Time, interval time.Duration, now time.Time) bool {
	if interval <= 0 || last.IsZero() {
		return true
	}
	tolerance := p.config.ControllerConfig.CollectionInterval / 10
	return now.Sub(last)+tolerance >= interval
}

// needsDatabaseClientFor reports whether this scrape has to open a connection
// to database. The static plan says whether any per-database collector is
// enabled at all; on top of that, a collector that is enabled but not due on
// this scrape does not justify the connection either.
//
// The one exception is the table count. postgresql.table.count is reported on
// every scrape, and on scrapes where the relation families are not due it is
// served from the count remembered at the last enumeration. A database with no
// remembered count — one that appeared in the selection between two
// enumerations, or whose last enumeration failed — still needs a connection so
// the count can be read rather than invented.
func (p *postgreSQLScraper) needsDatabaseClientFor(database string, due familiesDue) bool {
	if !p.plan.needsDatabaseClient() {
		return false
	}
	if due.relations && (p.plan.tables || p.plan.indexes || p.plan.functions) {
		return true
	}
	if due.bloat && (p.plan.tableBloat || p.plan.indexBloat) {
		return true
	}
	if p.plan.tables && !due.relations {
		_, remembered := p.tableCounts[database]
		return !remembered
	}
	return false
}

// rememberTableCount stores the result of a table enumeration for the scrapes
// between now and the next one. A failed enumeration forgets the database
// rather than remembering a count of zero, so the next scrape reads the count
// again instead of reporting the failure as an empty database for the rest of
// the interval.
func (p *postgreSQLScraper) rememberTableCount(database string, count int, err error) int64 {
	if err != nil {
		delete(p.tableCounts, database)
	} else {
		p.tableCounts[database] = int64(count)
	}
	return int64(count)
}
