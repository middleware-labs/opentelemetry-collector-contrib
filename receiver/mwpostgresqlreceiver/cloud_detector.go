// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"strings"
)

// CloudDetector detects the cloud platform hosting PostgreSQL
type CloudDetector struct {
	db *IgnoredDB
}

// NewCloudDetector creates a new cloud detector
func NewCloudDetector(db *IgnoredDB) *CloudDetector {
	return &CloudDetector{db: db}
}

// Detect detects the cloud platform
func (cd *CloudDetector) Detect(ctx context.Context) (CloudProvider, *CloudMetadata, error) {
	// Detection order (most specific first)

	// 1. AWS Aurora (check for aurora_control_plane)
	if cd.hasAuroraControlPlane(ctx) {
		return CloudProviderAWSAurora, cd.getAuroraMetadata(ctx), nil
	}

	// 2. AWS RDS (check for rds_superuser role)
	if cd.hasRDSSuperuser(ctx) {
		return CloudProviderAWSRDS, cd.getRDSMetadata(ctx), nil
	}

	// 3. Google AlloyDB (check for alloydb catalog)
	if cd.hasAlloyDBCatalog(ctx) {
		return CloudProviderGCPAlloyDB, cd.getAlloyDBMetadata(ctx), nil
	}

	// 4. Google Cloud SQL (check for Cloud SQL metadata)
	if cd.hasCloudSQLMetadata(ctx) {
		return CloudProviderGCPCloudSQL, cd.getCloudSQLMetadata(ctx), nil
	}

	// 5. Azure Database (check for azure_superuser)
	if cd.hasAzureSuperuser(ctx) {
		return CloudProviderAzure, cd.getAzureMetadata(ctx), nil
	}

	// 6. Heroku (check for heroku_ext schema)
	if cd.hasHerokuExt(ctx) {
		return CloudProviderHeroku, cd.getHerokuMetadata(ctx), nil
	}

	// 7. Crunchy Bridge (check for specific config)
	if cd.hasCrunchyConfig(ctx) {
		return CloudProviderCrunchy, cd.getCrunchyMetadata(ctx), nil
	}

	// 8. Tembo Cloud (check for tembo extensions)
	if cd.hasTemboExtensions(ctx) {
		return CloudProviderTembo, cd.getTemboMetadata(ctx), nil
	}

	// Default: Self-hosted
	return CloudProviderSelfHosted, cd.getSelfHostedMetadata(ctx), nil
}

// Detection helper methods

func (cd *CloudDetector) hasAuroraControlPlane(ctx context.Context) bool {
	var exists bool
	err := cd.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'aurora_control_plane')").
		Scan(&exists)
	return err == nil && exists
}

func (cd *CloudDetector) hasRDSSuperuser(ctx context.Context) bool {
	var exists bool
	err := cd.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = 'rds_superuser')").
		Scan(&exists)
	return err == nil && exists
}

func (cd *CloudDetector) hasAlloyDBCatalog(ctx context.Context) bool {
	var exists bool
	err := cd.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = 'alloydb_version')").
		Scan(&exists)
	return err == nil && exists
}

func (cd *CloudDetector) hasCloudSQLMetadata(ctx context.Context) bool {
	var exists bool
	err := cd.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'cloudsql_postgres_advisory_locks')").
		Scan(&exists)
	return err == nil && exists
}

func (cd *CloudDetector) hasAzureSuperuser(ctx context.Context) bool {
	var exists bool
	err := cd.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname LIKE '%azure%')").
		Scan(&exists)
	return err == nil && exists
}

func (cd *CloudDetector) hasHerokuExt(ctx context.Context) bool {
	var exists bool
	err := cd.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'heroku_ext')").
		Scan(&exists)
	return err == nil && exists
}

func (cd *CloudDetector) hasCrunchyConfig(ctx context.Context) bool {
	var exists bool
	err := cd.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = 'crunchy_superuser')").
		Scan(&exists)
	return err == nil && exists
}

func (cd *CloudDetector) hasTemboExtensions(ctx context.Context) bool {
	var exists bool
	err := cd.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname LIKE 'tembo%')").
		Scan(&exists)
	return err == nil && exists
}

// Metadata retrieval methods

func (cd *CloudDetector) getAuroraMetadata(ctx context.Context) *CloudMetadata {
	metadata := &CloudMetadata{
		Provider: CloudProviderAWSAurora,
		SupportedFeatures: map[string]bool{
			"table_stats":      true,
			"index_stats":      true,
			"column_stats":     true,
			"io_stats":         false,
			"extended_stats":   false,
			"helper_functions": true,
		},
		PrivilegeLevel:     PrivilegeLevelLimited,
		HasHelperFunctions: true,
		ReplicationEnabled: true,
		BackupEnabled:      true,
		EncryptionEnabled:  true,
	}

	// Try to get region from parameter
	var region string
	_ = cd.db.QueryRowContext(ctx,
		"SELECT current_setting('rds.region', true)").Scan(&region)
	if region != "" {
		metadata.Region = region
	}

	return metadata
}

func (cd *CloudDetector) getRDSMetadata(ctx context.Context) *CloudMetadata {
	metadata := &CloudMetadata{
		Provider: CloudProviderAWSRDS,
		SupportedFeatures: map[string]bool{
			"table_stats":      true,
			"index_stats":      true,
			"column_stats":     true,
			"io_stats":         false,
			"extended_stats":   false,
			"helper_functions": true,
		},
		PrivilegeLevel:     PrivilegeLevelLimited,
		HasHelperFunctions: true,
		BackupEnabled:      true,
		EncryptionEnabled:  true,
	}

	var region string
	_ = cd.db.QueryRowContext(ctx,
		"SELECT current_setting('rds.region', true)").Scan(&region)
	if region != "" {
		metadata.Region = region
	}

	return metadata
}

func (cd *CloudDetector) getAlloyDBMetadata(ctx context.Context) *CloudMetadata {
	return &CloudMetadata{
		Provider: CloudProviderGCPAlloyDB,
		SupportedFeatures: map[string]bool{
			"table_stats":      true,
			"index_stats":      true,
			"column_stats":     true,
			"io_stats":         true,
			"extended_stats":   true,
			"helper_functions": false,
		},
		PrivilegeLevel:     PrivilegeLevelSuperuser,
		HasHelperFunctions: false,
		ReplicationEnabled: true,
		BackupEnabled:      true,
		EncryptionEnabled:  true,
	}
}

func (cd *CloudDetector) getCloudSQLMetadata(ctx context.Context) *CloudMetadata {
	return &CloudMetadata{
		Provider: CloudProviderGCPCloudSQL,
		SupportedFeatures: map[string]bool{
			"table_stats":      true,
			"index_stats":      true,
			"column_stats":     true,
			"io_stats":         false,
			"extended_stats":   false,
			"helper_functions": false,
		},
		PrivilegeLevel:     PrivilegeLevelLimited,
		HasHelperFunctions: false,
		BackupEnabled:      true,
		EncryptionEnabled:  true,
	}
}

func (cd *CloudDetector) getAzureMetadata(ctx context.Context) *CloudMetadata {
	metadata := &CloudMetadata{
		Provider: CloudProviderAzure,
		SupportedFeatures: map[string]bool{
			"table_stats":      true,
			"index_stats":      false,
			"column_stats":     false,
			"io_stats":         false,
			"extended_stats":   false,
			"helper_functions": false,
		},
		PrivilegeLevel:     PrivilegeLevelLimited,
		HasHelperFunctions: false,
		BackupEnabled:      true,
		EncryptionEnabled:  true,
	}

	// Detect Azure server type (flexible vs single)
	var version string
	err := cd.db.QueryRowContext(ctx, "SELECT version()").Scan(&version)
	if err == nil && strings.Contains(version, "Azure Database") {
		if strings.Contains(version, "Flexible") {
			metadata.InstanceType = "flexible_server"
		} else {
			metadata.InstanceType = "single_server"
		}
	}

	return metadata
}

func (cd *CloudDetector) getHerokuMetadata(ctx context.Context) *CloudMetadata {
	return &CloudMetadata{
		Provider: CloudProviderHeroku,
		SupportedFeatures: map[string]bool{
			"table_stats":      true,
			"index_stats":      false,
			"column_stats":     false,
			"io_stats":         false,
			"extended_stats":   false,
			"helper_functions": false,
		},
		PrivilegeLevel:     PrivilegeLevelReadonly,
		HasHelperFunctions: false,
		BackupEnabled:      true,
		EncryptionEnabled:  true,
	}
}

func (cd *CloudDetector) getCrunchyMetadata(ctx context.Context) *CloudMetadata {
	return &CloudMetadata{
		Provider: CloudProviderCrunchy,
		SupportedFeatures: map[string]bool{
			"table_stats":      true,
			"index_stats":      true,
			"column_stats":     true,
			"io_stats":         false,
			"extended_stats":   true,
			"helper_functions": true,
		},
		PrivilegeLevel:     PrivilegeLevelLimited,
		HasHelperFunctions: true,
		ReplicationEnabled: true,
		BackupEnabled:      true,
		EncryptionEnabled:  true,
	}
}

func (cd *CloudDetector) getTemboMetadata(ctx context.Context) *CloudMetadata {
	return &CloudMetadata{
		Provider: CloudProviderTembo,
		SupportedFeatures: map[string]bool{
			"table_stats":      true,
			"index_stats":      true,
			"column_stats":     true,
			"io_stats":         true,
			"extended_stats":   true,
			"helper_functions": false,
		},
		PrivilegeLevel:     PrivilegeLevelSuperuser,
		HasHelperFunctions: false,
		ReplicationEnabled: true,
		BackupEnabled:      true,
		EncryptionEnabled:  true,
	}
}

func (cd *CloudDetector) getSelfHostedMetadata(ctx context.Context) *CloudMetadata {
	return &CloudMetadata{
		Provider: CloudProviderSelfHosted,
		SupportedFeatures: map[string]bool{
			"table_stats":      true,
			"index_stats":      true,
			"column_stats":     true,
			"io_stats":         true,
			"extended_stats":   true,
			"helper_functions": true,
		},
		PrivilegeLevel:     PrivilegeLevelSuperuser,
		HasHelperFunctions: true,
		ReplicationEnabled: true,
		BackupEnabled:      true,
		EncryptionEnabled:  false,
	}
}
