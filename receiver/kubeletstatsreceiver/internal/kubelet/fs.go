// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package kubelet // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/kubeletstatsreceiver/internal/kubelet"

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
	stats "k8s.io/kubelet/pkg/apis/stats/v1alpha1"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/kubeletstatsreceiver/internal/metadata"
)

func calculateUtilization(usedBytes, capacityBytes *uint64) float64 {
	return float64(*usedBytes) / float64(*capacityBytes) * 100
}

func addFilesystemMetrics(mb *metadata.MetricsBuilder, filesystemMetrics metadata.FilesystemMetrics, s *stats.FsStats, currentTime pcommon.Timestamp) {
	if s == nil {
		return
	}
	utilization := calculateUtilization(s.UsedBytes, s.CapacityBytes)
	recordIntDataPoint(mb, filesystemMetrics.Available, s.AvailableBytes, currentTime)
	recordIntDataPoint(mb, filesystemMetrics.Capacity, s.CapacityBytes, currentTime)
	recordIntDataPoint(mb, filesystemMetrics.Usage, s.UsedBytes, currentTime)
	recordDoubleDataPoint(mb, filesystemMetrics.Utilization, &utilization, currentTime)
	recordIntDataPoint(mb, filesystemMetrics.Inodes, s.Inodes, currentTime)
	recordIntDataPoint(mb, filesystemMetrics.InodesFree, s.InodesFree, currentTime)
}

func addEphemeralStorageMetrics(mb *metadata.MetricsBuilder, esMetrics metadata.EphemeralStorageMetrics, s *stats.FsStats, fsType metadata.AttributeFsType, currentTime pcommon.Timestamp) {
	if s == nil {
		return
	}

	recordIntDataPointWithFsType(mb, esMetrics.Usage, s.UsedBytes, fsType, currentTime)
}

func recordIntDataPointWithFsType(mb *metadata.MetricsBuilder, recordDataPoint metadata.RecordIntDataPointWithFsTypeFunc, value *uint64, fsType metadata.AttributeFsType, currentTime pcommon.Timestamp) {
	if value == nil || recordDataPoint == nil {
		return
	}
	recordDataPoint(mb, currentTime, int64(*value), fsType)
}
