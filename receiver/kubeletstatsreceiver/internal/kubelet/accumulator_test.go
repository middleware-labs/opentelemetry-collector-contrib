// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package kubelet

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vone "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	stats "k8s.io/kubelet/pkg/apis/stats/v1alpha1"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/kubeletstatsreceiver/internal/metadata"
)

// TestMetadataErrorCases walks through the error cases of collecting
// metadata. Happy paths are covered in metadata_test.go and volume_test.go.
func TestMetadataErrorCases(t *testing.T) {
	tests := []struct {
		name                            string
		metricGroupsToCollect           map[MetricGroup]bool
		testScenario                    func(acc metricDataAccumulator)
		metadata                        Metadata
		numMDs                          int
		numLogs                         int
		logMessages                     []string
		detailedPVCLabelsSetterOverride func(rb *metadata.ResourceBuilder, volCacheID, volumeClaim, namespace string) ([]metadata.ResourceMetricsOption, error)
	}{
		{
			name: "Fails to get container metadata",
			metricGroupsToCollect: map[MetricGroup]bool{
				ContainerMetricGroup: true,
			},
			metadata: NewMetadata([]MetadataLabel{MetadataLabelContainerID}, &v1.PodList{
				Items: []v1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							UID: "pod-uid-123",
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name: "container2",
								},
							},
						},
					},
				},
			}, NodeCapacity{}, nil),
			testScenario: func(acc metricDataAccumulator) {
				now := metav1.Now()
				podStats := stats.PodStats{
					PodRef: stats.PodReference{
						UID: "pod-uid-123",
					},
				}
				containerStats := stats.ContainerStats{
					Name:      "container1",
					StartTime: now,
				}

				acc.containerStats(podStats, containerStats)
			},
			numMDs:  0,
			numLogs: 1,
			logMessages: []string{
				"failed to fetch container metrics",
			},
		},
		{
			name: "Fails to get volume metadata - no pods data",
			metricGroupsToCollect: map[MetricGroup]bool{
				VolumeMetricGroup: true,
			},
			metadata: NewMetadata([]MetadataLabel{MetadataLabelVolumeType}, nil, NodeCapacity{}, nil),
			testScenario: func(acc metricDataAccumulator) {
				podStats := stats.PodStats{
					PodRef: stats.PodReference{
						UID: "pod-uid-123",
					},
				}
				volumeStats := stats.VolumeStats{
					Name: "volume-1",
				}

				acc.volumeStats(podStats, volumeStats)
			},
			numMDs:  0,
			numLogs: 1,
			logMessages: []string{
				"Failed to gather additional volume metadata. Skipping metric collection.",
			},
		},
		{
			name: "Fails to get volume metadata - volume not found",
			metricGroupsToCollect: map[MetricGroup]bool{
				VolumeMetricGroup: true,
			},
			metadata: NewMetadata([]MetadataLabel{MetadataLabelVolumeType}, &v1.PodList{
				Items: []v1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							UID: "pod-uid-123",
						},
						Spec: v1.PodSpec{
							Volumes: []v1.Volume{
								{
									Name: "volume-0",
									VolumeSource: v1.VolumeSource{
										HostPath: &v1.HostPathVolumeSource{},
									},
								},
							},
						},
					},
				},
			}, NodeCapacity{}, nil),
			testScenario: func(acc metricDataAccumulator) {
				podStats := stats.PodStats{
					PodRef: stats.PodReference{
						UID: "pod-uid-123",
					},
				}
				volumeStats := stats.VolumeStats{
					Name: "volume-1",
				}

				acc.volumeStats(podStats, volumeStats)
			},
			numMDs:  0,
			numLogs: 1,
			logMessages: []string{
				"Failed to gather additional volume metadata. Skipping metric collection.",
			},
		},
		{
			name: "Fails to get volume metadata - metadata from PVC",
			metricGroupsToCollect: map[MetricGroup]bool{
				VolumeMetricGroup: true,
			},
			metadata: NewMetadata([]MetadataLabel{MetadataLabelVolumeType}, &v1.PodList{
				Items: []v1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							UID: "pod-uid-123",
						},
						Spec: v1.PodSpec{
							Volumes: []v1.Volume{
								{
									Name: "volume-0",
									VolumeSource: v1.VolumeSource{
										PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
											ClaimName: "claim",
										},
									},
								},
							},
						},
					},
				},
			}, NodeCapacity{}, nil),
			detailedPVCLabelsSetterOverride: func(*metadata.ResourceBuilder, string, string, string) error {
				// Mock failure cases.
				return nil, errors.New("")
			},
			testScenario: func(acc metricDataAccumulator) {
				podStats := stats.PodStats{
					PodRef: stats.PodReference{
						UID: "pod-uid-123",
					},
				}
				volumeStats := stats.VolumeStats{
					Name: "volume-0",
				}

				acc.volumeStats(podStats, volumeStats)
			},
			numMDs:  0,
			numLogs: 1,
			logMessages: []string{
				"Failed to gather additional volume metadata. Skipping metric collection.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observedLogger, logs := observer.New(zapcore.WarnLevel)
			logger := zap.New(observedLogger)

			tt.metadata.DetailedPVCResourceSetter = tt.detailedPVCLabelsSetterOverride
			acc := metricDataAccumulator{
				metadata:              tt.metadata,
				logger:                logger,
				metricGroupsToCollect: tt.metricGroupsToCollect,
				mbs: &metadata.MetricsBuilders{
					NodeMetricsBuilder:      metadata.NewMetricsBuilder(metadata.DefaultMetricsBuilderConfig(), receivertest.NewNopSettings()),
					PodMetricsBuilder:       metadata.NewMetricsBuilder(metadata.DefaultMetricsBuilderConfig(), receivertest.NewNopSettings()),
					ContainerMetricsBuilder: metadata.NewMetricsBuilder(metadata.DefaultMetricsBuilderConfig(), receivertest.NewNopSettings()),
					OtherMetricsBuilder:     metadata.NewMetricsBuilder(metadata.DefaultMetricsBuilderConfig(), receivertest.NewNopSettings()),
				},
			}

			tt.testScenario(acc)

			assert.Len(t, acc.m, tt.numMDs)
			require.Equal(t, tt.numLogs, logs.Len())
			for i := 0; i < tt.numLogs; i++ {
				assert.Equal(t, tt.logMessages[i], logs.All()[i].Message)
			}
		})
	}
}

func TestNilHandling(t *testing.T) {
	acc := metricDataAccumulator{
		metricGroupsToCollect: map[MetricGroup]bool{
			PodMetricGroup:       true,
			NodeMetricGroup:      true,
			ContainerMetricGroup: true,
			VolumeMetricGroup:    true,
		},
		mbs: &metadata.MetricsBuilders{
			NodeMetricsBuilder:      metadata.NewMetricsBuilder(metadata.DefaultMetricsBuilderConfig(), receivertest.NewNopSettings()),
			PodMetricsBuilder:       metadata.NewMetricsBuilder(metadata.DefaultMetricsBuilderConfig(), receivertest.NewNopSettings()),
			ContainerMetricsBuilder: metadata.NewMetricsBuilder(metadata.DefaultMetricsBuilderConfig(), receivertest.NewNopSettings()),
			OtherMetricsBuilder:     metadata.NewMetricsBuilder(metadata.DefaultMetricsBuilderConfig(), receivertest.NewNopSettings()),
		},
	}
	assert.NotPanics(t, func() {
		acc.nodeStats(stats.NodeStats{})
	})
	assert.NotPanics(t, func() {
		acc.podStats(stats.PodStats{})
	})
	assert.NotPanics(t, func() {
		acc.containerStats(stats.PodStats{}, stats.ContainerStats{})
	})
	assert.NotPanics(t, func() {
		acc.volumeStats(stats.PodStats{}, stats.VolumeStats{})
	})
}

func TestGetServiceName(t *testing.T) {
	// Create a fake Kubernetes client
	client := fake.NewSimpleClientset()

	// Create a Pod with labels
	var pods []v1.Pod
	pods = append(pods, v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "test-pod-uid-123",
			Name:      "test-pod-1",
			Namespace: "test-namespace",
			Labels: map[string]string{
				"foo":  "bar",
				"foo1": "",
			},
		},
		Spec: v1.PodSpec{},
	})

	acc := metricDataAccumulator{
		metadata: NewMetadata([]MetadataLabel{MetadataLabelContainerID}, &v1.PodList{
			Items: pods,
		}, &v1.NodeList{Items: []v1.Node{}}, NodeLimits{}, nil),
	}

	// Create a Service with the same labels as the Pod
	service := &v1.Service{
		ObjectMeta: vone.ObjectMeta{
			Name:      "test-service",
			Namespace: "test-namespace",
		},
		Spec: v1.ServiceSpec{
			Selector: map[string]string{
				"foo":  "bar",
				"foo1": "",
			},
		},
	}

	// Create the Service in the fake client
	_, err := client.CoreV1().Services(service.Namespace).Create(context.TODO(), service, vone.CreateOptions{})
	assert.NoError(t, err)

	// Call the getServiceName method
	result := acc.getServiceName(client, string(pods[0].UID))

	// Verify the result
	assert.Equal(t, service.Name, result)
}

func TestGetServiceAccountName(t *testing.T) {
	// Create a Pod with labels
	var pods []v1.Pod
	pods = append(pods, v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "test-pod-uid-123",
			Name:      "test-pod-1",
			Namespace: "test-namespace",
			Labels: map[string]string{
				"foo":  "bar",
				"foo1": "",
			},
		},
		Spec: v1.PodSpec{
			ServiceAccountName: "test-service-account",
		},
	})

	acc := metricDataAccumulator{
		metadata: NewMetadata([]MetadataLabel{MetadataLabelContainerID}, &v1.PodList{
			Items: pods,
		}, &v1.NodeList{Items: []v1.Node{}}, NodeLimits{}, nil),
	}

	// Call the getServiceName method
	result := acc.getServiceAccountName(string(pods[0].UID))

	// Verify the result
	expectedServiceAccountName := "test-service-account"

	assert.Equal(t, expectedServiceAccountName, result)
}

func TestGetJobInfo(t *testing.T) {
	// Create a fake Kubernetes client
	client := fake.NewSimpleClientset()

	// Create a Pod with labels
	var pods []v1.Pod
	pods = append(pods, v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "test-pod-uid-123",
			Name:      "test-pod-1",
			Namespace: "test-namespace",
			Labels: map[string]string{
				"foo":  "bar",
				"foo1": "",
			},
		},
		Spec: v1.PodSpec{},
	})

	acc := metricDataAccumulator{
		metadata: NewMetadata([]MetadataLabel{MetadataLabelContainerID}, &v1.PodList{
			Items: pods,
		}, &v1.NodeList{Items: []v1.Node{}}, NodeLimits{}, nil),
	}

	// Create a Job with the same labels as the Pod
	job := &batchv1.Job{
		ObjectMeta: vone.ObjectMeta{
			Name:      "test-job-1",
			Namespace: "test-namespace",
			UID:       types.UID("test-job-1-uid"),
			Labels: map[string]string{
				"foo":  "bar",
				"foo1": "",
			},
		},
		Spec: batchv1.JobSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"foo":  "bar",
					"foo1": "",
				},
			},
		},
	}

	// Create the Job in the fake client
	_, err := client.BatchV1().Jobs(job.Namespace).Create(context.TODO(), job, vone.CreateOptions{})
	assert.NoError(t, err)

	// Call the getJobInfo method
	jobInfo := acc.getJobInfo(client, string(pods[0].UID))

	// Verify the result
	expectedJobInfo := JobInfo{
		Name: "test-job-1",
		UID:  job.UID,
	}

	assert.Equal(t, expectedJobInfo, jobInfo)
}
