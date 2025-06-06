// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
)

// ResourceBuilder is a helper struct to build resources predefined in metadata.yaml.
// The ResourceBuilder is not thread-safe and must not to be used in multiple goroutines.
type ResourceBuilder struct {
	config ResourceAttributesConfig
	res    pcommon.Resource
}

// NewResourceBuilder creates a new ResourceBuilder. This method should be called on the start of the application.
func NewResourceBuilder(rac ResourceAttributesConfig) *ResourceBuilder {
	return &ResourceBuilder{
		config: rac,
		res:    pcommon.NewResource(),
	}
}

// SetContainerID sets provided value as "container.id" attribute.
func (rb *ResourceBuilder) SetContainerID(val string) {
	if rb.config.ContainerID.Enabled {
		rb.res.Attributes().PutStr("container.id", val)
	}
}

// SetContainerImageName sets provided value as "container.image.name" attribute.
func (rb *ResourceBuilder) SetContainerImageName(val string) {
	if rb.config.ContainerImageName.Enabled {
		rb.res.Attributes().PutStr("container.image.name", val)
	}
}

// SetContainerImageTag sets provided value as "container.image.tag" attribute.
func (rb *ResourceBuilder) SetContainerImageTag(val string) {
	if rb.config.ContainerImageTag.Enabled {
		rb.res.Attributes().PutStr("container.image.tag", val)
	}
}

// SetContainerRuntime sets provided value as "container.runtime" attribute.
func (rb *ResourceBuilder) SetContainerRuntime(val string) {
	if rb.config.ContainerRuntime.Enabled {
		rb.res.Attributes().PutStr("container.runtime", val)
	}
}

// SetContainerRuntimeVersion sets provided value as "container.runtime.version" attribute.
func (rb *ResourceBuilder) SetContainerRuntimeVersion(val string) {
	if rb.config.ContainerRuntimeVersion.Enabled {
		rb.res.Attributes().PutStr("container.runtime.version", val)
	}
}

// SetK8sClusterName sets provided value as "k8s.cluster.name" attribute.
func (rb *ResourceBuilder) SetK8sClusterName(val string) {
	if rb.config.K8sClusterName.Enabled {
		rb.res.Attributes().PutStr("k8s.cluster.name", val)
	}
}

// SetK8sClusterroleAnnotations sets provided value as "k8s.clusterrole.annotations" attribute.
func (rb *ResourceBuilder) SetK8sClusterroleAnnotations(val string) {
	if rb.config.K8sClusterroleAnnotations.Enabled {
		rb.res.Attributes().PutStr("k8s.clusterrole.annotations", val)
	}
}

// SetK8sClusterroleLabels sets provided value as "k8s.clusterrole.labels" attribute.
func (rb *ResourceBuilder) SetK8sClusterroleLabels(val string) {
	if rb.config.K8sClusterroleLabels.Enabled {
		rb.res.Attributes().PutStr("k8s.clusterrole.labels", val)
	}
}

// SetK8sClusterroleName sets provided value as "k8s.clusterrole.name" attribute.
func (rb *ResourceBuilder) SetK8sClusterroleName(val string) {
	if rb.config.K8sClusterroleName.Enabled {
		rb.res.Attributes().PutStr("k8s.clusterrole.name", val)
	}
}

// SetK8sClusterroleRules sets provided value as "k8s.clusterrole.rules" attribute.
func (rb *ResourceBuilder) SetK8sClusterroleRules(val string) {
	if rb.config.K8sClusterroleRules.Enabled {
		rb.res.Attributes().PutStr("k8s.clusterrole.rules", val)
	}
}

// SetK8sClusterroleStartTime sets provided value as "k8s.clusterrole.start_time" attribute.
func (rb *ResourceBuilder) SetK8sClusterroleStartTime(val string) {
	if rb.config.K8sClusterroleStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.clusterrole.start_time", val)
	}
}

// SetK8sClusterroleType sets provided value as "k8s.clusterrole.type" attribute.
func (rb *ResourceBuilder) SetK8sClusterroleType(val string) {
	if rb.config.K8sClusterroleType.Enabled {
		rb.res.Attributes().PutStr("k8s.clusterrole.type", val)
	}
}

// SetK8sClusterroleUID sets provided value as "k8s.clusterrole.uid" attribute.
func (rb *ResourceBuilder) SetK8sClusterroleUID(val string) {
	if rb.config.K8sClusterroleUID.Enabled {
		rb.res.Attributes().PutStr("k8s.clusterrole.uid", val)
	}
}

// SetK8sClusterrolebindingAnnotations sets provided value as "k8s.clusterrolebinding.annotations" attribute.
func (rb *ResourceBuilder) SetK8sClusterrolebindingAnnotations(val string) {
	if rb.config.K8sClusterrolebindingAnnotations.Enabled {
		rb.res.Attributes().PutStr("k8s.clusterrolebinding.annotations", val)
	}
}

// SetK8sClusterrolebindingLabels sets provided value as "k8s.clusterrolebinding.labels" attribute.
func (rb *ResourceBuilder) SetK8sClusterrolebindingLabels(val string) {
	if rb.config.K8sClusterrolebindingLabels.Enabled {
		rb.res.Attributes().PutStr("k8s.clusterrolebinding.labels", val)
	}
}

// SetK8sClusterrolebindingName sets provided value as "k8s.clusterrolebinding.name" attribute.
func (rb *ResourceBuilder) SetK8sClusterrolebindingName(val string) {
	if rb.config.K8sClusterrolebindingName.Enabled {
		rb.res.Attributes().PutStr("k8s.clusterrolebinding.name", val)
	}
}

// SetK8sClusterrolebindingRoleRef sets provided value as "k8s.clusterrolebinding.role_ref" attribute.
func (rb *ResourceBuilder) SetK8sClusterrolebindingRoleRef(val string) {
	if rb.config.K8sClusterrolebindingRoleRef.Enabled {
		rb.res.Attributes().PutStr("k8s.clusterrolebinding.role_ref", val)
	}
}

// SetK8sClusterrolebindingStartTime sets provided value as "k8s.clusterrolebinding.start_time" attribute.
func (rb *ResourceBuilder) SetK8sClusterrolebindingStartTime(val string) {
	if rb.config.K8sClusterrolebindingStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.clusterrolebinding.start_time", val)
	}
}

// SetK8sClusterrolebindingSubjects sets provided value as "k8s.clusterrolebinding.subjects" attribute.
func (rb *ResourceBuilder) SetK8sClusterrolebindingSubjects(val string) {
	if rb.config.K8sClusterrolebindingSubjects.Enabled {
		rb.res.Attributes().PutStr("k8s.clusterrolebinding.subjects", val)
	}
}

// SetK8sClusterrolebindingType sets provided value as "k8s.clusterrolebinding.type" attribute.
func (rb *ResourceBuilder) SetK8sClusterrolebindingType(val string) {
	if rb.config.K8sClusterrolebindingType.Enabled {
		rb.res.Attributes().PutStr("k8s.clusterrolebinding.type", val)
	}
}

// SetK8sClusterrolebindingUID sets provided value as "k8s.clusterrolebinding.uid" attribute.
func (rb *ResourceBuilder) SetK8sClusterrolebindingUID(val string) {
	if rb.config.K8sClusterrolebindingUID.Enabled {
		rb.res.Attributes().PutStr("k8s.clusterrolebinding.uid", val)
	}
}

// SetK8sContainerName sets provided value as "k8s.container.name" attribute.
func (rb *ResourceBuilder) SetK8sContainerName(val string) {
	if rb.config.K8sContainerName.Enabled {
		rb.res.Attributes().PutStr("k8s.container.name", val)
	}
}

// SetK8sContainerStatusCurrentWaitingReason sets provided value as "k8s.container.status.current_waiting_reason" attribute.
func (rb *ResourceBuilder) SetK8sContainerStatusCurrentWaitingReason(val string) {
	if rb.config.K8sContainerStatusCurrentWaitingReason.Enabled {
		rb.res.Attributes().PutStr("k8s.container.status.current_waiting_reason", val)
	}
}

// SetK8sContainerStatusLastTerminatedReason sets provided value as "k8s.container.status.last_terminated_reason" attribute.
func (rb *ResourceBuilder) SetK8sContainerStatusLastTerminatedReason(val string) {
	if rb.config.K8sContainerStatusLastTerminatedReason.Enabled {
		rb.res.Attributes().PutStr("k8s.container.status.last_terminated_reason", val)
	}
}

// SetK8sCronjobConcurrencyPolicy sets provided value as "k8s.cronjob.concurrency_policy" attribute.
func (rb *ResourceBuilder) SetK8sCronjobConcurrencyPolicy(val string) {
	if rb.config.K8sCronjobConcurrencyPolicy.Enabled {
		rb.res.Attributes().PutStr("k8s.cronjob.concurrency_policy", val)
	}
}

// SetK8sCronjobLastScheduleTime sets provided value as "k8s.cronjob.last_schedule_time" attribute.
func (rb *ResourceBuilder) SetK8sCronjobLastScheduleTime(val string) {
	if rb.config.K8sCronjobLastScheduleTime.Enabled {
		rb.res.Attributes().PutStr("k8s.cronjob.last_schedule_time", val)
	}
}

// SetK8sCronjobLastSuccessfulTime sets provided value as "k8s.cronjob.last_successful_time" attribute.
func (rb *ResourceBuilder) SetK8sCronjobLastSuccessfulTime(val string) {
	if rb.config.K8sCronjobLastSuccessfulTime.Enabled {
		rb.res.Attributes().PutStr("k8s.cronjob.last_successful_time", val)
	}
}

// SetK8sCronjobName sets provided value as "k8s.cronjob.name" attribute.
func (rb *ResourceBuilder) SetK8sCronjobName(val string) {
	if rb.config.K8sCronjobName.Enabled {
		rb.res.Attributes().PutStr("k8s.cronjob.name", val)
	}
}

// SetK8sCronjobSchedule sets provided value as "k8s.cronjob.schedule" attribute.
func (rb *ResourceBuilder) SetK8sCronjobSchedule(val string) {
	if rb.config.K8sCronjobSchedule.Enabled {
		rb.res.Attributes().PutStr("k8s.cronjob.schedule", val)
	}
}

// SetK8sCronjobStartTime sets provided value as "k8s.cronjob.start_time" attribute.
func (rb *ResourceBuilder) SetK8sCronjobStartTime(val string) {
	if rb.config.K8sCronjobStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.cronjob.start_time", val)
	}
}

// SetK8sCronjobSuspend sets provided value as "k8s.cronjob.suspend" attribute.
func (rb *ResourceBuilder) SetK8sCronjobSuspend(val string) {
	if rb.config.K8sCronjobSuspend.Enabled {
		rb.res.Attributes().PutStr("k8s.cronjob.suspend", val)
	}
}

// SetK8sCronjobUID sets provided value as "k8s.cronjob.uid" attribute.
func (rb *ResourceBuilder) SetK8sCronjobUID(val string) {
	if rb.config.K8sCronjobUID.Enabled {
		rb.res.Attributes().PutStr("k8s.cronjob.uid", val)
	}
}

// SetK8sDaemonsetName sets provided value as "k8s.daemonset.name" attribute.
func (rb *ResourceBuilder) SetK8sDaemonsetName(val string) {
	if rb.config.K8sDaemonsetName.Enabled {
		rb.res.Attributes().PutStr("k8s.daemonset.name", val)
	}
}

// SetK8sDaemonsetSelectors sets provided value as "k8s.daemonset.selectors" attribute.
func (rb *ResourceBuilder) SetK8sDaemonsetSelectors(val string) {
	if rb.config.K8sDaemonsetSelectors.Enabled {
		rb.res.Attributes().PutStr("k8s.daemonset.selectors", val)
	}
}

// SetK8sDaemonsetStartTime sets provided value as "k8s.daemonset.start_time" attribute.
func (rb *ResourceBuilder) SetK8sDaemonsetStartTime(val string) {
	if rb.config.K8sDaemonsetStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.daemonset.start_time", val)
	}
}

// SetK8sDaemonsetStrategy sets provided value as "k8s.daemonset.strategy" attribute.
func (rb *ResourceBuilder) SetK8sDaemonsetStrategy(val string) {
	if rb.config.K8sDaemonsetStrategy.Enabled {
		rb.res.Attributes().PutStr("k8s.daemonset.strategy", val)
	}
}

// SetK8sDaemonsetUID sets provided value as "k8s.daemonset.uid" attribute.
func (rb *ResourceBuilder) SetK8sDaemonsetUID(val string) {
	if rb.config.K8sDaemonsetUID.Enabled {
		rb.res.Attributes().PutStr("k8s.daemonset.uid", val)
	}
}

// SetK8sDeploymentName sets provided value as "k8s.deployment.name" attribute.
func (rb *ResourceBuilder) SetK8sDeploymentName(val string) {
	if rb.config.K8sDeploymentName.Enabled {
		rb.res.Attributes().PutStr("k8s.deployment.name", val)
	}
}

// SetK8sDeploymentStartTime sets provided value as "k8s.deployment.start_time" attribute.
func (rb *ResourceBuilder) SetK8sDeploymentStartTime(val string) {
	if rb.config.K8sDeploymentStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.deployment.start_time", val)
	}
}

// SetK8sDeploymentUID sets provided value as "k8s.deployment.uid" attribute.
func (rb *ResourceBuilder) SetK8sDeploymentUID(val string) {
	if rb.config.K8sDeploymentUID.Enabled {
		rb.res.Attributes().PutStr("k8s.deployment.uid", val)
	}
}

// SetK8sHpaName sets provided value as "k8s.hpa.name" attribute.
func (rb *ResourceBuilder) SetK8sHpaName(val string) {
	if rb.config.K8sHpaName.Enabled {
		rb.res.Attributes().PutStr("k8s.hpa.name", val)
	}
}

// SetK8sHpaUID sets provided value as "k8s.hpa.uid" attribute.
func (rb *ResourceBuilder) SetK8sHpaUID(val string) {
	if rb.config.K8sHpaUID.Enabled {
		rb.res.Attributes().PutStr("k8s.hpa.uid", val)
	}
}

// SetK8sIngressAnnotations sets provided value as "k8s.ingress.annotations" attribute.
func (rb *ResourceBuilder) SetK8sIngressAnnotations(val string) {
	if rb.config.K8sIngressAnnotations.Enabled {
		rb.res.Attributes().PutStr("k8s.ingress.annotations", val)
	}
}

// SetK8sIngressLabels sets provided value as "k8s.ingress.labels" attribute.
func (rb *ResourceBuilder) SetK8sIngressLabels(val string) {
	if rb.config.K8sIngressLabels.Enabled {
		rb.res.Attributes().PutStr("k8s.ingress.labels", val)
	}
}

// SetK8sIngressName sets provided value as "k8s.ingress.name" attribute.
func (rb *ResourceBuilder) SetK8sIngressName(val string) {
	if rb.config.K8sIngressName.Enabled {
		rb.res.Attributes().PutStr("k8s.ingress.name", val)
	}
}

// SetK8sIngressNamespace sets provided value as "k8s.ingress.namespace" attribute.
func (rb *ResourceBuilder) SetK8sIngressNamespace(val string) {
	if rb.config.K8sIngressNamespace.Enabled {
		rb.res.Attributes().PutStr("k8s.ingress.namespace", val)
	}
}

// SetK8sIngressRules sets provided value as "k8s.ingress.rules" attribute.
func (rb *ResourceBuilder) SetK8sIngressRules(val string) {
	if rb.config.K8sIngressRules.Enabled {
		rb.res.Attributes().PutStr("k8s.ingress.rules", val)
	}
}

// SetK8sIngressStartTime sets provided value as "k8s.ingress.start_time" attribute.
func (rb *ResourceBuilder) SetK8sIngressStartTime(val string) {
	if rb.config.K8sIngressStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.ingress.start_time", val)
	}
}

// SetK8sIngressType sets provided value as "k8s.ingress.type" attribute.
func (rb *ResourceBuilder) SetK8sIngressType(val string) {
	if rb.config.K8sIngressType.Enabled {
		rb.res.Attributes().PutStr("k8s.ingress.type", val)
	}
}

// SetK8sIngressUID sets provided value as "k8s.ingress.uid" attribute.
func (rb *ResourceBuilder) SetK8sIngressUID(val string) {
	if rb.config.K8sIngressUID.Enabled {
		rb.res.Attributes().PutStr("k8s.ingress.uid", val)
	}
}

// SetK8sJobEndTime sets provided value as "k8s.job.end_time" attribute.
func (rb *ResourceBuilder) SetK8sJobEndTime(val string) {
	if rb.config.K8sJobEndTime.Enabled {
		rb.res.Attributes().PutStr("k8s.job.end_time", val)
	}
}

// SetK8sJobName sets provided value as "k8s.job.name" attribute.
func (rb *ResourceBuilder) SetK8sJobName(val string) {
	if rb.config.K8sJobName.Enabled {
		rb.res.Attributes().PutStr("k8s.job.name", val)
	}
}

// SetK8sJobStartTime sets provided value as "k8s.job.start_time" attribute.
func (rb *ResourceBuilder) SetK8sJobStartTime(val string) {
	if rb.config.K8sJobStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.job.start_time", val)
	}
}

// SetK8sJobUID sets provided value as "k8s.job.uid" attribute.
func (rb *ResourceBuilder) SetK8sJobUID(val string) {
	if rb.config.K8sJobUID.Enabled {
		rb.res.Attributes().PutStr("k8s.job.uid", val)
	}
}

// SetK8sKubeletVersion sets provided value as "k8s.kubelet.version" attribute.
func (rb *ResourceBuilder) SetK8sKubeletVersion(val string) {
	if rb.config.K8sKubeletVersion.Enabled {
		rb.res.Attributes().PutStr("k8s.kubelet.version", val)
	}
}

// SetK8sNamespaceName sets provided value as "k8s.namespace.name" attribute.
func (rb *ResourceBuilder) SetK8sNamespaceName(val string) {
	if rb.config.K8sNamespaceName.Enabled {
		rb.res.Attributes().PutStr("k8s.namespace.name", val)
	}
}

// SetK8sNamespaceStartTime sets provided value as "k8s.namespace.start_time" attribute.
func (rb *ResourceBuilder) SetK8sNamespaceStartTime(val string) {
	if rb.config.K8sNamespaceStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.namespace.start_time", val)
	}
}

// SetK8sNamespaceUID sets provided value as "k8s.namespace.uid" attribute.
func (rb *ResourceBuilder) SetK8sNamespaceUID(val string) {
	if rb.config.K8sNamespaceUID.Enabled {
		rb.res.Attributes().PutStr("k8s.namespace.uid", val)
	}
}

// SetK8sNodeName sets provided value as "k8s.node.name" attribute.
func (rb *ResourceBuilder) SetK8sNodeName(val string) {
	if rb.config.K8sNodeName.Enabled {
		rb.res.Attributes().PutStr("k8s.node.name", val)
	}
}

// SetK8sNodeStartTime sets provided value as "k8s.node.start_time" attribute.
func (rb *ResourceBuilder) SetK8sNodeStartTime(val string) {
	if rb.config.K8sNodeStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.node.start_time", val)
	}
}

// SetK8sNodeUID sets provided value as "k8s.node.uid" attribute.
func (rb *ResourceBuilder) SetK8sNodeUID(val string) {
	if rb.config.K8sNodeUID.Enabled {
		rb.res.Attributes().PutStr("k8s.node.uid", val)
	}
}

// SetK8sPersistentvolumeAccessModes sets provided value as "k8s.persistentvolume.access_modes" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeAccessModes(val string) {
	if rb.config.K8sPersistentvolumeAccessModes.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolume.access_modes", val)
	}
}

// SetK8sPersistentvolumeAnnotations sets provided value as "k8s.persistentvolume.annotations" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeAnnotations(val string) {
	if rb.config.K8sPersistentvolumeAnnotations.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolume.annotations", val)
	}
}

// SetK8sPersistentvolumeFinalizers sets provided value as "k8s.persistentvolume.finalizers" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeFinalizers(val string) {
	if rb.config.K8sPersistentvolumeFinalizers.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolume.finalizers", val)
	}
}

// SetK8sPersistentvolumeLabels sets provided value as "k8s.persistentvolume.labels" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeLabels(val string) {
	if rb.config.K8sPersistentvolumeLabels.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolume.labels", val)
	}
}

// SetK8sPersistentvolumeName sets provided value as "k8s.persistentvolume.name" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeName(val string) {
	if rb.config.K8sPersistentvolumeName.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolume.name", val)
	}
}

// SetK8sPersistentvolumeNamespace sets provided value as "k8s.persistentvolume.namespace" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeNamespace(val string) {
	if rb.config.K8sPersistentvolumeNamespace.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolume.namespace", val)
	}
}

// SetK8sPersistentvolumePhase sets provided value as "k8s.persistentvolume.phase" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumePhase(val string) {
	if rb.config.K8sPersistentvolumePhase.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolume.phase", val)
	}
}

// SetK8sPersistentvolumeReclaimPolicy sets provided value as "k8s.persistentvolume.reclaim_policy" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeReclaimPolicy(val string) {
	if rb.config.K8sPersistentvolumeReclaimPolicy.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolume.reclaim_policy", val)
	}
}

// SetK8sPersistentvolumeStartTime sets provided value as "k8s.persistentvolume.start_time" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeStartTime(val string) {
	if rb.config.K8sPersistentvolumeStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolume.start_time", val)
	}
}

// SetK8sPersistentvolumeStorageClass sets provided value as "k8s.persistentvolume.storage_class" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeStorageClass(val string) {
	if rb.config.K8sPersistentvolumeStorageClass.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolume.storage_class", val)
	}
}

// SetK8sPersistentvolumeType sets provided value as "k8s.persistentvolume.type" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeType(val string) {
	if rb.config.K8sPersistentvolumeType.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolume.type", val)
	}
}

// SetK8sPersistentvolumeUID sets provided value as "k8s.persistentvolume.uid" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeUID(val string) {
	if rb.config.K8sPersistentvolumeUID.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolume.uid", val)
	}
}

// SetK8sPersistentvolumeVolumeMode sets provided value as "k8s.persistentvolume.volume_mode" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeVolumeMode(val string) {
	if rb.config.K8sPersistentvolumeVolumeMode.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolume.volume_mode", val)
	}
}

// SetK8sPersistentvolumeclaimAccessModes sets provided value as "k8s.persistentvolumeclaim.access_modes" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeclaimAccessModes(val string) {
	if rb.config.K8sPersistentvolumeclaimAccessModes.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolumeclaim.access_modes", val)
	}
}

// SetK8sPersistentvolumeclaimAnnotations sets provided value as "k8s.persistentvolumeclaim.annotations" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeclaimAnnotations(val string) {
	if rb.config.K8sPersistentvolumeclaimAnnotations.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolumeclaim.annotations", val)
	}
}

// SetK8sPersistentvolumeclaimFinalizers sets provided value as "k8s.persistentvolumeclaim.finalizers" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeclaimFinalizers(val string) {
	if rb.config.K8sPersistentvolumeclaimFinalizers.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolumeclaim.finalizers", val)
	}
}

// SetK8sPersistentvolumeclaimLabels sets provided value as "k8s.persistentvolumeclaim.labels" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeclaimLabels(val string) {
	if rb.config.K8sPersistentvolumeclaimLabels.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolumeclaim.labels", val)
	}
}

// SetK8sPersistentvolumeclaimName sets provided value as "k8s.persistentvolumeclaim.name" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeclaimName(val string) {
	if rb.config.K8sPersistentvolumeclaimName.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolumeclaim.name", val)
	}
}

// SetK8sPersistentvolumeclaimNamespace sets provided value as "k8s.persistentvolumeclaim.namespace" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeclaimNamespace(val string) {
	if rb.config.K8sPersistentvolumeclaimNamespace.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolumeclaim.namespace", val)
	}
}

// SetK8sPersistentvolumeclaimPhase sets provided value as "k8s.persistentvolumeclaim.phase" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeclaimPhase(val string) {
	if rb.config.K8sPersistentvolumeclaimPhase.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolumeclaim.phase", val)
	}
}

// SetK8sPersistentvolumeclaimSelector sets provided value as "k8s.persistentvolumeclaim.selector" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeclaimSelector(val string) {
	if rb.config.K8sPersistentvolumeclaimSelector.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolumeclaim.selector", val)
	}
}

// SetK8sPersistentvolumeclaimStartTime sets provided value as "k8s.persistentvolumeclaim.start_time" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeclaimStartTime(val string) {
	if rb.config.K8sPersistentvolumeclaimStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolumeclaim.start_time", val)
	}
}

// SetK8sPersistentvolumeclaimStorageClass sets provided value as "k8s.persistentvolumeclaim.storage_class" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeclaimStorageClass(val string) {
	if rb.config.K8sPersistentvolumeclaimStorageClass.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolumeclaim.storage_class", val)
	}
}

// SetK8sPersistentvolumeclaimType sets provided value as "k8s.persistentvolumeclaim.type" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeclaimType(val string) {
	if rb.config.K8sPersistentvolumeclaimType.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolumeclaim.type", val)
	}
}

// SetK8sPersistentvolumeclaimUID sets provided value as "k8s.persistentvolumeclaim.uid" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeclaimUID(val string) {
	if rb.config.K8sPersistentvolumeclaimUID.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolumeclaim.uid", val)
	}
}

// SetK8sPersistentvolumeclaimVolumeMode sets provided value as "k8s.persistentvolumeclaim.volume_mode" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeclaimVolumeMode(val string) {
	if rb.config.K8sPersistentvolumeclaimVolumeMode.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolumeclaim.volume_mode", val)
	}
}

// SetK8sPersistentvolumeclaimVolumeName sets provided value as "k8s.persistentvolumeclaim.volume_name" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeclaimVolumeName(val string) {
	if rb.config.K8sPersistentvolumeclaimVolumeName.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolumeclaim.volume_name", val)
	}
}

// SetK8sPodName sets provided value as "k8s.pod.name" attribute.
func (rb *ResourceBuilder) SetK8sPodName(val string) {
	if rb.config.K8sPodName.Enabled {
		rb.res.Attributes().PutStr("k8s.pod.name", val)
	}
}

// SetK8sPodQosClass sets provided value as "k8s.pod.qos_class" attribute.
func (rb *ResourceBuilder) SetK8sPodQosClass(val string) {
	if rb.config.K8sPodQosClass.Enabled {
		rb.res.Attributes().PutStr("k8s.pod.qos_class", val)
	}
}

// SetK8sPodStartTime sets provided value as "k8s.pod.start_time" attribute.
func (rb *ResourceBuilder) SetK8sPodStartTime(val string) {
	if rb.config.K8sPodStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.pod.start_time", val)
	}
}

// SetK8sPodUID sets provided value as "k8s.pod.uid" attribute.
func (rb *ResourceBuilder) SetK8sPodUID(val string) {
	if rb.config.K8sPodUID.Enabled {
		rb.res.Attributes().PutStr("k8s.pod.uid", val)
	}
}

// SetK8sReplicasetName sets provided value as "k8s.replicaset.name" attribute.
func (rb *ResourceBuilder) SetK8sReplicasetName(val string) {
	if rb.config.K8sReplicasetName.Enabled {
		rb.res.Attributes().PutStr("k8s.replicaset.name", val)
	}
}

// SetK8sReplicasetStartTime sets provided value as "k8s.replicaset.start_time" attribute.
func (rb *ResourceBuilder) SetK8sReplicasetStartTime(val string) {
	if rb.config.K8sReplicasetStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.replicaset.start_time", val)
	}
}

// SetK8sReplicasetUID sets provided value as "k8s.replicaset.uid" attribute.
func (rb *ResourceBuilder) SetK8sReplicasetUID(val string) {
	if rb.config.K8sReplicasetUID.Enabled {
		rb.res.Attributes().PutStr("k8s.replicaset.uid", val)
	}
}

// SetK8sReplicationcontrollerName sets provided value as "k8s.replicationcontroller.name" attribute.
func (rb *ResourceBuilder) SetK8sReplicationcontrollerName(val string) {
	if rb.config.K8sReplicationcontrollerName.Enabled {
		rb.res.Attributes().PutStr("k8s.replicationcontroller.name", val)
	}
}

// SetK8sReplicationcontrollerUID sets provided value as "k8s.replicationcontroller.uid" attribute.
func (rb *ResourceBuilder) SetK8sReplicationcontrollerUID(val string) {
	if rb.config.K8sReplicationcontrollerUID.Enabled {
		rb.res.Attributes().PutStr("k8s.replicationcontroller.uid", val)
	}
}

// SetK8sResourcequotaName sets provided value as "k8s.resourcequota.name" attribute.
func (rb *ResourceBuilder) SetK8sResourcequotaName(val string) {
	if rb.config.K8sResourcequotaName.Enabled {
		rb.res.Attributes().PutStr("k8s.resourcequota.name", val)
	}
}

// SetK8sResourcequotaUID sets provided value as "k8s.resourcequota.uid" attribute.
func (rb *ResourceBuilder) SetK8sResourcequotaUID(val string) {
	if rb.config.K8sResourcequotaUID.Enabled {
		rb.res.Attributes().PutStr("k8s.resourcequota.uid", val)
	}
}

// SetK8sRoleAnnotations sets provided value as "k8s.role.annotations" attribute.
func (rb *ResourceBuilder) SetK8sRoleAnnotations(val string) {
	if rb.config.K8sRoleAnnotations.Enabled {
		rb.res.Attributes().PutStr("k8s.role.annotations", val)
	}
}

// SetK8sRoleLabels sets provided value as "k8s.role.labels" attribute.
func (rb *ResourceBuilder) SetK8sRoleLabels(val string) {
	if rb.config.K8sRoleLabels.Enabled {
		rb.res.Attributes().PutStr("k8s.role.labels", val)
	}
}

// SetK8sRoleName sets provided value as "k8s.role.name" attribute.
func (rb *ResourceBuilder) SetK8sRoleName(val string) {
	if rb.config.K8sRoleName.Enabled {
		rb.res.Attributes().PutStr("k8s.role.name", val)
	}
}

// SetK8sRoleNamespace sets provided value as "k8s.role.namespace" attribute.
func (rb *ResourceBuilder) SetK8sRoleNamespace(val string) {
	if rb.config.K8sRoleNamespace.Enabled {
		rb.res.Attributes().PutStr("k8s.role.namespace", val)
	}
}

// SetK8sRoleRules sets provided value as "k8s.role.rules" attribute.
func (rb *ResourceBuilder) SetK8sRoleRules(val string) {
	if rb.config.K8sRoleRules.Enabled {
		rb.res.Attributes().PutStr("k8s.role.rules", val)
	}
}

// SetK8sRoleStartTime sets provided value as "k8s.role.start_time" attribute.
func (rb *ResourceBuilder) SetK8sRoleStartTime(val string) {
	if rb.config.K8sRoleStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.role.start_time", val)
	}
}

// SetK8sRoleType sets provided value as "k8s.role.type" attribute.
func (rb *ResourceBuilder) SetK8sRoleType(val string) {
	if rb.config.K8sRoleType.Enabled {
		rb.res.Attributes().PutStr("k8s.role.type", val)
	}
}

// SetK8sRoleUID sets provided value as "k8s.role.uid" attribute.
func (rb *ResourceBuilder) SetK8sRoleUID(val string) {
	if rb.config.K8sRoleUID.Enabled {
		rb.res.Attributes().PutStr("k8s.role.uid", val)
	}
}

// SetK8sRolebindingAnnotations sets provided value as "k8s.rolebinding.annotations" attribute.
func (rb *ResourceBuilder) SetK8sRolebindingAnnotations(val string) {
	if rb.config.K8sRolebindingAnnotations.Enabled {
		rb.res.Attributes().PutStr("k8s.rolebinding.annotations", val)
	}
}

// SetK8sRolebindingLabels sets provided value as "k8s.rolebinding.labels" attribute.
func (rb *ResourceBuilder) SetK8sRolebindingLabels(val string) {
	if rb.config.K8sRolebindingLabels.Enabled {
		rb.res.Attributes().PutStr("k8s.rolebinding.labels", val)
	}
}

// SetK8sRolebindingName sets provided value as "k8s.rolebinding.name" attribute.
func (rb *ResourceBuilder) SetK8sRolebindingName(val string) {
	if rb.config.K8sRolebindingName.Enabled {
		rb.res.Attributes().PutStr("k8s.rolebinding.name", val)
	}
}

// SetK8sRolebindingNamespace sets provided value as "k8s.rolebinding.namespace" attribute.
func (rb *ResourceBuilder) SetK8sRolebindingNamespace(val string) {
	if rb.config.K8sRolebindingNamespace.Enabled {
		rb.res.Attributes().PutStr("k8s.rolebinding.namespace", val)
	}
}

// SetK8sRolebindingRoleRef sets provided value as "k8s.rolebinding.role_ref" attribute.
func (rb *ResourceBuilder) SetK8sRolebindingRoleRef(val string) {
	if rb.config.K8sRolebindingRoleRef.Enabled {
		rb.res.Attributes().PutStr("k8s.rolebinding.role_ref", val)
	}
}

// SetK8sRolebindingStartTime sets provided value as "k8s.rolebinding.start_time" attribute.
func (rb *ResourceBuilder) SetK8sRolebindingStartTime(val string) {
	if rb.config.K8sRolebindingStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.rolebinding.start_time", val)
	}
}

// SetK8sRolebindingSubjects sets provided value as "k8s.rolebinding.subjects" attribute.
func (rb *ResourceBuilder) SetK8sRolebindingSubjects(val string) {
	if rb.config.K8sRolebindingSubjects.Enabled {
		rb.res.Attributes().PutStr("k8s.rolebinding.subjects", val)
	}
}

// SetK8sRolebindingType sets provided value as "k8s.rolebinding.type" attribute.
func (rb *ResourceBuilder) SetK8sRolebindingType(val string) {
	if rb.config.K8sRolebindingType.Enabled {
		rb.res.Attributes().PutStr("k8s.rolebinding.type", val)
	}
}

// SetK8sRolebindingUID sets provided value as "k8s.rolebinding.uid" attribute.
func (rb *ResourceBuilder) SetK8sRolebindingUID(val string) {
	if rb.config.K8sRolebindingUID.Enabled {
		rb.res.Attributes().PutStr("k8s.rolebinding.uid", val)
	}
}

// SetK8sServiceClusterIP sets provided value as "k8s.service.cluster_ip" attribute.
func (rb *ResourceBuilder) SetK8sServiceClusterIP(val string) {
	if rb.config.K8sServiceClusterIP.Enabled {
		rb.res.Attributes().PutStr("k8s.service.cluster_ip", val)
	}
}

// SetK8sServiceName sets provided value as "k8s.service.name" attribute.
func (rb *ResourceBuilder) SetK8sServiceName(val string) {
	if rb.config.K8sServiceName.Enabled {
		rb.res.Attributes().PutStr("k8s.service.name", val)
	}
}

// SetK8sServiceNamespace sets provided value as "k8s.service.namespace" attribute.
func (rb *ResourceBuilder) SetK8sServiceNamespace(val string) {
	if rb.config.K8sServiceNamespace.Enabled {
		rb.res.Attributes().PutStr("k8s.service.namespace", val)
	}
}

// SetK8sServiceType sets provided value as "k8s.service.type" attribute.
func (rb *ResourceBuilder) SetK8sServiceType(val string) {
	if rb.config.K8sServiceType.Enabled {
		rb.res.Attributes().PutStr("k8s.service.type", val)
	}
}

// SetK8sServiceUID sets provided value as "k8s.service.uid" attribute.
func (rb *ResourceBuilder) SetK8sServiceUID(val string) {
	if rb.config.K8sServiceUID.Enabled {
		rb.res.Attributes().PutStr("k8s.service.uid", val)
	}
}

// SetK8sServiceAccountName sets provided value as "k8s.service_account.name" attribute.
func (rb *ResourceBuilder) SetK8sServiceAccountName(val string) {
	if rb.config.K8sServiceAccountName.Enabled {
		rb.res.Attributes().PutStr("k8s.service_account.name", val)
	}
}

// SetK8sServiceaccountAnnotations sets provided value as "k8s.serviceaccount.annotations" attribute.
func (rb *ResourceBuilder) SetK8sServiceaccountAnnotations(val string) {
	if rb.config.K8sServiceaccountAnnotations.Enabled {
		rb.res.Attributes().PutStr("k8s.serviceaccount.annotations", val)
	}
}

// SetK8sServiceaccountAutomountServiceaccountToken sets provided value as "k8s.serviceaccount.automount_serviceaccount_token" attribute.
func (rb *ResourceBuilder) SetK8sServiceaccountAutomountServiceaccountToken(val string) {
	if rb.config.K8sServiceaccountAutomountServiceaccountToken.Enabled {
		rb.res.Attributes().PutStr("k8s.serviceaccount.automount_serviceaccount_token", val)
	}
}

// SetK8sServiceaccountImagePullSecrets sets provided value as "k8s.serviceaccount.image_pull_secrets" attribute.
func (rb *ResourceBuilder) SetK8sServiceaccountImagePullSecrets(val string) {
	if rb.config.K8sServiceaccountImagePullSecrets.Enabled {
		rb.res.Attributes().PutStr("k8s.serviceaccount.image_pull_secrets", val)
	}
}

// SetK8sServiceaccountLabels sets provided value as "k8s.serviceaccount.labels" attribute.
func (rb *ResourceBuilder) SetK8sServiceaccountLabels(val string) {
	if rb.config.K8sServiceaccountLabels.Enabled {
		rb.res.Attributes().PutStr("k8s.serviceaccount.labels", val)
	}
}

// SetK8sServiceaccountName sets provided value as "k8s.serviceaccount.name" attribute.
func (rb *ResourceBuilder) SetK8sServiceaccountName(val string) {
	if rb.config.K8sServiceaccountName.Enabled {
		rb.res.Attributes().PutStr("k8s.serviceaccount.name", val)
	}
}

// SetK8sServiceaccountNamespace sets provided value as "k8s.serviceaccount.namespace" attribute.
func (rb *ResourceBuilder) SetK8sServiceaccountNamespace(val string) {
	if rb.config.K8sServiceaccountNamespace.Enabled {
		rb.res.Attributes().PutStr("k8s.serviceaccount.namespace", val)
	}
}

// SetK8sServiceaccountSecrets sets provided value as "k8s.serviceaccount.secrets" attribute.
func (rb *ResourceBuilder) SetK8sServiceaccountSecrets(val string) {
	if rb.config.K8sServiceaccountSecrets.Enabled {
		rb.res.Attributes().PutStr("k8s.serviceaccount.secrets", val)
	}
}

// SetK8sServiceaccountStartTime sets provided value as "k8s.serviceaccount.start_time" attribute.
func (rb *ResourceBuilder) SetK8sServiceaccountStartTime(val string) {
	if rb.config.K8sServiceaccountStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.serviceaccount.start_time", val)
	}
}

// SetK8sServiceaccountType sets provided value as "k8s.serviceaccount.type" attribute.
func (rb *ResourceBuilder) SetK8sServiceaccountType(val string) {
	if rb.config.K8sServiceaccountType.Enabled {
		rb.res.Attributes().PutStr("k8s.serviceaccount.type", val)
	}
}

// SetK8sServiceaccountUID sets provided value as "k8s.serviceaccount.uid" attribute.
func (rb *ResourceBuilder) SetK8sServiceaccountUID(val string) {
	if rb.config.K8sServiceaccountUID.Enabled {
		rb.res.Attributes().PutStr("k8s.serviceaccount.uid", val)
	}
}

// SetK8sStatefulsetName sets provided value as "k8s.statefulset.name" attribute.
func (rb *ResourceBuilder) SetK8sStatefulsetName(val string) {
	if rb.config.K8sStatefulsetName.Enabled {
		rb.res.Attributes().PutStr("k8s.statefulset.name", val)
	}
}

// SetK8sStatefulsetPodManagementPolicy sets provided value as "k8s.statefulset.pod_management_policy" attribute.
func (rb *ResourceBuilder) SetK8sStatefulsetPodManagementPolicy(val string) {
	if rb.config.K8sStatefulsetPodManagementPolicy.Enabled {
		rb.res.Attributes().PutStr("k8s.statefulset.pod_management_policy", val)
	}
}

// SetK8sStatefulsetServiceName sets provided value as "k8s.statefulset.service_name" attribute.
func (rb *ResourceBuilder) SetK8sStatefulsetServiceName(val string) {
	if rb.config.K8sStatefulsetServiceName.Enabled {
		rb.res.Attributes().PutStr("k8s.statefulset.service_name", val)
	}
}

// SetK8sStatefulsetStartTime sets provided value as "k8s.statefulset.start_time" attribute.
func (rb *ResourceBuilder) SetK8sStatefulsetStartTime(val string) {
	if rb.config.K8sStatefulsetStartTime.Enabled {
		rb.res.Attributes().PutStr("k8s.statefulset.start_time", val)
	}
}

// SetK8sStatefulsetUID sets provided value as "k8s.statefulset.uid" attribute.
func (rb *ResourceBuilder) SetK8sStatefulsetUID(val string) {
	if rb.config.K8sStatefulsetUID.Enabled {
		rb.res.Attributes().PutStr("k8s.statefulset.uid", val)
	}
}

// SetOpenshiftClusterquotaName sets provided value as "openshift.clusterquota.name" attribute.
func (rb *ResourceBuilder) SetOpenshiftClusterquotaName(val string) {
	if rb.config.OpenshiftClusterquotaName.Enabled {
		rb.res.Attributes().PutStr("openshift.clusterquota.name", val)
	}
}

// SetOpenshiftClusterquotaUID sets provided value as "openshift.clusterquota.uid" attribute.
func (rb *ResourceBuilder) SetOpenshiftClusterquotaUID(val string) {
	if rb.config.OpenshiftClusterquotaUID.Enabled {
		rb.res.Attributes().PutStr("openshift.clusterquota.uid", val)
	}
}

// SetOsDescription sets provided value as "os.description" attribute.
func (rb *ResourceBuilder) SetOsDescription(val string) {
	if rb.config.OsDescription.Enabled {
		rb.res.Attributes().PutStr("os.description", val)
	}
}

// SetOsType sets provided value as "os.type" attribute.
func (rb *ResourceBuilder) SetOsType(val string) {
	if rb.config.OsType.Enabled {
		rb.res.Attributes().PutStr("os.type", val)
	}
}

// Emit returns the built resource and resets the internal builder state.
func (rb *ResourceBuilder) Emit() pcommon.Resource {
	r := rb.res
	rb.res = pcommon.NewResource()
	return r
}
