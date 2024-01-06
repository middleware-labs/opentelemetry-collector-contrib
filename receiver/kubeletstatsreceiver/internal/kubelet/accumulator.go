// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package kubelet // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/kubeletstatsreceiver/internal/kubelet"

import (
	"log"
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/k8sconfig"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	stats "k8s.io/kubelet/pkg/apis/stats/v1alpha1"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/kubeletstatsreceiver/internal/metadata"
)

type MetricGroup string

// Values for MetricGroup enum.
const (
	ContainerMetricGroup = MetricGroup("container")
	PodMetricGroup       = MetricGroup("pod")
	NodeMetricGroup      = MetricGroup("node")
	VolumeMetricGroup    = MetricGroup("volume")
)

// ValidMetricGroups map of valid metrics.
var ValidMetricGroups = map[MetricGroup]bool{
	ContainerMetricGroup: true,
	PodMetricGroup:       true,
	NodeMetricGroup:      true,
	VolumeMetricGroup:    true,
}

type metricDataAccumulator struct {
	m                     []pmetric.Metrics
	metadata              Metadata
	logger                *zap.Logger
	metricGroupsToCollect map[MetricGroup]bool
	time                  time.Time
	mbs                   *metadata.MetricsBuilders
}

func addUptimeMetric(mb *metadata.MetricsBuilder, uptimeMetric metadata.RecordIntDataPointFunc, startTime v1.Time, currentTime pcommon.Timestamp) {
	if !startTime.IsZero() {
		value := int64(time.Since(startTime.Time).Seconds())
		uptimeMetric(mb, currentTime, value)
	}
}

func (a *metricDataAccumulator) nodeStats(s stats.NodeStats) {
	if !a.metricGroupsToCollect[NodeMetricGroup] {
		return
	}

	currentTime := pcommon.NewTimestampFromTime(a.time)
	addUptimeMetric(a.mbs.NodeMetricsBuilder, metadata.NodeUptimeMetrics.Uptime, s.StartTime, currentTime)
	addCPUMetrics(a.mbs.NodeMetricsBuilder, metadata.NodeCPUMetrics, s.CPU, currentTime, resources{})
	addMemoryMetrics(a.mbs.NodeMetricsBuilder, metadata.NodeMemoryMetrics, s.Memory, currentTime, resources{})
	addFilesystemMetrics(a.mbs.NodeMetricsBuilder, metadata.NodeFilesystemMetrics, s.Fs, currentTime)
	addNetworkMetrics(a.mbs.NodeMetricsBuilder, metadata.NodeNetworkMetrics, s.Network, currentTime)
	// todo s.Runtime.ImageFs
	rb := a.mbs.NodeMetricsBuilder.NewResourceBuilder()
	rb.SetK8sNodeName(s.NodeName)
	rb.SetK8sNodeUID(a.getNodeUID(s.NodeName))
	rb.SetK8sNodeStartTime(s.StartTime.Time.String())
	a.m = append(a.m, a.mbs.NodeMetricsBuilder.Emit(
		metadata.WithStartTimeOverride(pcommon.NewTimestampFromTime(s.StartTime.Time)),
		metadata.WithResource(rb.Emit()),
	))
}

// getch k8s node uid from metadata
func (a *metricDataAccumulator) getNodeUID(nodeName string) string {
	uid, err := a.metadata.getNodeUID(nodeName)
	if err != nil {
		log.Println(err.Error())
		return ""
	}
	return uid
}

func (a *metricDataAccumulator) podStats(s stats.PodStats) {
	if !a.metricGroupsToCollect[PodMetricGroup] {
		return
	}

	currentTime := pcommon.NewTimestampFromTime(a.time)
	addUptimeMetric(a.mbs.PodMetricsBuilder, metadata.PodUptimeMetrics.Uptime, s.StartTime, currentTime)
	addCPUMetrics(a.mbs.PodMetricsBuilder, metadata.PodCPUMetrics, s.CPU, currentTime, a.metadata.podResources[s.PodRef.UID])
	addMemoryMetrics(a.mbs.PodMetricsBuilder, metadata.PodMemoryMetrics, s.Memory, currentTime, a.metadata.podResources[s.PodRef.UID])
	addFilesystemMetrics(a.mbs.PodMetricsBuilder, metadata.PodFilesystemMetrics, s.EphemeralStorage, currentTime)
	addNetworkMetrics(a.mbs.PodMetricsBuilder, metadata.PodNetworkMetrics, s.Network, currentTime)

	serviceName := a.getServiceName(s.PodRef.UID)
	serviceAccountName := a.getServiceAccountName(s.PodRef.UID)

	rb := a.mbs.PodMetricsBuilder.NewResourceBuilder()
	rb.SetK8sPodUID(s.PodRef.UID)
	rb.SetK8sPodName(s.PodRef.Name)
	rb.SetK8sNamespaceName(s.PodRef.Namespace)
	rb.SetK8sServiceName(serviceName)
	rb.SetK8sServiceAccountName(serviceAccountName)
	rb.SetK8sClusterName("unknown")
	a.m = append(a.m, a.mbs.PodMetricsBuilder.Emit(
		metadata.WithStartTimeOverride(pcommon.NewTimestampFromTime(s.StartTime.Time)),
		metadata.WithResource(rb.Emit()),
	))
}

// getch k8s service name from metadata
func (a *metricDataAccumulator) getServiceName(podUID string) string {
	k8sAPIClient, err := k8sconfig.MakeClient(k8sconfig.APIConfig{
		AuthType: k8sconfig.AuthTypeServiceAccount,
	})
	if err != nil {
		return ""
	}

	name, err := a.metadata.getServiceName(podUID, k8sAPIClient)
	if err != nil {
		log.Println(err.Error())
		return ""
	}
	return name
}

// getch k8s service account name from metadata
func (a *metricDataAccumulator) getServiceAccountName(podUID string) string {
	name, err := a.metadata.getServiceAccountName(podUID)
	if err != nil {
		log.Println(err.Error())
		return ""
	}
	return name
}

func (a *metricDataAccumulator) containerStats(sPod stats.PodStats, s stats.ContainerStats) {
	if !a.metricGroupsToCollect[ContainerMetricGroup] {
		return
	}

	rb := a.mbs.ContainerMetricsBuilder.NewResourceBuilder()
	res, err := getContainerResource(rb, sPod, s, a.metadata)
	if err != nil {
		a.logger.Warn(
			"failed to fetch container metrics",
			zap.String("pod", sPod.PodRef.Name),
			zap.String("container", s.Name),
			zap.Error(err))
		return
	}

	resourceKey := sPod.PodRef.UID + s.Name
	currentTime := pcommon.NewTimestampFromTime(a.time)
	addUptimeMetric(a.mbs.ContainerMetricsBuilder, metadata.ContainerUptimeMetrics.Uptime, s.StartTime, currentTime)
	addCPUMetrics(a.mbs.ContainerMetricsBuilder, metadata.ContainerCPUMetrics, s.CPU, currentTime, a.metadata.containerResources[resourceKey])
	addMemoryMetrics(a.mbs.ContainerMetricsBuilder, metadata.ContainerMemoryMetrics, s.Memory, currentTime, a.metadata.containerResources[resourceKey])
	addFilesystemMetrics(a.mbs.ContainerMetricsBuilder, metadata.ContainerFilesystemMetrics, s.Rootfs, currentTime)

	a.m = append(a.m, a.mbs.ContainerMetricsBuilder.Emit(
		metadata.WithStartTimeOverride(pcommon.NewTimestampFromTime(s.StartTime.Time)),
		metadata.WithResource(res),
	))
}

func (a *metricDataAccumulator) volumeStats(sPod stats.PodStats, s stats.VolumeStats) {
	if !a.metricGroupsToCollect[VolumeMetricGroup] {
		return
	}

	rb := a.mbs.OtherMetricsBuilder.NewResourceBuilder()
	res, err := getVolumeResourceOptions(rb, sPod, s, a.metadata)
	if err != nil {
		a.logger.Warn(
			"Failed to gather additional volume metadata. Skipping metric collection.",
			zap.String("pod", sPod.PodRef.Name),
			zap.String("volume", s.Name),
			zap.Error(err))
		return
	}

	currentTime := pcommon.NewTimestampFromTime(a.time)
	addVolumeMetrics(a.mbs.OtherMetricsBuilder, metadata.K8sVolumeMetrics, s, currentTime)

	a.m = append(a.m, a.mbs.OtherMetricsBuilder.Emit(metadata.WithResource(res)))
}
