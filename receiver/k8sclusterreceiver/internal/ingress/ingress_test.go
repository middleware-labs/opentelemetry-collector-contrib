package ingress

import (
	"testing"

	"github.com/stretchr/testify/assert"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTransform(t *testing.T) {
	originalI := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-ingress",
			UID:  "my-ingress-uid",
		},
	}
	wantI := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-ingress",
			UID:  "my-ingress-uid",
		},
	}
	assert.Equal(t, wantI, Transform(originalI))
}

func TestConvertIngressRulesToString(t *testing.T) {
	pathType := netv1.PathTypePrefix
	tests := []struct {
		name     string
		rules    []netv1.IngressRule
		expected string
	}{
		{
			name: "single rule with path",
			rules: []netv1.IngressRule{
				{
					Host: "hello.local",
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: []netv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: netv1.IngressBackend{
										Service: &netv1.IngressServiceBackend{
											Name: "hello",
											Port: netv1.ServiceBackendPort{
												Number: 80,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: "host=hello.local&http=(paths=(path=/&pathType=Prefix&backend=(service=(name=hello&port=(number=80)))))",
		},
		{
			name: "rule with nil HTTP",
			rules: []netv1.IngressRule{
				{
					Host: "hello.local",
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: nil,
					},
				},
			},
			expected: "host=hello.local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, convertIngressRulesToString(tt.rules))
		})
	}
}
