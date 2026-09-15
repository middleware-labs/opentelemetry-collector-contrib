// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"sync"
	"time"
)

// defaultMaxTrackedDatabases bounds how many databases the tracker will hold
// snapshots for. A server whose databases are created and dropped continuously
// would otherwise accumulate one snapshot per database name ever seen.
const defaultMaxTrackedDatabases = 500

// XminChangeTracker tracks table changes using xmin-based change detection.
//
// Snapshots are held per database. PostgreSQL OIDs are only unique within a
// database — pg_class is a per-database catalog, so two databases can each hold
// a row with the same OID describing unrelated tables. A single map keyed by
// bare OID therefore cannot distinguish them, and one database's snapshot
// silently answers change-detection questions asked about another.
type XminChangeTracker struct {
	mu sync.RWMutex
	// lastXminByDB maps a database name to that database's OID -> xmin
	// snapshot. Nesting rather than using a composite (database, OID) key means
	// a database's snapshot can be replaced wholesale, which is both the update
	// and the eviction: OIDs absent from the new snapshot simply cease to exist.
	// Dropped tables therefore do not accumulate, and since PostgreSQL does not
	// reuse OIDs, that accumulation would otherwise be unbounded.
	lastXminByDB        map[string]map[uint32]uint32
	lastSnapshotByDB    map[string]time.Time
	lastSnapshot        time.Time
	schemaVersion       uint64
	changeHistory       []*ChangeEvent
	maxHistorySize      int
	maxTrackedDatabases int
}

// ChangeEvent represents a detected change
type ChangeEvent struct {
	DatabaseName string
	TableOID     uint32
	TableName    string
	OldXmin      uint32
	NewXmin      uint32
	DetectedAt   time.Time
	ChangeType   string // DDL, structure, unknown
}

// NewXminChangeTracker creates a new xmin change tracker
func NewXminChangeTracker(maxHistorySize int) *XminChangeTracker {
	if maxHistorySize <= 0 {
		maxHistorySize = 1000
	}

	return &XminChangeTracker{
		lastXminByDB:        make(map[string]map[uint32]uint32),
		lastSnapshotByDB:    make(map[string]time.Time),
		schemaVersion:       0,
		changeHistory:       make([]*ChangeEvent, 0, maxHistorySize),
		maxHistorySize:      maxHistorySize,
		maxTrackedDatabases: defaultMaxTrackedDatabases,
	}
}

// HasChanged reports whether a table has changed since the last snapshot.
//
// This is a pure read: an untracked table is reported as changed but is NOT
// recorded, because recording it here would mark a newly created table as
// already-seen before its schema had actually been collected. If the ensuing
// collection were then skipped or failed, that table would look unchanged on
// every later cycle and its schema would never be sent. The snapshot is
// advanced only by UpdateSnapshot, once a collection has actually happened.
func (ct *XminChangeTracker) HasChanged(database string, tableOID, currentXmin uint32) bool {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	lastXmin, tracked := ct.lastXminByDB[database][tableOID]
	if !tracked {
		return true
	}

	return currentXmin != lastXmin
}

// Compare reports whether a table is tracked and, if so, whether its xmin
// differs from the snapshot. An untracked table is reported as not changed:
// callers treat it as new, which is distinct from altered.
func (ct *XminChangeTracker) Compare(database string, tableOID, currentXmin uint32) (tracked, changed bool) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	lastXmin, tracked := ct.lastXminByDB[database][tableOID]
	return tracked, tracked && lastXmin != currentXmin
}

// UpdateXmin updates the tracked xmin for a table
func (ct *XminChangeTracker) UpdateXmin(database string, tableOID, xmin uint32) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	dbSnapshot, ok := ct.lastXminByDB[database]
	if !ok {
		ct.evictIfAtCapacityLocked()
		dbSnapshot = make(map[uint32]uint32)
		ct.lastXminByDB[database] = dbSnapshot
	}

	oldXmin, existed := dbSnapshot[tableOID]
	dbSnapshot[tableOID] = xmin

	if existed && oldXmin != xmin {
		// Record the change
		ct.recordChange(&ChangeEvent{
			DatabaseName: database,
			TableOID:     tableOID,
			OldXmin:      oldXmin,
			NewXmin:      xmin,
			DetectedAt:   time.Now(),
		})

		// Increment schema version
		ct.schemaVersion++
	}
}

// GetChangedTables returns the OIDs in current whose xmin differs from the
// tracked snapshot for the given database, or which are not tracked at all.
func (ct *XminChangeTracker) GetChangedTables(database string, current map[uint32]uint32) []uint32 {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	dbSnapshot := ct.lastXminByDB[database]
	changed := make([]uint32, 0)

	for tableOID, currentXmin := range current {
		if lastXmin, tracked := dbSnapshot[tableOID]; !tracked || lastXmin != currentXmin {
			changed = append(changed, tableOID)
		}
	}

	return changed
}

// UpdateSnapshot replaces the snapshot for a single database.
//
// Replacement is deliberate and is what bounds the tracker: OIDs that are no
// longer present have been dropped, and are removed by not being carried over.
// Only the named database is affected — other databases keep their snapshots.
func (ct *XminChangeTracker) UpdateSnapshot(database string, snapshot map[uint32]uint32) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if _, exists := ct.lastXminByDB[database]; !exists {
		ct.evictIfAtCapacityLocked()
	}

	ct.lastXminByDB[database] = snapshot
	now := time.Now()
	ct.lastSnapshotByDB[database] = now
	ct.lastSnapshot = now
	ct.schemaVersion++
}

// evictIfAtCapacityLocked drops the least recently snapshotted database to make
// room for a new one. Callers must hold the write lock.
func (ct *XminChangeTracker) evictIfAtCapacityLocked() {
	if ct.maxTrackedDatabases <= 0 || len(ct.lastXminByDB) < ct.maxTrackedDatabases {
		return
	}

	var oldestDB string
	var oldestAt time.Time
	for db := range ct.lastXminByDB {
		at, ok := ct.lastSnapshotByDB[db]
		if !ok {
			// Never snapshotted: evict in preference to anything that has been.
			oldestDB = db
			break
		}
		if oldestDB == "" || at.Before(oldestAt) {
			oldestDB, oldestAt = db, at
		}
	}

	if oldestDB != "" {
		delete(ct.lastXminByDB, oldestDB)
		delete(ct.lastSnapshotByDB, oldestDB)
	}
}

// GetLastSnapshot returns the timestamp of the most recent snapshot across all
// databases.
func (ct *XminChangeTracker) GetLastSnapshot() time.Time {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	return ct.lastSnapshot
}

// GetLastSnapshotFor returns the timestamp of the last snapshot for a single
// database, and whether that database has been snapshotted at all.
func (ct *XminChangeTracker) GetLastSnapshotFor(database string) (time.Time, bool) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	at, ok := ct.lastSnapshotByDB[database]
	return at, ok
}

// GetSchemaVersion returns the current schema version
func (ct *XminChangeTracker) GetSchemaVersion() uint64 {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	return ct.schemaVersion
}

// GetTrackedTableCount returns the number of tracked tables across all
// databases.
func (ct *XminChangeTracker) GetTrackedTableCount() int {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	total := 0
	for _, dbSnapshot := range ct.lastXminByDB {
		total += len(dbSnapshot)
	}
	return total
}

// TrackedTableCountFor returns the number of tables tracked for one database.
func (ct *XminChangeTracker) TrackedTableCountFor(database string) int {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	return len(ct.lastXminByDB[database])
}

// GetTrackedDatabaseCount returns the number of databases holding a snapshot.
func (ct *XminChangeTracker) GetTrackedDatabaseCount() int {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	return len(ct.lastXminByDB)
}

// GetChangeHistory returns the change history
func (ct *XminChangeTracker) GetChangeHistory() []*ChangeEvent {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	result := make([]*ChangeEvent, len(ct.changeHistory))
	copy(result, ct.changeHistory)
	return result
}

// ClearChangeHistory clears the change history
func (ct *XminChangeTracker) ClearChangeHistory() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.changeHistory = make([]*ChangeEvent, 0, ct.maxHistorySize)
}

// recordChange records a change event internally
func (ct *XminChangeTracker) recordChange(event *ChangeEvent) {
	if len(ct.changeHistory) >= ct.maxHistorySize {
		// Remove oldest entry
		ct.changeHistory = ct.changeHistory[1:]
	}

	ct.changeHistory = append(ct.changeHistory, event)
}

// Reset resets the tracker state
func (ct *XminChangeTracker) Reset() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.lastXminByDB = make(map[string]map[uint32]uint32)
	ct.lastSnapshotByDB = make(map[string]time.Time)
	ct.lastSnapshot = time.Time{}
	ct.schemaVersion = 0
	ct.changeHistory = make([]*ChangeEvent, 0, ct.maxHistorySize)
}

// HasTableBeenTracked checks if a table has been tracked before
func (ct *XminChangeTracker) HasTableBeenTracked(database string, tableOID uint32) bool {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	_, tracked := ct.lastXminByDB[database][tableOID]
	return tracked
}

// GetXmin returns the last known xmin for a table
func (ct *XminChangeTracker) GetXmin(database string, tableOID uint32) (uint32, bool) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	xmin, exists := ct.lastXminByDB[database][tableOID]
	return xmin, exists
}

// TimeSinceLastSnapshot returns the duration since last snapshot
func (ct *XminChangeTracker) TimeSinceLastSnapshot() time.Duration {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	if ct.lastSnapshot.IsZero() {
		return 0
	}

	return time.Since(ct.lastSnapshot)
}
