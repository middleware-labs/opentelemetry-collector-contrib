// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package awsecscontainermetrics // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/awsecscontainermetrics"

// Constant attributes for aws ecs container metrics.
const (
	attributeECSDockerName        = "aws.ecs.docker.name"
	attributeECSCluster           = "aws.ecs.cluster.name"
	attributeECSTaskID            = "aws.ecs.task.id"
	attributeECSTaskRevision      = "aws.ecs.task.version"
	attributeECSServiceName       = "aws.ecs.service.name"
	attributeECSServiceARN        = "aws.ecs.service.arn"
	attributeECSClusterARN        = "aws.ecs.cluster.arn"

	attributeECSServiceStatus                     = "aws.ecs.service.status"
	attributeECSTaskDefinitionARN                 = "aws.ecs.task_definition.arn"
	attributeECSServiceLaunchType                 = "aws.ecs.service.launch_type"
	attributeECSServiceSchedulingStrategy         = "aws.ecs.service.scheduling_strategy"
	attributeECSServicePlatformFamily             = "aws.ecs.service.platform_family"
	attributeECSServicePlatformVersion            = "aws.ecs.service.platform_version"
	attributeECSServiceCreatedAt                  = "aws.ecs.service.created_at"
	attributeECSServiceHealthCheckGracePeriodSecs = "aws.ecs.service.health_check_grace_period_seconds"
	attributeECSServiceEnableExecuteCommand       = "aws.ecs.service.enable_execute_command"
	attributeECSServiceEnableECSManagedTags       = "aws.ecs.service.enable_ecs_managed_tags"
	attributeECSServicePropagateTags              = "aws.ecs.service.propagate_tags"
	attributeECSServiceRoleArn                    = "aws.ecs.service.role_arn"
	attributeECSTaskPullStartedAt = "aws.ecs.task.pull_started_at"
	attributeECSTaskPullStoppedAt = "aws.ecs.task.pull_stopped_at"
	attributeECSTaskKnownStatus   = "aws.ecs.task.known_status"
	attributeECSTaskLaunchType    = "aws.ecs.task.launch_type"
	attributeContainerImageID     = "aws.ecs.container.image.id"
	attributeContainerCreatedAt   = "aws.ecs.container.created_at"
	attributeContainerStartedAt   = "aws.ecs.container.started_at"
	attributeContainerFinishedAt  = "aws.ecs.container.finished_at"
	attributeContainerKnownStatus = "aws.ecs.container.know_status"
	attributeContainerExitCode    = "aws.ecs.container.exit_code"

	cpusInVCpu = 1024
	bytesInMiB = 1024 * 1024

	taskPrefix      = "ecs.task."
	containerPrefix = "container."
	servicePrefix = "ecs.service."

	attributeECSServiceDesiredCount            = servicePrefix + "desired_count"
	attributeECSServiceRunningCount            = servicePrefix + "running_count"
	attributeECSServicePendingCount            = servicePrefix + "pending_count"
	attributeECSServiceDeployments             = servicePrefix + "deployments"
	attributeECSServiceDeploymentMinHealthyPct = servicePrefix + "deployment.minimum_healthy_percent"
	attributeECSServiceDeploymentMaxPct        = servicePrefix + "deployment.maximum_percent"

	attributeMemoryUsage    = "memory.usage"
	attributeMemoryMaxUsage = "memory.usage.max"
	attributeMemoryLimit    = "memory.usage.limit"
	attributeMemoryReserved = "memory.reserved"
	attributeMemoryUtilized = "memory.utilized"

	attributeCPUTotalUsage      = "cpu.usage.total"
	attributeCPUKernelModeUsage = "cpu.usage.kernelmode"
	attributeCPUUserModeUsage   = "cpu.usage.usermode"
	attributeCPUSystemUsage     = "cpu.usage.system"
	attributeCPUCores           = "cpu.cores"
	attributeCPUOnlines         = "cpu.onlines"
	attributeCPUReserved        = "cpu.reserved"
	attributeCPUUtilized        = "cpu.utilized"
	attributeCPUUsageInVCPU     = "cpu.usage.vcpu"

	attributeNetworkRateRx = "network.rate.rx"
	attributeNetworkRateTx = "network.rate.tx"

	attributeNetworkRxBytes   = "network.io.usage.rx_bytes"
	attributeNetworkRxPackets = "network.io.usage.rx_packets"
	attributeNetworkRxErrors  = "network.io.usage.rx_errors"
	attributeNetworkRxDropped = "network.io.usage.rx_dropped"
	attributeNetworkTxBytes   = "network.io.usage.tx_bytes"
	attributeNetworkTxPackets = "network.io.usage.tx_packets"
	attributeNetworkTxErrors  = "network.io.usage.tx_errors"
	attributeNetworkTxDropped = "network.io.usage.tx_dropped"

	attributeStorageRead  = "storage.read_bytes"
	attributeStorageWrite = "storage.write_bytes"

	attributeDuration = "duration"

	unitBytes       = "Bytes"
	unitMegaBytes   = "Megabytes"
	unitNanoSecond  = "Nanoseconds"
	unitBytesPerSec = "Bytes/Second"
	unitCount       = "Count"
	unitVCpu        = "vCPU"
	unitSecond      = "Seconds"
	unitNone        = "None"
)
