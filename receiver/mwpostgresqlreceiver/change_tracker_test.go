// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTrackerDatabasesDoNotCollide is the core of the per-database keying.
// PostgreSQL OIDs are only unique within a database — every database is created
// from template1 and allocates from the same starting point, so two databases
// routinely hold the same OID for unrelated tables. Keyed by bare OID, one
// database's snapshot answers questions asked about another.
func TestTrackerDatabasesDoNotCollide(t *testing.T) {
	ct := NewXminChangeTracker(10)

	// The same OID in two databases, with different xmins.
	const sharedOID = uint32(16428)
	ct.UpdateSnapshot("db_a", map[uint32]uint32{sharedOID: 100})
	ct.UpdateSnapshot("db_b", map[uint32]uint32{sharedOID: 200})

	// Each database sees its own xmin, not the other's.
	xminA, okA := ct.GetXmin("db_a", sharedOID)
	require.True(t, okA)
	require.Equal(t, uint32(100), xminA)

	xminB, okB := ct.GetXmin("db_b", sharedOID)
	require.True(t, okB)
	require.Equal(t, uint32(200), xminB)

	// Neither reports a change against its own snapshot...
	require.False(t, ct.HasChanged("db_a", sharedOID, 100))
	require.False(t, ct.HasChanged("db_b", sharedOID, 200))

	// ...and each correctly reports a change against the other's value.
	require.True(t, ct.HasChanged("db_a", sharedOID, 200))
	require.True(t, ct.HasChanged("db_b", sharedOID, 100))
}

// TestTrackerSnapshotIsPerDatabase covers the wholesale-replacement bug: writing
// one database's snapshot used to discard every other database's.
func TestTrackerSnapshotIsPerDatabase(t *testing.T) {
	ct := NewXminChangeTracker(10)

	ct.UpdateSnapshot("db_a", map[uint32]uint32{1: 10, 2: 20})
	ct.UpdateSnapshot("db_b", map[uint32]uint32{3: 30})
	ct.UpdateSnapshot("db_c", map[uint32]uint32{4: 40})

	require.Equal(t, 3, ct.GetTrackedDatabaseCount())
	require.Equal(t, 4, ct.GetTrackedTableCount())

	// db_a survived the later writes intact.
	require.True(t, ct.HasTableBeenTracked("db_a", 1))
	require.True(t, ct.HasTableBeenTracked("db_a", 2))
	require.True(t, ct.HasTableBeenTracked("db_b", 3))
	require.True(t, ct.HasTableBeenTracked("db_c", 4))

	// A table is not visible under a database that does not hold it.
	require.False(t, ct.HasTableBeenTracked("db_b", 1))
}

// TestTrackerEvictsDroppedTables covers the unbounded-growth defect. PostgreSQL
// does not reuse OIDs, so without eviction a database with DDL churn grows the
// map forever. Replacing a database's snapshot must drop what is no longer in it.
func TestTrackerEvictsDroppedTables(t *testing.T) {
	ct := NewXminChangeTracker(10)

	ct.UpdateSnapshot("db_a", map[uint32]uint32{1: 10, 2: 20, 3: 30})
	require.Equal(t, 3, ct.GetTrackedTableCount())

	// Two tables dropped, one new one created.
	ct.UpdateSnapshot("db_a", map[uint32]uint32{3: 30, 4: 40})

	require.Equal(t, 2, ct.GetTrackedTableCount(), "dropped OIDs must not persist")
	require.False(t, ct.HasTableBeenTracked("db_a", 1))
	require.False(t, ct.HasTableBeenTracked("db_a", 2))
	require.True(t, ct.HasTableBeenTracked("db_a", 3))
	require.True(t, ct.HasTableBeenTracked("db_a", 4))
}

// TestTrackerDoesNotGrowAcrossChurn simulates repeated create/drop cycles and
// asserts the tracker stays bounded rather than accumulating one entry per OID
// ever seen.
func TestTrackerDoesNotGrowAcrossChurn(t *testing.T) {
	ct := NewXminChangeTracker(10)

	nextOID := uint32(16384)
	for cycle := range 500 {
		// Ten live tables, all with fresh OIDs each cycle, as a drop/recreate
		// would produce.
		snapshot := make(map[uint32]uint32, 10)
		for range 10 {
			snapshot[nextOID] = uint32(cycle)
			nextOID++
		}
		ct.UpdateSnapshot("db_churn", snapshot)
	}

	require.Equal(t, 10, ct.GetTrackedTableCount(),
		"tracker must hold only the current tables, not every OID ever seen")
	require.Equal(t, 1, ct.GetTrackedDatabaseCount())
}

// TestTrackerBoundsDatabaseCount checks the backstop for a server whose
// databases are themselves created and dropped continuously.
func TestTrackerBoundsDatabaseCount(t *testing.T) {
	ct := NewXminChangeTracker(10)
	ct.maxTrackedDatabases = 5

	for i := range 50 {
		ct.UpdateSnapshot(string(rune('a'+i%26))+string(rune('a'+i/26)), map[uint32]uint32{1: uint32(i)})
	}

	require.LessOrEqual(t, ct.GetTrackedDatabaseCount(), 5,
		"tracked databases must stay bounded")
}

// TestHasChangedDoesNotRecord guards a subtle ordering hazard: detection must
// not advance the snapshot. If an unseen table were recorded at detection time,
// and the collection that followed were skipped or failed, that table would
// look unchanged forever and its schema would never be emitted.
func TestHasChangedDoesNotRecord(t *testing.T) {
	ct := NewXminChangeTracker(10)

	require.True(t, ct.HasChanged("db_a", 42, 100))
	require.False(t, ct.HasTableBeenTracked("db_a", 42),
		"detection must not record; only a completed collection may")

	// Still reported as changed on a subsequent check.
	require.True(t, ct.HasChanged("db_a", 42, 100))

	// Only a snapshot records it.
	ct.UpdateSnapshot("db_a", map[uint32]uint32{42: 100})
	require.False(t, ct.HasChanged("db_a", 42, 100))
}

// TestGetChangedTablesIsPerDatabase checks the bulk comparison path is scoped
// the same way as the single-table one.
func TestGetChangedTablesIsPerDatabase(t *testing.T) {
	ct := NewXminChangeTracker(10)
	ct.UpdateSnapshot("db_a", map[uint32]uint32{1: 10, 2: 20})

	// Same OIDs and xmins, different database: everything is unknown, so
	// everything is changed.
	require.Len(t, ct.GetChangedTables("db_b", map[uint32]uint32{1: 10, 2: 20}), 2)

	// Against its own database, nothing changed.
	require.Empty(t, ct.GetChangedTables("db_a", map[uint32]uint32{1: 10, 2: 20}))

	// One altered table is reported alone.
	changed := ct.GetChangedTables("db_a", map[uint32]uint32{1: 10, 2: 99})
	require.Equal(t, []uint32{2}, changed)
}

// TestGetLastSnapshotForIsPerDatabase checks the refresh clock is per database,
// so one database's collection does not reset another's refresh timer.
func TestGetLastSnapshotForIsPerDatabase(t *testing.T) {
	ct := NewXminChangeTracker(10)

	_, tracked := ct.GetLastSnapshotFor("db_a")
	require.False(t, tracked)

	ct.UpdateSnapshot("db_a", map[uint32]uint32{1: 10})

	atA, tracked := ct.GetLastSnapshotFor("db_a")
	require.True(t, tracked)
	require.False(t, atA.IsZero())

	_, tracked = ct.GetLastSnapshotFor("db_b")
	require.False(t, tracked, "collecting db_a must not mark db_b as snapshotted")
}
