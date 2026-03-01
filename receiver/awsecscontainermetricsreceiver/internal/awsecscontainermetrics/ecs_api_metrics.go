// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package awsecscontainermetrics // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/awsecscontainermetrics"

import (
	"strconv"
	"strings"
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

const (
	attributeECSServiceDesiredCount = "ecs.service.desired_count"
	attributeECSServiceRunningCount = "ecs.service.running_count"
	attributeECSServicePendingCount = "ecs.service.pending_count"
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

	// Build deployment ID -> service name map
	svcByDeployment := make(map[string]string)
	for i := range services {
		s := &services[i]
		for j := range s.Deployments {
			d := &s.Deployments[j]
			status := aws.ToString(d.Status)
			if status == "ACTIVE" || status == "PRIMARY" {
				svcByDeployment[aws.ToString(d.Id)] = aws.ToString(s.ServiceName)
				break
			}
		}
	}

	// Emit task-level metrics for each task
	for i := range tasks {
		task := &tasks[i]
		metadata := convertECSTaskToTaskMetadata(task, taskDefs, svcByDeployment, clusterName)
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
	svcByDeployment map[string]string,
	clusterName string,
) ecsutil.TaskMetadata {
	metadata := ecsutil.TaskMetadata{
		TaskARN:     aws.ToString(task.TaskArn),
		Cluster:     aws.ToString(task.ClusterArn),
		Family:      "",
		Revision:    "",
		KnownStatus: aws.ToString(task.LastStatus),
		LaunchType:  string(task.LaunchType),
		ServiceName: svcByDeployment[aws.ToString(task.StartedBy)],
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

func serviceMetricsToOTLP(svc *ecstypes.Service, cluster string, timestamp pcommon.Timestamp) pmetric.Metrics {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.SetSchemaUrl(conventions.SchemaURL)
	res := rm.Resource()

	clusterName := ecsclient.GetClusterName(cluster)
	res.Attributes().PutStr("aws.ecs.cluster.name", clusterName)
	res.Attributes().PutStr("aws.ecs.service.name", aws.ToString(svc.ServiceName))
	res.Attributes().PutStr("aws.ecs.service.arn", aws.ToString(svc.ServiceArn))

	region, accountID := getRegionAndAccountFromARN(aws.ToString(svc.ServiceArn))
	if region != "" {
		res.Attributes().PutStr(string(conventions.CloudRegionKey), region)
	}
	if accountID != "" {
		res.Attributes().PutStr(string(conventions.CloudAccountIDKey), accountID)
	}

	ilms := rm.ScopeMetrics().AppendEmpty()
	desired := int64(svc.DesiredCount)
	running := int64(svc.RunningCount)
	pending := int64(svc.PendingCount)

	appendServiceIntGauge(ilms, attributeECSServiceDesiredCount, desired, timestamp)
	appendServiceIntGauge(ilms, attributeECSServiceRunningCount, running, timestamp)
	appendServiceIntGauge(ilms, attributeECSServicePendingCount, pending, timestamp)

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

func getRegionAndAccountFromARN(arn string) (region, accountID string) {
	if arn == "" || !strings.HasPrefix(arn, "arn:aws:") {
		return "", ""
	}
	parts := strings.Split(arn, ":")
	if len(parts) >= 5 {
		return parts[3], parts[4]
	}
	return "", ""
}
