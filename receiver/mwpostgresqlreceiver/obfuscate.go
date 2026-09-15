// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// source(Apache 2.0): https://github.com/DataDog/datadog-agent/blob/main/pkg/collector/python/datadog_agent.go

// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"sync"

	"github.com/DataDog/datadog-agent/pkg/obfuscate"
)

var (
	obfuscator       *obfuscate.Obfuscator
	obfuscatorLoader sync.Once
)

// defaultSQLPlanNormalizeSettings are the default JSON obfuscator settings for both obfuscating and normalizing SQL
// execution plans
var defaultSQLPlanNormalizeSettings = obfuscate.JSONConfig{
	Enabled: true,
	ObfuscateSQLValues: []string{
		// mysql
		"attached_condition",
		// postgres
		"Cache Key",
		"Conflict Filter",
		"Function Call",
		"Filter",
		"Hash Cond",
		"Index Cond",
		"Join Filter",
		"Merge Cond",
		"Output",
		"Recheck Cond",
		"Repeatable Seed",
		"Sampling Parameters",
		"TID Cond",
	},
	KeepValues: []string{
		// postgres (added)
		// the mysql related section has been removed. if we need to keep any value from normalized,
		// we should add the json property to here.
		"Plan Rows",
		"Plan Width",
		"Startup Cost",
		"Total Cost",
		// postgres (original values from the datadog library)
		"Actual Loops",
		"Actual Rows",
		"Actual Startup Time",
		"Actual Total Time",
		"Alias",
		"Async Capable",
		"Average Sort Space Used",
		"Cache Evictions",
		"Cache Hits",
		"Cache Misses",
		"Cache Overflows",
		"Calls",
		"Command",
		"Conflict Arbiter Indexes",
		"Conflict Resolution",
		"Conflicting Tuples",
		"Constraint Name",
		"CTE Name",
		"Custom Plan Provider",
		"Deforming",
		"Emission",
		"Exact Heap Blocks",
		"Execution Time",
		"Expressions",
		"Foreign Delete",
		"Foreign Insert",
		"Foreign Update",
		"Full-sort Group",
		"Function Name",
		"Generation",
		"Group Count",
		"Grouping Sets",
		"Group Key",
		"HashAgg Batches",
		"Hash Batches",
		"Hash Buckets",
		"Heap Fetches",
		"I/O Read Time",
		"I/O Write Time",
		"Index Name",
		"Inlining",
		"Join Type",
		"Local Dirtied Blocks",
		"Local Hit Blocks",
		"Local Read Blocks",
		"Local Written Blocks",
		"Lossy Heap Blocks",
		"Node Type",
		"Optimization",
		"Original Hash Batches",
		"Original Hash Buckets",
		"Parallel Aware",
		"Parent Relationship",
		"Partial Mode",
		"Peak Memory Usage",
		"Peak Sort Space Used",
		"Planned Partitions",
		"Planning Time",
		"Pre-sorted Groups",
		"Presorted Key",
		"Query Identifier",
		"Relation Name",
		"Rows Removed by Conflict Filter",
		"Rows Removed by Filter",
		"Rows Removed by Index Recheck",
		"Rows Removed by Join Filter",
		"Sampling Method",
		"Scan Direction",
		"Schema",
		"Settings",
		"Shared Dirtied Blocks",
		"Shared Hit Blocks",
		"Shared Read Blocks",
		"Shared Written Blocks",
		"Single Copy",
		"Sort Key",
		"Sort Method",
		"Sort Methods Used",
		"Sort Space Type",
		"Sort Space Used",
		"Strategy",
		"Subplan Name",
		"Subplans Removed",
		"Target Tables",
		"Temp Read Blocks",
		"Temp Written Blocks",
		"Time",
		"Timing",
		"Total",
		"Trigger",
		"Trigger Name",
		"Triggers",
		"Tuples Inserted",
		"Tuplestore Name",
		"WAL Bytes",
		"WAL FPI",
		"WAL Records",
		"Worker",
		"Worker Number",
		"Workers",
		"Workers Launched",
		"Workers Planned",
	},
}

// defaultSQLPlanObfuscateSettings builds upon sqlPlanNormalizeSettings by including cost & row estimates in the keep
// list
var defaultSQLPlanObfuscateSettings = obfuscate.JSONConfig{
	Enabled: true,
	KeepValues: append([]string{
		// mysql section was removed
		// postgres (original from the datadog library)
		"Plan Rows",
		"Plan Width",
		"Startup Cost",
		"Total Cost",
	}, defaultSQLPlanNormalizeSettings.KeepValues...),
	ObfuscateSQLValues: defaultSQLPlanNormalizeSettings.ObfuscateSQLValues,
}

// obfuscatorCacheMaxBytes bounds the obfuscator's query cache.
//
// The cache is keyed on the raw statement text, which is exactly what repeats:
// pg_stat_statements returns a largely unchanged set of statements every
// collection interval, so without a cache the same text is re-obfuscated every
// ten seconds for the life of the process.
//
// 4 MB is comparable to the existing query-text cache (5000 entries at
// PostgreSQL's default pg_stat_statements.max) and is a bound on payload bytes
// rather than entries, so a server with a few very long statements cannot grow
// it without limit. The vendored cache is ristretto, which derives its internal
// counter budget from this size.
const obfuscatorCacheMaxBytes = 4 << 20

// lazyInitObfuscator initializes the obfuscator the first time it is used.
//
// The obfuscator is a process-wide singleton, so the cache's background
// goroutines live for the life of the process. That is acceptable for a bounded
// cache that every scrape uses, but it is why the size is fixed here rather
// than derived from per-receiver configuration.
func lazyInitObfuscator() *obfuscate.Obfuscator {
	obfuscatorLoader.Do(func() {
		obfuscator = obfuscate.NewObfuscator(obfuscate.Config{
			SQL: obfuscate.SQLConfig{
				DBMS:         "postgresql",
				KeepSQLAlias: true,
				KeepBoolean:  true,
				KeepNull:     true,
			},
			SQLExecPlan:          defaultSQLPlanObfuscateSettings,
			SQLExecPlanNormalize: defaultSQLPlanNormalizeSettings,
			Cache: obfuscate.CacheConfig{
				Enabled: true,
				MaxSize: obfuscatorCacheMaxBytes,
			},
		})
	})
	return obfuscator
}

// obfuscateSQL obfuscates & normalizes the provided SQL query, writing the error into errResult if the operation fails.
func obfuscateSQL(rawQuery string) (string, error) {
	obfuscatedQuery, err := lazyInitObfuscator().ObfuscateSQLString(rawQuery)
	if err != nil {
		return "", err
	}

	return obfuscatedQuery.Query, nil
}

// obfuscateSQLExecPlan obfuscates the provided json query execution plan, writing the error into errResult if the
// operation fails
func obfuscateSQLExecPlan(rawPlan string) (string, error) {
	return lazyInitObfuscator().ObfuscateSQLExecPlan(
		rawPlan,
		true,
	)
}

// Ending source(Apache 2.0): https://github.com/DataDog/datadog-agent/blob/main/pkg/collector/python/datadog_agent.go
