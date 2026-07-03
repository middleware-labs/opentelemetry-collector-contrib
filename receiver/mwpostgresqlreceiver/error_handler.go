// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ErrorCategory represents the type of error
type ErrorCategory string

const (
	ErrorCategoryPermission  ErrorCategory = "permission"
	ErrorCategoryUnsupported ErrorCategory = "unsupported"
	ErrorCategoryConnection  ErrorCategory = "connection"
	ErrorCategoryTimeout     ErrorCategory = "timeout"
	ErrorCategoryQuery       ErrorCategory = "query"
	ErrorCategoryUnknown     ErrorCategory = "unknown"
)

// ErrorAction represents the action to take on an error
type ErrorAction string

const (
	ErrorActionFallback        ErrorAction = "fallback"
	ErrorActionHelperFunction  ErrorAction = "helper_function"
	ErrorActionSkipTable       ErrorAction = "skip_table"
	ErrorActionRedetectVersion ErrorAction = "redetect_version"
	ErrorActionContinue        ErrorAction = "continue"
	ErrorActionStop            ErrorAction = "stop"
)

// ErrorHandler manages error recovery and classification
type ErrorHandler struct {
	logger Logger
	db     *IgnoredDB
}

// NewErrorHandler creates a new error handler
func NewErrorHandler(db *IgnoredDB, logger Logger) *ErrorHandler {
	return &ErrorHandler{
		logger: logger,
		db:     db,
	}
}

// ClassifyError classifies an error and returns the appropriate action
func (eh *ErrorHandler) ClassifyError(err error) (ErrorCategory, ErrorAction, string) {
	if err == nil {
		return ErrorCategoryUnknown, ErrorActionContinue, ""
	}

	errStr := err.Error()

	// Permission errors
	if strings.Contains(errStr, "permission denied") ||
		strings.Contains(errStr, "insufficient privilege") ||
		strings.Contains(errStr, "must be owner") {
		return ErrorCategoryPermission, ErrorActionHelperFunction, "Insufficient permissions - trying helper function"
	}

	// Feature not supported
	if strings.Contains(errStr, "does not exist") ||
		strings.Contains(errStr, "syntax error") ||
		strings.Contains(errStr, "unsupported") {
		return ErrorCategoryUnsupported, ErrorActionRedetectVersion, "Feature not supported - redetecting version"
	}

	// Connection errors
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "i/o timeout") ||
		strings.Contains(errStr, "broken pipe") {
		return ErrorCategoryConnection, ErrorActionStop, "Connection error - cannot continue"
	}

	// Timeout errors
	if strings.Contains(errStr, "context deadline exceeded") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "cancellation requested") {
		return ErrorCategoryTimeout, ErrorActionSkipTable, "Query timeout - skipping table"
	}

	// Query errors
	if strings.Contains(errStr, "ERROR") || strings.Contains(errStr, "error") {
		return ErrorCategoryQuery, ErrorActionContinue, "Query error - continuing with next item"
	}

	return ErrorCategoryUnknown, ErrorActionContinue, "Unknown error - continuing"
}

// RecoverFromError attempts to recover from an error
func (eh *ErrorHandler) RecoverFromError(ctx context.Context, category ErrorCategory, originalErr error) (bool, error) {
	switch category {
	case ErrorCategoryPermission:
		return eh.recoverFromPermissionError(ctx)

	case ErrorCategoryUnsupported:
		return eh.recoverFromUnsupportedError(ctx)

	case ErrorCategoryConnection:
		// Cannot recover from connection errors
		return false, originalErr

	case ErrorCategoryTimeout:
		// Cannot recover from timeout, but can continue
		return false, originalErr

	case ErrorCategoryQuery:
		// Continue on query errors
		return true, nil

	default:
		return false, originalErr
	}
}

// recoverFromPermissionError attempts to recover from permission errors
func (eh *ErrorHandler) recoverFromPermissionError(ctx context.Context) (bool, error) {
	// Check if helper functions are available
	var exists bool
	err := eh.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_proc WHERE proname = 'pganalyze_get_table_stats')").
		Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check for helper functions: %w", err)
	}

	if exists {
		eh.logger.Info("Using helper functions for limited schema collection")
		return true, nil
	}

	eh.logger.Warn("No helper functions available for recovery")
	return false, errors.New("insufficient privileges and no helper functions available")
}

// recoverFromUnsupportedError attempts to recover from unsupported feature errors
func (eh *ErrorHandler) recoverFromUnsupportedError(ctx context.Context) (bool, error) {
	// Re-detect version to ensure we're using correct SQL
	var versionNum int32
	err := eh.db.QueryRowContext(ctx, "SELECT current_setting('server_version_num')::int").
		Scan(&versionNum)

	if err != nil {
		return false, fmt.Errorf("failed to re-detect version: %w", err)
	}

	eh.logger.Info("Version re-detected", "version_num", versionNum)
	return true, nil
}

// ShouldContinueOnError determines if collection should continue after an error
func (eh *ErrorHandler) ShouldContinueOnError(category ErrorCategory, severity string) bool {
	switch category {
	case ErrorCategoryConnection:
		return false // Never continue on connection errors

	case ErrorCategoryTimeout:
		return severity == "warning" // Only continue on warning-level timeouts

	case ErrorCategoryPermission:
		return true // Continue, but with limited capabilities

	case ErrorCategoryUnsupported:
		return true // Continue with fallback approach

	case ErrorCategoryQuery:
		return true // Skip problematic query and continue

	default:
		return true // Default to continuing
	}
}

// HealthMonitor tracks collection health
type HealthMonitor struct {
	lastSuccessfulCollection time.Time
	lastError                error
	lastErrorTime            time.Time
	consecutiveFailures      int
	maxConsecutiveFailures   int
	errorHistory             []*ErrorInfo
	maxHistorySize           int
	mu                       sync.Mutex
	logger                   Logger
}

// NewHealthMonitor creates a new health monitor
func NewHealthMonitor(maxConsecutiveFailures int, logger Logger) *HealthMonitor {
	if maxConsecutiveFailures <= 0 {
		maxConsecutiveFailures = 5
	}

	return &HealthMonitor{
		maxConsecutiveFailures: maxConsecutiveFailures,
		errorHistory:           make([]*ErrorInfo, 0, 100),
		maxHistorySize:         100,
		logger:                 logger,
	}
}

// UpdateStatus updates the health status
func (hm *HealthMonitor) UpdateStatus(success bool, err error) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	if success {
		hm.lastSuccessfulCollection = time.Now()
		hm.consecutiveFailures = 0
		hm.lastError = nil
	} else {
		hm.consecutiveFailures++
		hm.lastError = err
		hm.lastErrorTime = time.Now()

		// Record error in history
		hm.recordError(&ErrorInfo{
			Message:   err.Error(),
			Timestamp: time.Now(),
			Severity:  "error",
		})

		// Alert after max consecutive failures
		if hm.consecutiveFailures >= hm.maxConsecutiveFailures {
			hm.logger.Error("Schema collection failing repeatedly",
				"consecutive_failures", hm.consecutiveFailures,
				"last_error", err.Error())
		}
	}
}

// IsHealthy checks if the collector is healthy
func (hm *HealthMonitor) IsHealthy() bool {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	// Healthy if: Last successful collection < 2 hours ago
	if hm.lastSuccessfulCollection.IsZero() {
		return false
	}

	return time.Since(hm.lastSuccessfulCollection) < 2*time.Hour
}

// GetLastError returns the last error
func (hm *HealthMonitor) GetLastError() error {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	return hm.lastError
}

// GetConsecutiveFailures returns the number of consecutive failures
func (hm *HealthMonitor) GetConsecutiveFailures() int {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	return hm.consecutiveFailures
}

// GetLastSuccessfulCollection returns the time of last successful collection
func (hm *HealthMonitor) GetLastSuccessfulCollection() time.Time {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	return hm.lastSuccessfulCollection
}

// GetErrorHistory returns the error history
func (hm *HealthMonitor) GetErrorHistory() []*ErrorInfo {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	result := make([]*ErrorInfo, len(hm.errorHistory))
	copy(result, hm.errorHistory)
	return result
}

// recordError records an error in history
func (hm *HealthMonitor) recordError(err *ErrorInfo) {
	if len(hm.errorHistory) >= hm.maxHistorySize {
		// Remove oldest entry
		hm.errorHistory = hm.errorHistory[1:]
	}

	hm.errorHistory = append(hm.errorHistory, err)
}

// Reset resets the health monitor
func (hm *HealthMonitor) Reset() {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.lastSuccessfulCollection = time.Time{}
	hm.lastError = nil
	hm.lastErrorTime = time.Time{}
	hm.consecutiveFailures = 0
	hm.errorHistory = make([]*ErrorInfo, 0, 100)
}
