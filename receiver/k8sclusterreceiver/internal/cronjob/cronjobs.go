// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package cronjob // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/cronjob"

import (
	"strconv"

	"go.opentelemetry.io/collector/pdata/pcommon"
	batchv1 "k8s.io/api/batch/v1"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/experimentalmetricmetadata"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/constants"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/metadata"
)

const (
	// Keys for cronjob metadata.
	cronJobKeySchedule          = "schedule"
	cronJobKeyConcurrencyPolicy = "concurrency_policy"
)

func RecordMetrics(mb *metadata.MetricsBuilder, cj *batchv1.CronJob, ts pcommon.Timestamp) {
	mb.RecordK8sCronjobActiveJobsDataPoint(ts, int64(len(cj.Status.Active)))

	rb := mb.NewResourceBuilder()
	rb.SetK8sNamespaceName(cj.Namespace)
	rb.SetK8sCronjobUID(string(cj.UID))
	rb.SetK8sCronjobName(cj.Name)
	if cj.Spec.ConcurrencyPolicy != "" {
		rb.SetK8sCronjobConcurrencyPolicy(string(cj.Spec.ConcurrencyPolicy))
	}
	if cj.Spec.Suspend != nil {
		rb.SetK8sCronjobSuspend(strconv.FormatBool(*cj.Spec.Suspend))
	}
	if cj.Spec.Schedule != "" {
		rb.SetK8sCronjobSchedule(cj.Spec.Schedule)
	}

	if cj.Status.LastScheduleTime != nil {
		rb.SetK8sCronjobLastScheduleTime(cj.Status.LastScheduleTime.String())
	}
	if cj.Status.LastSuccessfulTime != nil {
		rb.SetK8sCronjobLastSuccessfulTime(cj.Status.LastSuccessfulTime.String())
	}
	
	rb.SetK8sCronjobStartTime(cj.GetCreationTimestamp().String())
	rb.SetK8sClusterName("unknown")
	mb.EmitForResource(metadata.WithResource(rb.Emit()))

}

func GetMetadata(cj *batchv1.CronJob) map[experimentalmetricmetadata.ResourceID]*metadata.KubernetesMetadata {
	rm := metadata.GetGenericMetadata(&cj.ObjectMeta, constants.K8sKindCronJob)
	rm.Metadata[cronJobKeySchedule] = cj.Spec.Schedule
	rm.Metadata[cronJobKeyConcurrencyPolicy] = string(cj.Spec.ConcurrencyPolicy)
	return map[experimentalmetricmetadata.ResourceID]*metadata.KubernetesMetadata{experimentalmetricmetadata.ResourceID(cj.UID): rm}
}
