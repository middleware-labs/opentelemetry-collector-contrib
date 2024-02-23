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

// SetK8sContainerName sets provided value as "k8s.container.name" attribute.
func (rb *ResourceBuilder) SetK8sContainerName(val string) {
	if rb.config.K8sContainerName.Enabled {
		rb.res.Attributes().PutStr("k8s.container.name", val)
	}
}

// SetK8sContainerStatusLastTerminatedReason sets provided value as "k8s.container.status.last_terminated_reason" attribute.
func (rb *ResourceBuilder) SetK8sContainerStatusLastTerminatedReason(val string) {
	if rb.config.K8sContainerStatusLastTerminatedReason.Enabled {
		rb.res.Attributes().PutStr("k8s.container.status.last_terminated_reason", val)
	}
}

// SetK8sCronjobName sets provided value as "k8s.cronjob.name" attribute.
func (rb *ResourceBuilder) SetK8sCronjobName(val string) {
	if rb.config.K8sCronjobName.Enabled {
		rb.res.Attributes().PutStr("k8s.cronjob.name", val)
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

// SetK8sJobName sets provided value as "k8s.job.name" attribute.
func (rb *ResourceBuilder) SetK8sJobName(val string) {
	if rb.config.K8sJobName.Enabled {
		rb.res.Attributes().PutStr("k8s.job.name", val)
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

// SetK8sStatefulsetName sets provided value as "k8s.statefulset.name" attribute.
func (rb *ResourceBuilder) SetK8sStatefulsetName(val string) {
	if rb.config.K8sStatefulsetName.Enabled {
		rb.res.Attributes().PutStr("k8s.statefulset.name", val)
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
