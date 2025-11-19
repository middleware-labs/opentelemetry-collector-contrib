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
	e := metadata.NewK8sCronjobEntity(string(cj.UID))
	e.SetK8sCronjobName(cj.Name)
	e.SetK8sNamespaceName(cj.Namespace)
	if cj.Spec.ConcurrencyPolicy != "" {
        e.SetK8sCronjobConcurrencyPolicy(string(cj.Spec.ConcurrencyPolicy))
    }
    if cj.Spec.Suspend != nil {
        e.SetK8sCronjobSuspend(strconv.FormatBool(*cj.Spec.Suspend))
    }
    if cj.Spec.Schedule != "" {
        e.SetK8sCronjobSchedule(cj.Spec.Schedule)
    }

    if cj.Status.LastScheduleTime != nil {
        e.SetK8sCronjobLastScheduleTime(cj.Status.LastScheduleTime.String())
    }
    if cj.Status.LastSuccessfulTime != nil {
        e.SetK8sCronjobLastSuccessfulTime(cj.Status.LastSuccessfulTime.String())
	}

	e.SetK8sCronjobStartTime(cj.GetCreationTimestamp().String())
    e.SetK8sClusterName("unknown"
	eb := mb.ForK8sCronjob(e)
	eb.RecordK8sCronjobActiveJobsDataPoint(ts, int64(len(cj.Status.Active)))eb.Emit()
}

func GetMetadata(cj *batchv1.CronJob) map[experimentalmetricmetadata.ResourceID]*metadata.KubernetesMetadata {
	rm := metadata.GetGenericMetadata(&cj.ObjectMeta, constants.K8sKindCronJob)
	rm.Metadata[cronJobKeySchedule] = cj.Spec.Schedule
	rm.Metadata[cronJobKeyConcurrencyPolicy] = string(cj.Spec.ConcurrencyPolicy)
	return map[experimentalmetricmetadata.ResourceID]*metadata.KubernetesMetadata{experimentalmetricmetadata.ResourceID(cj.UID): rm}
}
