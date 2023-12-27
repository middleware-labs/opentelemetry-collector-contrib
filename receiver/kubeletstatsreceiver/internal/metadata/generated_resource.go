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

// SetAwsVolumeID sets provided value as "aws.volume.id" attribute.
func (rb *ResourceBuilder) SetAwsVolumeID(val string) {
	if rb.config.AwsVolumeID.Enabled {
		rb.res.Attributes().PutStr("aws.volume.id", val)
	}
}

// SetContainerID sets provided value as "container.id" attribute.
func (rb *ResourceBuilder) SetContainerID(val string) {
	if rb.config.ContainerID.Enabled {
		rb.res.Attributes().PutStr("container.id", val)
	}
}

// SetFsType sets provided value as "fs.type" attribute.
func (rb *ResourceBuilder) SetFsType(val string) {
	if rb.config.FsType.Enabled {
		rb.res.Attributes().PutStr("fs.type", val)
	}
}

// SetGcePdName sets provided value as "gce.pd.name" attribute.
func (rb *ResourceBuilder) SetGcePdName(val string) {
	if rb.config.GcePdName.Enabled {
		rb.res.Attributes().PutStr("gce.pd.name", val)
	}
}

// SetGlusterfsEndpointsName sets provided value as "glusterfs.endpoints.name" attribute.
func (rb *ResourceBuilder) SetGlusterfsEndpointsName(val string) {
	if rb.config.GlusterfsEndpointsName.Enabled {
		rb.res.Attributes().PutStr("glusterfs.endpoints.name", val)
	}
}

// SetGlusterfsPath sets provided value as "glusterfs.path" attribute.
func (rb *ResourceBuilder) SetGlusterfsPath(val string) {
	if rb.config.GlusterfsPath.Enabled {
		rb.res.Attributes().PutStr("glusterfs.path", val)
	}
}

// SetK8sClusterName sets provided value as "k8s.cluster.name" attribute.
func (rb *ResourceBuilder) SetK8sClusterName(val string) {
	if rb.config.K8sClusterName.Enabled {
		rb.res.Attributes().PutStr("k8s.cluster.name", val)
	}
}

// SetK8sContainerName sets provided value as "k8s.container.name" attribute.
func (rb *ResourceBuilder) SetK8sContainerName(val string) {
	if rb.config.K8sContainerName.Enabled {
		rb.res.Attributes().PutStr("k8s.container.name", val)
	}
}

// SetK8sNamespaceName sets provided value as "k8s.namespace.name" attribute.
func (rb *ResourceBuilder) SetK8sNamespaceName(val string) {
	if rb.config.K8sNamespaceName.Enabled {
		rb.res.Attributes().PutStr("k8s.namespace.name", val)
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

// SetK8sPersistentvolumeclaimName sets provided value as "k8s.persistentvolumeclaim.name" attribute.
func (rb *ResourceBuilder) SetK8sPersistentvolumeclaimName(val string) {
	if rb.config.K8sPersistentvolumeclaimName.Enabled {
		rb.res.Attributes().PutStr("k8s.persistentvolumeclaim.name", val)
	}
}

// SetK8sPodName sets provided value as "k8s.pod.name" attribute.
func (rb *ResourceBuilder) SetK8sPodName(val string) {
	if rb.config.K8sPodName.Enabled {
		rb.res.Attributes().PutStr("k8s.pod.name", val)
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

// SetK8sServiceName sets provided value as "k8s.service.name" attribute.
func (rb *ResourceBuilder) SetK8sServiceName(val string) {
	if rb.config.K8sServiceName.Enabled {
		rb.res.Attributes().PutStr("k8s.service.name", val)
	}
}

// SetK8sServiceAccountName sets provided value as "k8s.service_account.name" attribute.
func (rb *ResourceBuilder) SetK8sServiceAccountName(val string) {
	if rb.config.K8sServiceAccountName.Enabled {
		rb.res.Attributes().PutStr("k8s.service_account.name", val)
	}
}

// SetK8sVolumeName sets provided value as "k8s.volume.name" attribute.
func (rb *ResourceBuilder) SetK8sVolumeName(val string) {
	if rb.config.K8sVolumeName.Enabled {
		rb.res.Attributes().PutStr("k8s.volume.name", val)
	}
}

// SetK8sVolumeType sets provided value as "k8s.volume.type" attribute.
func (rb *ResourceBuilder) SetK8sVolumeType(val string) {
	if rb.config.K8sVolumeType.Enabled {
		rb.res.Attributes().PutStr("k8s.volume.type", val)
	}
}

// SetPartition sets provided value as "partition" attribute.
func (rb *ResourceBuilder) SetPartition(val string) {
	if rb.config.Partition.Enabled {
		rb.res.Attributes().PutStr("partition", val)
	}
}

// Emit returns the built resource and resets the internal builder state.
func (rb *ResourceBuilder) Emit() pcommon.Resource {
	r := rb.res
	rb.res = pcommon.NewResource()
	return r
}
