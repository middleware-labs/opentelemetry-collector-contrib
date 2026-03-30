// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ecsutil // import "github.com/open-telemetry/opentelemetry-collector-contrib/internal/aws/ecsutil"

// TaskMetadata defines task metadata for a task
type TaskMetadata struct {
	AvailabilityZone string              `json:"AvailabilityZone,omitempty"`
	Cluster          string              `json:"Cluster,omitempty"`
	Containers       []ContainerMetadata `json:"Containers,omitempty"`
	Family           string              `json:"Family,omitempty"`
	KnownStatus      string              `json:"KnownStatus,omitempty"`
	LaunchType       string              `json:"LaunchType,omitempty"`
	Limits           Limits              `json:"Limits,omitzero"`
	PullStartedAt    string              `json:"PullStartedAt,omitempty"`
	PullStoppedAt    string              `json:"PullStoppedAt,omitempty"`
	Revision         string              `json:"Revision,omitempty"`
	ServiceName      string              `json:"ServiceName,omitempty"`
	ServiceArn       string              `json:"ServiceArn,omitempty"`
	TaskARN          string              `json:"TaskARN,omitempty"`
}

// ServiceMetadata holds ECS service fields from DescribeServices for service-level metrics (daemonset mode).
// Populated from AWS API; optional fields are omitted when empty.
type ServiceMetadata struct {
	ClusterName   string `json:"ClusterName,omitempty"`
	ClusterARN    string `json:"ClusterArn,omitempty"`
	ServiceName   string `json:"ServiceName,omitempty"`
	ServiceArn    string `json:"ServiceArn,omitempty"`
	Status        string `json:"Status,omitempty"`
	TaskDefinition string `json:"TaskDefinition,omitempty"` // task definition ARN
	LaunchType    string `json:"LaunchType,omitempty"`
	SchedulingStrategy string `json:"SchedulingStrategy,omitempty"`
	PlatformFamily string `json:"PlatformFamily,omitempty"`
	PlatformVersion string `json:"PlatformVersion,omitempty"`
	CreatedAt     string `json:"CreatedAt,omitempty"` // RFC3339 from DescribeServices CreatedAt
	// HealthCheckGracePeriodSeconds is set when the API returns a value (seconds).
	HealthCheckGracePeriodSeconds *int32 `json:"HealthCheckGracePeriodSeconds,omitempty"`
	EnableExecuteCommand          bool   `json:"EnableExecuteCommand,omitempty"`
	EnableECSManagedTags          bool   `json:"EnableECSManagedTags,omitempty"`
	PropagateTags                 string `json:"PropagateTags,omitempty"`
	RoleArn                       string `json:"RoleArn,omitempty"`
}

// ContainerMetadata defines container metadata for a container
type ContainerMetadata struct {
	ContainerARN  string            `json:"ContainerARN,omitempty"`
	ContainerName string            `json:"Name,omitempty"`
	CreatedAt     string            `json:"CreatedAt,omitempty"`
	DockerID      string            `json:"DockerId,omitempty"`
	DockerName    string            `json:"DockerName,omitempty"`
	ExitCode      *int64            `json:"ExitCode,omitempty"`
	FinishedAt    string            `json:"FinishedAt,omitempty"`
	Image         string            `json:"Image,omitempty"`
	ImageID       string            `json:"ImageID,omitempty"`
	KnownStatus   string            `json:"KnownStatus,omitempty"`
	Labels        map[string]string `json:"Labels,omitempty"`
	Limits        Limits            `json:"Limits,omitzero"`
	LogDriver     string            `json:"LogDriver,omitempty"`
	LogOptions    LogOptions        `json:"LogOptions,omitzero"`
	Networks      []Network         `json:"Networks,omitempty"`
	StartedAt     string            `json:"StartedAt,omitempty"`
	Type          string            `json:"Type,omitempty"`
}

// Limits defines the Cpu and Memory limits
type Limits struct {
	CPU    *float64 `json:"CPU,omitempty"`
	Memory *uint64  `json:"Memory,omitempty"`
}

// LogOptions defines the CloudWatch configuration
type LogOptions struct {
	LogGroup string `json:"awslogs-group,omitempty"`
	Region   string `json:"awslogs-region,omitempty"`
	Stream   string `json:"awslogs-stream,omitempty"`
}

type Network struct {
	IPv4Addresses []string `json:"IPv4Addresses,omitempty"`
	NetworkMode   string   `json:"NetworkMode,omitempty"`
}
