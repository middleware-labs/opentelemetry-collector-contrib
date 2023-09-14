// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package service // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/service"
import (
	"context"
	"fmt"
	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/k8sconfig"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"

	"go.opentelemetry.io/collector/pdata/pcommon"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/constants"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/metadata"
	imetadata "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/metadata"
)

// Transform transforms the pod to remove the fields that we don't use to reduce RAM utilization.
// IMPORTANT: Make sure to update this function before using new service fields.
func Transform(service *corev1.Service) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metadata.TransformObjectMeta(service.ObjectMeta),
		Spec: corev1.ServiceSpec{
			Selector: service.Spec.Selector,
		},
	}
}

// GetPodServiceTags returns a set of services associated with the pod.
func GetPodServiceTags(pod *corev1.Pod, services cache.Store) map[string]string {
	properties := map[string]string{}

	for _, ser := range services.List() {
		serObj := ser.(*corev1.Service)
		if serObj.Namespace == pod.Namespace &&
			labels.Set(serObj.Spec.Selector).AsSelectorPreValidated().Matches(labels.Set(pod.Labels)) {
			properties[fmt.Sprintf("%s%s", constants.K8sServicePrefix, serObj.Name)] = ""
		}
	}

	return properties
}

func RecordMetrics(mb *imetadata.MetricsBuilder, svc *corev1.Service, ts pcommon.Timestamp) {
	log.Println("svc:", svc)
	log.Println("svc.Spec.Ports====>", int64(len(svc.Spec.Ports)))
	mb.RecordK8sServicePortCountDataPoint(ts, int64(len(svc.Spec.Ports)))
	rb := mb.NewResourceBuilder()

	//serviceDetails :=
	serviceDetail := GetServiceDetails(svc)
	log.Println("service: ", serviceDetail)
	log.Println("service.spec: ", serviceDetail.Spec)

	rb.SetK8sServiceClusterIP(svc.Spec.ClusterIP)
	rb.SetK8sServiceName(svc.ObjectMeta.Name)
	rb.SetK8sServiceNamespace(svc.ObjectMeta.Namespace)
	rb.SetK8sServiceUID(string(svc.UID))
	rb.SetK8sServiceType(string(svc.Spec.Type))
	rb.SetK8sClusterName("unknown")
	rb.SetK8sServiceClusterIP(svc.Spec.ClusterIP)
	mb.EmitForResource(metadata.WithResource(rb.Emit()))
}

func GetServiceDetails(svc *corev1.Service) *corev1.Service {
	var svcObject *corev1.Service

	client, _ := k8sconfig.MakeClient(k8sconfig.APIConfig{
		AuthType: k8sconfig.AuthTypeServiceAccount,
	})
	log.Println("Namespace: ", svc.ObjectMeta.Namespace)
	log.Println("Name: ", svc.ObjectMeta.Name)
	service, err := client.CoreV1().Services(svc.ObjectMeta.Namespace).Get(context.TODO(), svc.ObjectMeta.Name, v1.GetOptions{})
	log.Println("err: ", err, service)
	if err != nil {
		panic(err)
	} else {
		svcObject = service
	}

	return svcObject
}
