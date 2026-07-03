// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"sync"
	"time"
)

// XminChangeTracker tracks table changes using xmin-based change detection
type XminChangeTracker struct {
	mu             sync.RWMutex
	lastXminByOID  map[uint32]uint32
	lastSnapshot   time.Time
	schemaVersion  uint64
	changeHistory  []*ChangeEvent
	maxHistorySize int
}

// ChangeEvent represents a detected change
type ChangeEvent struct {
	TableOID   uint32
	TableName  string
	OldXmin    uint32
	NewXmin    uint32
	DetectedAt time.Time
	ChangeType string // DDL, structure, unknown
}

// NewXminChangeTracker creates a new xmin change tracker
func NewXminChangeTracker(maxHistorySize int) *XminChangeTracker {
	if maxHistorySize <= 0 {
		maxHistorySize = 1000
	}

	return &XminChangeTracker{
		lastXminByOID:  make(map[uint32]uint32),
		schemaVersion:  0,
		changeHistory:  make([]*ChangeEvent, 0, maxHistorySize),
		maxHistorySize: maxHistorySize,
	}
}

// HasChanged checks if a table has changed since last check
func (ct *XminChangeTracker) HasChanged(tableOID uint32, currentXmin uint32) bool {
	ct.mu.RLock()
	lastXmin, tracked := ct.lastXminByOID[tableOID]
	ct.mu.RUnlock()

	if !tracked {
		// First time seeing this table
		ct.UpdateXmin(tableOID, currentXmin)
		return true
	}

	return currentXmin != lastXmin
}

// UpdateXmin updates the tracked xmin for a table
func (ct *XminChangeTracker) UpdateXmin(tableOID uint32, xmin uint32) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	oldXmin, existed := ct.lastXminByOID[tableOID]
	ct.lastXminByOID[tableOID] = xmin

	if existed && oldXmin != xmin {
		// Record the change
		ct.recordChange(&ChangeEvent{
			TableOID:   tableOID,
			OldXmin:    oldXmin,
			NewXmin:    xmin,
			DetectedAt: time.Now(),
		})

		// Increment schema version
		ct.schemaVersion++
	}
}

// GetChangedTables returns all tables that have changed
func (ct *XminChangeTracker) GetChangedTables(current map[uint32]uint32) []uint32 {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	changed := make([]uint32, 0)

	for tableOID, currentXmin := range current {
		if lastXmin, tracked := ct.lastXminByOID[tableOID]; !tracked || lastXmin != currentXmin {
			changed = append(changed, tableOID)
		}
	}

	return changed
}

// UpdateSnapshot updates the entire snapshot
func (ct *XminChangeTracker) UpdateSnapshot(snapshot map[uint32]uint32) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.lastXminByOID = snapshot
	ct.lastSnapshot = time.Now()
	ct.schemaVersion++
}

// GetLastSnapshot returns the timestamp of last snapshot
func (ct *XminChangeTracker) GetLastSnapshot() time.Time {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	return ct.lastSnapshot
}

// GetSchemaVersion returns the current schema version
func (ct *XminChangeTracker) GetSchemaVersion() uint64 {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	return ct.schemaVersion
}

// GetTrackedTableCount returns the number of tracked tables
func (ct *XminChangeTracker) GetTrackedTableCount() int {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	return len(ct.lastXminByOID)
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

	ct.lastXminByOID = make(map[uint32]uint32)
	ct.lastSnapshot = time.Time{}
	ct.schemaVersion = 0
	ct.changeHistory = make([]*ChangeEvent, 0, ct.maxHistorySize)
}

// HasTableBeenTracked checks if a table has been tracked before
func (ct *XminChangeTracker) HasTableBeenTracked(tableOID uint32) bool {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	_, tracked := ct.lastXminByOID[tableOID]
	return tracked
}

// GetXmin returns the last known xmin for a table
func (ct *XminChangeTracker) GetXmin(tableOID uint32) (uint32, bool) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	xmin, exists := ct.lastXminByOID[tableOID]
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
