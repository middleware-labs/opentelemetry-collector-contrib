// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import "time"

// CollectionType represents the type of schema collection event
type CollectionType string

const (
	CollectionTypeSnapshot CollectionType = "snapshot"
	CollectionTypeRefresh  CollectionType = "refresh"
	CollectionTypeSettings CollectionType = "settings"
)

// CloudProvider represents the cloud platform hosting PostgreSQL
type CloudProvider string

const (
	CloudProviderSelfHosted  CloudProvider = "self_hosted"
	CloudProviderAWSRDS      CloudProvider = "aws_rds"
	CloudProviderAWSAurora   CloudProvider = "aws_aurora"
	CloudProviderGCPCloudSQL CloudProvider = "gcp_cloud_sql"
	CloudProviderGCPAlloyDB  CloudProvider = "gcp_alloydb"
	CloudProviderAzure       CloudProvider = "azure"
	CloudProviderHeroku      CloudProvider = "heroku"
	CloudProviderCrunchy     CloudProvider = "crunchy_bridge"
	CloudProviderTembo       CloudProvider = "tembo_cloud"
)

// PrivilegeLevel represents the privilege level of the connection
type PrivilegeLevel string

const (
	PrivilegeLevelSuperuser PrivilegeLevel = "superuser"
	PrivilegeLevelLimited   PrivilegeLevel = "limited"
	PrivilegeLevelReadonly  PrivilegeLevel = "readonly"
)

// VersionInfo contains PostgreSQL version information
type VersionInfo struct {
	VersionNum        uint32
	VersionString     string
	MajorVersion      int
	MinorVersion      int
	IsAurora          bool
	IsAlloyDB         bool
	IsCloudSQL        bool
	IsRDS             bool
	IsAzure           bool
	IsHeroku          bool
	IsCrunchy         bool
	IsTembo           bool
	RelHasOids        bool
	ExtendedStats     bool
	InheritedStats    bool
	PgStatIO          bool
	DirectPgStats     bool
	PartitionedTables bool
	GeneratedColumns  bool
}

// ColumnDefinition represents a table column
type ColumnDefinition struct {
	OID          uint32
	Name         string
	TypeName     string
	TypeOID      uint32
	TypeModifier int32
	NotNull      bool
	HasDefault   bool
	DefaultValue string
	Description  string
	Collation    uint32
	Xmin         uint32
	Position     int16

	// Column statistics from pg_stats
	NullFraction float64
	AvgWidth     int32
	NDistinct    float64
	Correlation  float64
}

// IndexDefinition represents a database index
type IndexDefinition struct {
	OID         uint32
	Name        string
	TableOID    uint32
	IsPrimary   bool
	IsUnique    bool
	IsValid     bool
	IsExclusion bool
	IndexType   string // btree, hash, gist, gin, spgist, brin
	Definition  string
	Partial     string
	Xmin        uint32
	Columns     []string
	SizeBytes   int64

	// Index statistics from pg_stat_user_indexes
	ScanCount  int64
	TupleRead  int64
	TupleFetch int64
	LastScan   *time.Time
}

// ConstraintDefinition represents a database constraint
type ConstraintDefinition struct {
	OID        uint32
	Name       string
	Type       string // p=primary key, u=unique, c=check, f=foreign key, t=trigger, x=exclusion
	TableOID   uint32
	Definition string
	Deferrable bool
	Deferred   bool
	Validated  bool

	// FK-specific fields (populated when Type == "f")
	ReferencedTableOID uint32
	ColumnNames        []string
	ReferencedColumns  []string
}

// TableDefinition represents a database table
type TableDefinition struct {
	OID             uint32
	SchemaName      string
	Name            string
	Type            string // r=table, t=toast, v=view, m=materialized view, p=partitioned
	Owner           string
	Description     string
	Tablespace      uint32
	Xmin            uint32
	HasOids         bool
	Columns         []*ColumnDefinition
	Indexes         []*IndexDefinition
	Constraints     []*ConstraintDefinition
	LiveTuples      int64
	DeadTuples      int64
	ModSinceAnalyze int64
	LastVacuum      *time.Time
	LastAutovacuum  *time.Time
	LastAnalyze     *time.Time
	LastAutoanalyze *time.Time
	SeqScans        int64
	SeqTupRead      int64
	IndexScans      int64
	IndexTupFetch   int64
	SizeBytes       int64
	TotalSizeBytes  int64

	// View-specific
	ViewDefinition string

	// Partition-specific
	IsPartitioned bool
	ParentOID     uint32
	PartitionExpr string
}

// ExtensionDefinition represents a PostgreSQL extension
type ExtensionDefinition struct {
	OID         uint32
	Name        string
	Version     string
	Relocatable bool
	SchemaName  string
	Description string
	Objects     []string
}

// Setting represents a PostgreSQL configuration setting
type Setting struct {
	Name    string
	Value   string
	Unit    string
	Context string
	Type    string
	Source  string
}

// CollectionStatistics provides collection performance metrics
type CollectionStatistics struct {
	CollectionDurationMs int64
	TableCount           int32
	ColumnCount          int32
	IndexCount           int32
	ConstraintCount      int32
	ExtensionCount       int32
	HasErrors            bool
	ErrorCount           int32
	CompletionRatio      float64
	TotalSizeBytes       int64
	TotalRowCount        int64 // Sum of table.live_tuples across all tables (estimate from pg_stat or reltuples)
}

// SchemaCollectionEvent represents a complete schema collection snapshot
type SchemaCollectionEvent struct {
	DatabaseName         string
	DatabaseOID          uint32
	PostgresVersion      uint32
	VersionString        string
	CloudProvider        string
	CollectionType       CollectionType
	CollectedAt          time.Time
	CollectedAtTimestamp int64
	SchemaVersion        uint64
	Tables               []*TableDefinition
	Extensions           []*ExtensionDefinition
	Settings             map[string]*Setting
	Statistics           *CollectionStatistics
	ErrorMessages        []string
}

// ChangeTracker tracks table changes using xmin
type ChangeTracker struct {
	LastXminByOID map[uint32]uint32
	LastSnapshot  time.Time
	SchemaVersion uint64
}

// ErrorInfo provides detailed error information
type ErrorInfo struct {
	Category      string
	Severity      string
	Code          string
	Message       string
	TableOID      uint32
	TableName     string
	Recoverable   bool
	RecoverySteps []string
	Timestamp     time.Time
}

// SettingsSnapshot represents collected PostgreSQL settings
type SettingsSnapshot struct {
	CollectedAt time.Time
	Settings    map[string]*Setting
	Errors      []ErrorInfo
}

// ExtensionsSnapshot represents collected extensions
type ExtensionsSnapshot struct {
	CollectedAt time.Time
	Extensions  []*ExtensionDefinition
	Errors      []ErrorInfo
}

// CloudMetadata contains cloud-specific information
type CloudMetadata struct {
	Provider           CloudProvider
	Region             string
	InstanceType       string
	EngineVersion      string
	PrivilegeLevel     PrivilegeLevel
	SupportedFeatures  map[string]bool
	HasHelperFunctions bool
	MaxConnections     int
	StorageType        string
	BackupEnabled      bool
	ReplicationEnabled bool
	EncryptionEnabled  bool
}

// CollectorConfig represents the schema collector configuration
type CollectorConfig struct {
	DatabaseName         string
	DatabaseOID          uint32
	ContinueOnError      bool
	CollectExtensions    bool
	CollectSettings      bool
	CollectMetadata      bool
	CollectColumnStats   bool
	MaxTableConcurrency  int
	QueryTimeoutMs       int64
	ExcludeSchemas       map[string]bool
	IncludeSchemas       map[string]bool
	ExcludeSystemSchemas bool
}

// FilterConfig represents table/schema filtering
type FilterConfig struct {
	ExcludeSchemas       map[string]bool
	IncludeSchemas       map[string]bool
	ExcludeTables        map[string][]string
	IncludeTables        map[string][]string
	MaxTables            int
	MaxColumns           int
	ExcludeSystemObjects bool
}
