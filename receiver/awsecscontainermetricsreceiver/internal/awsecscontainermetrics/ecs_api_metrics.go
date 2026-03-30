// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package awsecscontainermetrics // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/awsecscontainermetrics"

import (
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	conventions "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/aws/ecsutil"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/ecsclient"
)

// TaskStatsMap maps task ARN -> container DockerID -> ContainerStats.
// When provided, usage metrics (cpu.utilized, memory.utilized, etc.) are populated.
type TaskStatsMap map[string]map[string]*ContainerStats

// MetricsDataFromECSAPI generates OTLP metrics from ECS API task and service data.
// Used when run_as is "daemonset". When taskStats is provided (e.g. from cgroups on EC2),
// usage metrics are populated; otherwise usage stays at 0.
func MetricsDataFromECSAPI(
	tasks []ecstypes.Task,
	taskDefs map[string]*ecstypes.TaskDefinition,
	services []ecstypes.Service,
	cluster string,
	taskStats TaskStatsMap,
	logger *zap.Logger,
) []pmetric.Metrics {
	var mds []pmetric.Metrics
	timestamp := pcommon.NewTimestampFromTime(time.Now())
	clusterName := ecsclient.GetClusterName(cluster)

	// Build deployment ID -> service name and service ARN maps
	svcNameByDeployment := make(map[string]string)
	svcArnByDeployment := make(map[string]string)
	for i := range services {
		s := &services[i]
		for j := range s.Deployments {
			d := &s.Deployments[j]
			status := aws.ToString(d.Status)
			if status == "ACTIVE" || status == "PRIMARY" {
				svcNameByDeployment[aws.ToString(d.Id)] = aws.ToString(s.ServiceName)
				svcArnByDeployment[aws.ToString(d.Id)] = aws.ToString(s.ServiceArn)
				break
			}
		}
	}

	// Emit task-level metrics for each task
	for i := range tasks {
		task := &tasks[i]
		metadata := convertECSTaskToTaskMetadata(task, taskDefs, svcNameByDeployment, svcArnByDeployment, clusterName)
		stats := taskStats[aws.ToString(task.TaskArn)]
		if stats == nil {
			stats = make(map[string]*ContainerStats)
		}
		taskMds := MetricsData(stats, metadata, logger)
		mds = append(mds, taskMds...)
	}

	// Emit service-level metrics
	for i := range services {
		s := &services[i]
		md := serviceMetricsToOTLP(s, cluster, timestamp)
		if md.ResourceMetrics().Len() > 0 {
			mds = append(mds, md)
		}
	}

	return mds
}

func convertECSTaskToTaskMetadata(
	task *ecstypes.Task,
	taskDefs map[string]*ecstypes.TaskDefinition,
	svcNameByDeployment map[string]string,
	svcArnByDeployment map[string]string,
	clusterName string,
) ecsutil.TaskMetadata {
	startedBy := aws.ToString(task.StartedBy)
	metadata := ecsutil.TaskMetadata{
		TaskARN:     aws.ToString(task.TaskArn),
		Cluster:     aws.ToString(task.ClusterArn),
		Family:      "",
		Revision:    "",
		KnownStatus: aws.ToString(task.LastStatus),
		LaunchType:  string(task.LaunchType),
		ServiceName: svcNameByDeployment[startedBy],
		ServiceArn:  svcArnByDeployment[startedBy],
	}

	if task.AvailabilityZone != nil {
		metadata.AvailabilityZone = *task.AvailabilityZone
	}

	// Get family and revision from task definition ARN
	if task.TaskDefinitionArn != nil {
		// ARN format: arn:aws:ecs:region:account:task-definition/family:revision
		arn := *task.TaskDefinitionArn
		if idx := len(arn) - 1; idx >= 0 {
			for j := idx; j >= 0; j-- {
				if arn[j] == ':' {
					metadata.Revision = arn[j+1:]
					familyPart := arn[:j]
					for k := len(familyPart) - 1; k >= 0; k-- {
						if familyPart[k] == '/' {
							metadata.Family = familyPart[k+1:]
							break
						}
					}
					break
				}
			}
		}
	}

	// Set limits from task definition and task overrides
	if def := taskDefs[aws.ToString(task.TaskDefinitionArn)]; def != nil {
		var taskCPU float64
		var taskMem uint64
		// Fargate: task.Cpu and task.Memory are set. CPU is in units (1024=1 vCPU), Memory in MB.
		if task.Cpu != nil {
			if v, err := strconv.ParseFloat(*task.Cpu, 64); err == nil {
				taskCPU = v / cpusInVCpu // convert to vCPU for metadata.Limits.CPU
			}
		}
		if task.Memory != nil {
			if v, err := strconv.ParseUint(*task.Memory, 10, 64); err == nil {
				taskMem = v * 1024 * 1024 // MB to bytes
			}
		}
		// EC2: sum from container definitions if task-level not set (units: 1024=1 vCPU)
		if taskCPU == 0 || taskMem == 0 {
			var cpuUnits float64
			for j := range def.ContainerDefinitions {
				cd := &def.ContainerDefinitions[j]
				if taskCPU == 0 && cd.Cpu != 0 {
					cpuUnits += float64(cd.Cpu)
				}
				if taskMem == 0 && cd.Memory != nil {
					taskMem += uint64(*cd.Memory) * 1024 * 1024 // ECS uses MB
				}
			}
			if taskCPU == 0 && cpuUnits > 0 {
				taskCPU = cpuUnits / cpusInVCpu
			}
		}
		if taskCPU > 0 {
			metadata.Limits.CPU = &taskCPU
		}
		if taskMem > 0 {
			metadata.Limits.Memory = &taskMem
		}

		// Build container metadata from task containers + definition
		containerDefs := make(map[string]*ecstypes.ContainerDefinition)
		for j := range def.ContainerDefinitions {
			cd := &def.ContainerDefinitions[j]
			if cd.Name != nil {
				containerDefs[*cd.Name] = cd
			}
		}
		for j := range task.Containers {
			ec := &task.Containers[j]
			cm := ecsutil.ContainerMetadata{
				ContainerName: aws.ToString(ec.Name),
				DockerID:      aws.ToString(ec.RuntimeId),
				KnownStatus:   aws.ToString(ec.LastStatus),
			}
			if ec.ContainerArn != nil {
				cm.ContainerARN = *ec.ContainerArn
			}
			if ec.Image != nil {
				cm.Image = *ec.Image
			}
			if cd := containerDefs[aws.ToString(ec.Name)]; cd != nil {
				if cd.Cpu != 0 {
					cpu := float64(cd.Cpu) / cpusInVCpu
					cm.Limits.CPU = &cpu
				}
				if cd.Memory != nil {
					mem := uint64(*cd.Memory) * 1024 * 1024
					cm.Limits.Memory = &mem
				}
			}
			metadata.Containers = append(metadata.Containers, cm)
		}
	}

	return metadata
}

func convertECSServiceToServiceMetadata(svc *ecstypes.Service, cluster string) ecsutil.ServiceMetadata {
	sm := ecsutil.ServiceMetadata{
		ClusterName:          ecsclient.GetClusterName(cluster),
		ClusterARN:           aws.ToString(svc.ClusterArn),
		ServiceName:          aws.ToString(svc.ServiceName),
		ServiceArn:           aws.ToString(svc.ServiceArn),
		Status:               aws.ToString(svc.Status),
		TaskDefinition:       aws.ToString(svc.TaskDefinition),
		LaunchType:           string(svc.LaunchType),
		SchedulingStrategy:   string(svc.SchedulingStrategy),
		PlatformFamily:       aws.ToString(svc.PlatformFamily),
		PlatformVersion:      aws.ToString(svc.PlatformVersion),
		PropagateTags:        string(svc.PropagateTags),
		RoleArn:              aws.ToString(svc.RoleArn),
		EnableExecuteCommand: svc.EnableExecuteCommand,
		EnableECSManagedTags: svc.EnableECSManagedTags,
	}
	if svc.CreatedAt != nil {
		sm.CreatedAt = svc.CreatedAt.Format(time.RFC3339Nano)
	}
	if svc.HealthCheckGracePeriodSeconds != nil {
		v := *svc.HealthCheckGracePeriodSeconds
		sm.HealthCheckGracePeriodSeconds = &v
	}
	return sm
}

// serviceMetricsToOTLP emits one ResourceMetrics batch per ECS service: desired/running/pending
// plus deployment configuration and deployment count (service-specific metrics).
func serviceMetricsToOTLP(svc *ecstypes.Service, cluster string, timestamp pcommon.Timestamp) pmetric.Metrics {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.SetSchemaUrl(conventions.SchemaURL)
	serviceResource(convertECSServiceToServiceMetadata(svc, cluster)).CopyTo(rm.Resource())

	ilms := rm.ScopeMetrics().AppendEmpty()
	appendServiceIntGauge(ilms, attributeECSServiceDesiredCount, int64(svc.DesiredCount), timestamp)
	appendServiceIntGauge(ilms, attributeECSServiceRunningCount, int64(svc.RunningCount), timestamp)
	appendServiceIntGauge(ilms, attributeECSServicePendingCount, int64(svc.PendingCount), timestamp)
	appendServiceIntGauge(ilms, attributeECSServiceDeployments, int64(len(svc.Deployments)), timestamp)

	if dc := svc.DeploymentConfiguration; dc != nil {
		if dc.MinimumHealthyPercent != nil {
			appendServiceIntGauge(ilms, attributeECSServiceDeploymentMinHealthyPct, int64(*dc.MinimumHealthyPercent), timestamp)
		}
		if dc.MaximumPercent != nil {
			appendServiceIntGauge(ilms, attributeECSServiceDeploymentMaxPct, int64(*dc.MaximumPercent), timestamp)
		}
	}

	return md
}

func appendServiceIntGauge(ilm pmetric.ScopeMetrics, name string, value int64, ts pcommon.Timestamp) {
	metric := ilm.Metrics().AppendEmpty()
	metric.SetName(name)
	metric.SetUnit(unitCount)
	dp := metric.SetEmptyGauge().DataPoints().AppendEmpty()
	dp.SetIntValue(value)
	dp.SetTimestamp(ts)
}
