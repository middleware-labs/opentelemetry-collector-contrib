// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package gvk // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/gvk"

import "k8s.io/apimachinery/pkg/runtime/schema"

// Kubernetes group version kinds
var (
	Pod                         = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	Node                        = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Node"}
	PersistentVolume            = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "PersistentVolume"}
	PersistentVolumeClaim       = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "PersistentVolumeClaim"}
	Namespace                   = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
	ReplicationController       = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ReplicationController"}
	ResourceQuota               = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ResourceQuota"}
	Service                     = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"}
	ServiceAccount              = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ServiceAccount"}
	DaemonSet                   = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DaemonSet"}
	Deployment                  = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	ReplicaSet                  = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}
	StatefulSet                 = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"}
	Job                         = schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "Job"}
	CronJob                     = schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "CronJob"}
	CronJobBeta                 = schema.GroupVersionKind{Group: "batch", Version: "v1beta1", Kind: "CronJob"}
	HorizontalPodAutoscaler     = schema.GroupVersionKind{Group: "autoscaling", Version: "v2", Kind: "HorizontalPodAutoscaler"}
	HorizontalPodAutoscalerBeta = schema.GroupVersionKind{Group: "autoscaling", Version: "v2beta2", Kind: "HorizontalPodAutoscaler"}
	ClusterResourceQuota        = schema.GroupVersionKind{Group: "quota", Version: "v1", Kind: "ClusterResourceQuota"}
	Role                        = schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"}
	RoleBinding                 = schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"}
	ClusterRole                 = schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}
	ClusterRoleBinding          = schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"}
	Ingress                     = schema.GroupVersionKind{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"}
)
