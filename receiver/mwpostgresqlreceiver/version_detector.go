// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// VersionDetector detects PostgreSQL version and supported features
type VersionDetector struct {
	db *IgnoredDB
}

// NewVersionDetector creates a new version detector
func NewVersionDetector(db *IgnoredDB) *VersionDetector {
	return &VersionDetector{db: db}
}

// Detect detects the PostgreSQL version and feature support
func (vd *VersionDetector) Detect(ctx context.Context) (*VersionInfo, error) {
	var versionString string
	var versionNum int32

	// Get version string and number
	err := vd.db.QueryRowContext(ctx, "SELECT version(), current_setting('server_version_num')::int").
		Scan(&versionString, &versionNum)
	if err != nil {
		return nil, fmt.Errorf("failed to detect version: %w", err)
	}

	info := &VersionInfo{
		VersionNum:    uint32(versionNum),
		VersionString: versionString,
		MajorVersion:  int(versionNum) / 10000,
		MinorVersion:  (int(versionNum) % 10000) / 100,
	}

	// Determine feature support based on version
	vd.determineFeatures(info)

	// Detect cloud platform specifics
	vd.detectCloudSpecifics(ctx, info)

	return info, nil
}

func (vd *VersionDetector) determineFeatures(info *VersionInfo) {
	majorVersion := info.MajorVersion

	// relhasoids was removed in PostgreSQL 12
	info.RelHasOids = majorVersion < 12

	// Extended statistics introduced in PostgreSQL 10, enhanced in 14
	info.ExtendedStats = majorVersion >= 10

	// Inherited statistics in PG14+
	info.InheritedStats = majorVersion >= 14

	// pg_stat_io introduced in PostgreSQL 16
	info.PgStatIO = majorVersion >= 16

	// Direct access to pg_stat improved in PG17+
	info.DirectPgStats = majorVersion >= 17

	// Partitioned tables in PostgreSQL 10+
	info.PartitionedTables = majorVersion >= 10

	// Generated columns in PostgreSQL 12+
	info.GeneratedColumns = majorVersion >= 12
}

// detectCloudSpecifics populates cloud flags from a previously detected CloudProvider.
// This avoids re-running the same detection queries that CloudDetector already ran.
func (vd *VersionDetector) detectCloudSpecifics(_ context.Context, info *VersionInfo) {
	// Cloud flags are populated later by the caller (scraper_schema.go) after
	// CloudDetector.Detect() completes, so we leave them as zero-values here.
	// This prevents duplicate queries against the database.
	_ = info
}

// SetCloudFlags populates the cloud platform booleans from a detected CloudProvider.
func SetCloudFlags(info *VersionInfo, provider CloudProvider) {
	info.IsAurora = provider == CloudProviderAWSAurora
	info.IsRDS = provider == CloudProviderAWSRDS || provider == CloudProviderAWSAurora
	info.IsAlloyDB = provider == CloudProviderGCPAlloyDB
	info.IsCloudSQL = provider == CloudProviderGCPCloudSQL
	info.IsAzure = provider == CloudProviderAzure
	info.IsHeroku = provider == CloudProviderHeroku
	info.IsCrunchy = provider == CloudProviderCrunchy
	info.IsTembo = provider == CloudProviderTembo
}

// GetFeatureMatrix returns the feature support for a version
func (vd *VersionDetector) GetFeatureMatrix(versionNum uint32) map[string]bool {
	majorVersion := int(versionNum) / 10000

	features := map[string]bool{
		"relhosoids":          majorVersion < 12,
		"extended_stats":      majorVersion >= 10,
		"inherited_stats":     majorVersion >= 14,
		"pg_stat_io":          majorVersion >= 16,
		"direct_pg_stats":     majorVersion >= 17,
		"partitioned_tables":  majorVersion >= 10,
		"generated_columns":   majorVersion >= 12,
		"logical_replication": majorVersion >= 10,
		"subscriptions":       majorVersion >= 10,
		"wal_compression":     majorVersion >= 14,
		"identity_columns":    majorVersion >= 10,
		"stored_procedures":   majorVersion >= 11,
		"jit_compilation":     majorVersion >= 11,
		"parallel_index_scan": majorVersion >= 13,
	}

	return features
}

// ParseVersionNumber parses version string to numeric form
func (vd *VersionDetector) ParseVersionNumber(versionStr string) (uint32, error) {
	// Extract version from strings like "PostgreSQL 15.2 on x86_64-pc-linux-gnu"
	parts := strings.Fields(versionStr)
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid version string: %s", versionStr)
	}

	versionPart := parts[1]
	versionComponents := strings.Split(versionPart, ".")

	if len(versionComponents) < 1 {
		return 0, fmt.Errorf("cannot parse version: %s", versionPart)
	}

	major, err := strconv.Atoi(versionComponents[0])
	if err != nil {
		return 0, err
	}

	minor := 0
	if len(versionComponents) > 1 {
		minor, _ = strconv.Atoi(versionComponents[1])
	}

	// Convert to numeric form: major * 10000 + minor * 100
	return uint32(major*10000 + minor*100), nil
}

// IsSupported checks if a PostgreSQL version is supported
func (vd *VersionDetector) IsSupported(versionNum uint32) bool {
	majorVersion := int(versionNum) / 10000
	// Support PG10 and above
	return majorVersion >= 10
}

// GetVersionDescription returns a human-readable version description
func (vd *VersionDetector) GetVersionDescription(info *VersionInfo) string {
	return fmt.Sprintf("PostgreSQL %d.%d", info.MajorVersion, info.MinorVersion)
}
