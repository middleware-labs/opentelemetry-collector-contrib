// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package daemonset // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/daemonset"

import (
	"fmt"
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/experimentalmetricmetadata"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/constants"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/metadata"
)

// Transform transforms the pod to remove the fields that we don't use to reduce RAM utilization.
// IMPORTANT: Make sure to update this function before using new daemonset fields.
func Transform(ds *appsv1.DaemonSet) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metadata.TransformObjectMeta(ds.ObjectMeta),
		Spec: appsv1.DaemonSetSpec{
			Selector:       ds.Spec.Selector,
			UpdateStrategy: ds.Spec.UpdateStrategy,
		},
		Status: appsv1.DaemonSetStatus{
			CurrentNumberScheduled: ds.Status.CurrentNumberScheduled,
			DesiredNumberScheduled: ds.Status.DesiredNumberScheduled,
			NumberMisscheduled:     ds.Status.NumberMisscheduled,
			NumberReady:            ds.Status.NumberReady,
		},
	}
}

func RecordMetrics(mb *metadata.MetricsBuilder, ds *appsv1.DaemonSet, ts pcommon.Timestamp) {
	e := metadata.NewK8sDaemonsetEntity(string(ds.UID))
	e.SetK8sDaemonsetName(ds.Name)
	e.SetK8sNamespaceName(ds.Namespace)
	e.SetK8sDaemonsetStartTime(ds.GetCreationTimestamp().String())
	e.SetK8sClusterName("unknown")
	if ds.Spec.Selector != nil && ds.Spec.Selector.MatchLabels != nil {
		e.SetK8sDaemonsetSelectors(mapToString(ds.Spec.Selector.MatchLabels, "&"))
	}
	// Set update strategy if available
	e.SetK8sDaemonsetStrategy(string(ds.Spec.UpdateStrategy.Type))
	eb := mb.ForK8sDaemonset(e)
	eb.RecordK8sDaemonsetCurrentScheduledNodesDataPoint(ts, int64(ds.Status.CurrentNumberScheduled))
	eb.RecordK8sDaemonsetDesiredScheduledNodesDataPoint(ts, int64(ds.Status.DesiredNumberScheduled))
	eb.RecordK8sDaemonsetMisscheduledNodesDataPoint(ts, int64(ds.Status.NumberMisscheduled))
	eb.RecordK8sDaemonsetReadyNodesDataPoint(ts, int64(ds.Status.NumberReady))
	eb.Emit()
}

func GetMetadata(ds *appsv1.DaemonSet) map[experimentalmetricmetadata.ResourceID]*metadata.KubernetesMetadata {
	return map[experimentalmetricmetadata.ResourceID]*metadata.KubernetesMetadata{
		experimentalmetricmetadata.ResourceID(ds.UID): metadata.GetGenericMetadata(&ds.ObjectMeta, constants.K8sKindDaemonSet),
	}
}

func mapToString(m map[string]string, seperator string) string {
	var res []string
	for k, v := range m {
		res = append(res, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(res, seperator)
}
