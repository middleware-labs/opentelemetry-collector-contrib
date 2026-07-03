// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

// stripEmptyLogAttrs removes attributes that represent absent PostgreSQL values
// from every log record in logs. NULL-able pg columns are COALESCE'd to empty
// strings before insertion; this strips those sentinels so the emitted event
// only contains meaningful data.
//
// Rules:
//   - string attributes with value ""  are removed
//   - slice attributes with 0 elements are removed
//   - int/double/bool attributes are never removed (zero is a valid value)
func stripEmptyLogAttrs(logs plog.Logs) {
	for i := 0; i < logs.ResourceLogs().Len(); i++ {
		rl := logs.ResourceLogs().At(i)
		for j := 0; j < rl.ScopeLogs().Len(); j++ {
			sl := rl.ScopeLogs().At(j)
			for k := 0; k < sl.LogRecords().Len(); k++ {
				removeZeroAttrs(sl.LogRecords().At(k).Attributes())
			}
		}
	}
}

// removeZeroAttrs removes zero/empty-valued attributes from a single attribute map.
func removeZeroAttrs(attrs pcommon.Map) {
	var toRemove []string
	attrs.Range(func(k string, v pcommon.Value) bool {
		switch v.Type() {
		case pcommon.ValueTypeStr:
			if v.Str() == "" {
				toRemove = append(toRemove, k)
			}
		case pcommon.ValueTypeSlice:
			if v.Slice().Len() == 0 {
				toRemove = append(toRemove, k)
			}
		}
		return true
	})
	for _, k := range toRemove {
		attrs.Remove(k)
	}
}
