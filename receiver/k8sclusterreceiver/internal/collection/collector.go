// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package collection // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/collection"

import (
	"time"

	quotav1 "github.com/openshift/api/quota/v1"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/clusterresourcequota"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/clusterrole"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/clusterrolebinding"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/cronjob"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/demonset"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/deployment"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/gvk"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/hpa"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/ingress"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/jobs"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/metadata"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/namespace"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/node"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/persistentvolume"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/persistentvolumeclaim"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/pod"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/replicaset"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/replicationcontroller"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/resourcequota"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/role"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/rolebinding"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/service"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/serviceaccount"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/statefulset"
)

// TODO: Consider moving some of these constants to
// https://go.opentelemetry.io/collector/blob/main/model/semconv/opentelemetry.go.

// DataCollector emits metrics with CollectMetricData based on the Kubernetes API objects in the metadata store.

const k8sType = "k8s"

type DataCollector struct {
	settings                 receiver.Settings
	metadataStore            *metadata.Store
	nodeConditionsToReport   []string
	allocatableTypesToReport []string
	metricsBuilder           *metadata.MetricsBuilder
}

// NewDataCollector returns a DataCollector.
func NewDataCollector(set receiver.Settings, ms *metadata.Store,
	metricsBuilderConfig metadata.MetricsBuilderConfig, nodeConditionsToReport, allocatableTypesToReport []string,
) *DataCollector {
	return &DataCollector{
		settings:                 set,
		metadataStore:            ms,
		nodeConditionsToReport:   nodeConditionsToReport,
		allocatableTypesToReport: allocatableTypesToReport,
		metricsBuilder:           metadata.NewMetricsBuilder(metricsBuilderConfig, set),
	}
}

func (dc *DataCollector) CollectMetricData(currentTime time.Time) pmetric.Metrics {
	ts := pcommon.NewTimestampFromTime(currentTime)
	customRMs := pmetric.NewResourceMetricsSlice()

	dc.metadataStore.ForEach(gvk.Pod, func(o any) {
		pod.RecordMetrics(dc.settings.Logger, dc.metricsBuilder, o.(*corev1.Pod), ts)
	})
	dc.metadataStore.ForEach(gvk.Node, func(o any) {
		crm := node.CustomMetrics(dc.settings, dc.metricsBuilder.NewResourceBuilder(), o.(*corev1.Node),
			dc.nodeConditionsToReport, dc.allocatableTypesToReport, ts)
		if crm.ScopeMetrics().Len() > 0 {
			crm.MoveTo(customRMs.AppendEmpty())
		}
		node.RecordMetrics(dc.metricsBuilder, o.(*corev1.Node), ts)
	})
	dc.metadataStore.ForEach(gvk.PersistentVolume, func(o any) {
		persistentvolume.RecordMetrics(dc.metricsBuilder, o.(*corev1.PersistentVolume), ts)
	})
	dc.metadataStore.ForEach(gvk.PersistentVolumeClaim, func(o any) {
		persistentvolumeclaim.RecordMetrics(dc.metricsBuilder, o.(*corev1.PersistentVolumeClaim), ts)
	})
	dc.metadataStore.ForEach(gvk.Role, func(o any) {
		role.RecordMetrics(dc.metricsBuilder, o.(*rbacv1.Role), ts)
	})
	dc.metadataStore.ForEach(gvk.RoleBinding, func(o any) {
		rolebinding.RecordMetrics(dc.metricsBuilder, o.(*rbacv1.RoleBinding), ts)
	})
	dc.metadataStore.ForEach(gvk.ClusterRole, func(o any) {
		clusterrole.RecordMetrics(dc.metricsBuilder, o.(*rbacv1.ClusterRole), ts)
	})
	dc.metadataStore.ForEach(gvk.ClusterRoleBinding, func(o any) {
		clusterrolebinding.RecordMetrics(dc.metricsBuilder, o.(*rbacv1.ClusterRoleBinding), ts)
	})
	dc.metadataStore.ForEach(gvk.Ingress, func(o any) {
		ingress.RecordMetrics(dc.metricsBuilder, o.(*netv1.Ingress), ts)
	})
	dc.metadataStore.ForEach(gvk.Namespace, func(o any) {
		namespace.RecordMetrics(dc.metricsBuilder, o.(*corev1.Namespace), ts)
	})
	dc.metadataStore.ForEach(gvk.ReplicationController, func(o any) {
		replicationcontroller.RecordMetrics(dc.metricsBuilder, o.(*corev1.ReplicationController), ts)
	})
	dc.metadataStore.ForEach(gvk.ResourceQuota, func(o any) {
		resourcequota.RecordMetrics(dc.metricsBuilder, o.(*corev1.ResourceQuota), ts)
	})
	dc.metadataStore.ForEach(gvk.Service, func(o any) {
		service.RecordMetrics(dc.metricsBuilder, o.(*corev1.Service), ts)
	})
	dc.metadataStore.ForEach(gvk.ServiceAccount, func(o any) {
		serviceaccount.RecordMetrics(dc.metricsBuilder, o.(*corev1.ServiceAccount), ts)
	})
	dc.metadataStore.ForEach(gvk.Deployment, func(o any) {
		deployment.RecordMetrics(dc.metricsBuilder, o.(*appsv1.Deployment), ts)
	})
	dc.metadataStore.ForEach(gvk.ReplicaSet, func(o any) {
		replicaset.RecordMetrics(dc.metricsBuilder, o.(*appsv1.ReplicaSet), ts)
	})
	dc.metadataStore.ForEach(gvk.DaemonSet, func(o any) {
		demonset.RecordMetrics(dc.metricsBuilder, o.(*appsv1.DaemonSet), ts)
	})
	dc.metadataStore.ForEach(gvk.StatefulSet, func(o any) {
		statefulset.RecordMetrics(dc.metricsBuilder, o.(*appsv1.StatefulSet), ts)
	})

	dc.metadataStore.ForEach(gvk.Job, func(o any) {
		jobs.RecordMetrics(dc.metricsBuilder, o.(*batchv1.Job), ts)
	})
	dc.metadataStore.ForEach(gvk.CronJob, func(o any) {
		cronjob.RecordMetrics(dc.metricsBuilder, o.(*batchv1.CronJob), ts)
	})
	dc.metadataStore.ForEach(gvk.HorizontalPodAutoscaler, func(o any) {
		hpa.RecordMetrics(dc.metricsBuilder, o.(*autoscalingv2.HorizontalPodAutoscaler), ts)
	})
	dc.metadataStore.ForEach(gvk.ClusterResourceQuota, func(o any) {
		clusterresourcequota.RecordMetrics(dc.metricsBuilder, o.(*quotav1.ClusterResourceQuota), ts)
	})

	m := dc.metricsBuilder.Emit()
	customRMs.MoveAndAppendTo(m.ResourceMetrics())
	return m
}
