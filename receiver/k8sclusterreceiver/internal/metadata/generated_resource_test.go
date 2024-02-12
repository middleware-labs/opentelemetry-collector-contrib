// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResourceBuilder(t *testing.T) {
	for _, test := range []string{"default", "all_set", "none_set"} {
		t.Run(test, func(t *testing.T) {
			cfg := loadResourceAttributesConfig(t, test)
			rb := NewResourceBuilder(cfg)
			rb.SetContainerID("container.id-val")
			rb.SetContainerImageName("container.image.name-val")
			rb.SetContainerImageTag("container.image.tag-val")
			rb.SetK8sClusterName("k8s.cluster.name-val")
			rb.SetK8sContainerName("k8s.container.name-val")
			rb.SetK8sCronjobName("k8s.cronjob.name-val")
			rb.SetK8sCronjobStartTime("k8s.cronjob.start_time-val")
			rb.SetK8sCronjobUID("k8s.cronjob.uid-val")
			rb.SetK8sDaemonsetName("k8s.daemonset.name-val")
			rb.SetK8sDaemonsetStartTime("k8s.daemonset.start_time-val")
			rb.SetK8sDaemonsetUID("k8s.daemonset.uid-val")
			rb.SetK8sDeploymentName("k8s.deployment.name-val")
			rb.SetK8sDeploymentStartTime("k8s.deployment.start_time-val")
			rb.SetK8sDeploymentUID("k8s.deployment.uid-val")
			rb.SetK8sHpaName("k8s.hpa.name-val")
			rb.SetK8sHpaUID("k8s.hpa.uid-val")
			rb.SetK8sJobName("k8s.job.name-val")
			rb.SetK8sJobStartTime("k8s.job.start_time-val")
			rb.SetK8sJobUID("k8s.job.uid-val")
			rb.SetK8sNamespaceName("k8s.namespace.name-val")
			rb.SetK8sNamespaceStartTime("k8s.namespace.start_time-val")
			rb.SetK8sNamespaceUID("k8s.namespace.uid-val")
			rb.SetK8sNodeName("k8s.node.name-val")
			rb.SetK8sNodeStartTime("k8s.node.start_time-val")
			rb.SetK8sNodeUID("k8s.node.uid-val")
			rb.SetK8sPodName("k8s.pod.name-val")
			rb.SetK8sPodStartTime("k8s.pod.start_time-val")
			rb.SetK8sPodUID("k8s.pod.uid-val")
			rb.SetK8sReplicasetName("k8s.replicaset.name-val")
			rb.SetK8sReplicasetStartTime("k8s.replicaset.start_time-val")
			rb.SetK8sReplicasetUID("k8s.replicaset.uid-val")
			rb.SetK8sReplicationcontrollerName("k8s.replicationcontroller.name-val")
			rb.SetK8sReplicationcontrollerUID("k8s.replicationcontroller.uid-val")
			rb.SetK8sResourcequotaName("k8s.resourcequota.name-val")
			rb.SetK8sResourcequotaUID("k8s.resourcequota.uid-val")
			rb.SetK8sServiceClusterIP("k8s.service.cluster_ip-val")
			rb.SetK8sServiceName("k8s.service.name-val")
			rb.SetK8sServiceNamespace("k8s.service.namespace-val")
			rb.SetK8sServiceType("k8s.service.type-val")
			rb.SetK8sServiceUID("k8s.service.uid-val")
			rb.SetK8sServiceAccountName("k8s.service_account.name-val")
			rb.SetK8sStatefulsetName("k8s.statefulset.name-val")
			rb.SetK8sStatefulsetStartTime("k8s.statefulset.start_time-val")
			rb.SetK8sStatefulsetUID("k8s.statefulset.uid-val")
			rb.SetOpencensusResourcetype("opencensus.resourcetype-val")
			rb.SetOpenshiftClusterquotaName("openshift.clusterquota.name-val")
			rb.SetOpenshiftClusterquotaUID("openshift.clusterquota.uid-val")

			res := rb.Emit()
			assert.Equal(t, 0, rb.Emit().Attributes().Len()) // Second call should return empty Resource

			switch test {
			case "default":
				assert.Equal(t, 47, res.Attributes().Len())
			case "all_set":
				assert.Equal(t, 47, res.Attributes().Len())
			case "none_set":
				assert.Equal(t, 0, res.Attributes().Len())
				return
			default:
				assert.Failf(t, "unexpected test case: %s", test)
			}

			val, ok := res.Attributes().Get("container.id")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "container.id-val", val.Str())
			}
			val, ok = res.Attributes().Get("container.image.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "container.image.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("container.image.tag")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "container.image.tag-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.cluster.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.cluster.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.container.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.container.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.cronjob.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.cronjob.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.cronjob.start_time")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.cronjob.start_time-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.cronjob.uid")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.cronjob.uid-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.daemonset.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.daemonset.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.daemonset.start_time")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.daemonset.start_time-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.daemonset.uid")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.daemonset.uid-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.deployment.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.deployment.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.deployment.start_time")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.deployment.start_time-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.deployment.uid")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.deployment.uid-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.hpa.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.hpa.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.hpa.uid")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.hpa.uid-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.job.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.job.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.job.start_time")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.job.start_time-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.job.uid")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.job.uid-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.namespace.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.namespace.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.namespace.start_time")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.namespace.start_time-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.namespace.uid")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.namespace.uid-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.node.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.node.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.node.start_time")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.node.start_time-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.node.uid")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.node.uid-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.pod.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.pod.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.pod.start_time")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.pod.start_time-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.pod.uid")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.pod.uid-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.replicaset.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.replicaset.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.replicaset.start_time")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.replicaset.start_time-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.replicaset.uid")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.replicaset.uid-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.replicationcontroller.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.replicationcontroller.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.replicationcontroller.uid")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.replicationcontroller.uid-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.resourcequota.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.resourcequota.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.resourcequota.uid")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.resourcequota.uid-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.service.cluster_ip")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.service.cluster_ip-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.service.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.service.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.service.namespace")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.service.namespace-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.service.type")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.service.type-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.service.uid")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.service.uid-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.service_account.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.service_account.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.statefulset.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.statefulset.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.statefulset.start_time")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.statefulset.start_time-val", val.Str())
			}
			val, ok = res.Attributes().Get("k8s.statefulset.uid")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "k8s.statefulset.uid-val", val.Str())
			}
			val, ok = res.Attributes().Get("opencensus.resourcetype")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "opencensus.resourcetype-val", val.Str())
			}
			val, ok = res.Attributes().Get("openshift.clusterquota.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "openshift.clusterquota.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("openshift.clusterquota.uid")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "openshift.clusterquota.uid-val", val.Str())
			}
		})
	}
}
