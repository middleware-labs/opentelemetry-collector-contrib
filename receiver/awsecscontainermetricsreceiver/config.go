// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package awsecscontainermetricsreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver"

import (
	"time"
)

// RunAsMode defines how the receiver is deployed.
type RunAsMode string

const (
	// RunAsSidecar collects metrics from the current task only via Task Metadata API.
	// Requires running as a sidecar within an ECS task with ECS_CONTAINER_METADATA_URI_V4 set.
	RunAsSidecar RunAsMode = "sidecar"
	// RunAsDaemonSet collects metrics for all tasks and services in the cluster via ECS APIs.
	// Requires cluster name and AWS credentials (task role or IAM). Use when deployed as one
	// task per EC2 instance (e.g. DaemonSet-style) to observe sibling tasks.
	RunAsDaemonSet RunAsMode = "daemonset"
)

// Config defines configuration for aws ecs container metrics receiver.
type Config struct {
	// CollectionInterval is the interval at which metrics should be collected
	CollectionInterval time.Duration `mapstructure:"collection_interval"`

	// RunAs determines the collection mode: "sidecar" (default) or "daemonset".
	// - sidecar: Uses Task Metadata API for current task only.
	// - daemonset: Uses ECS APIs (ListTasks, DescribeTasks, ListServices, DescribeServices)
	//   to collect task and service metadata for the entire cluster.
	RunAs RunAsMode `mapstructure:"run_as"`

	// Cluster is the ECS cluster name or ARN. Required when run_as is "daemonset".
	Cluster string `mapstructure:"cluster"`

	// Region is the AWS region. Optional when run_as is "daemonset"; defaults to
	// AWS_REGION env or EC2/ECS metadata when empty.
	Region string `mapstructure:"region"`

	// CGroupsMountPath is the path to the cgroup mount (e.g. /sys/fs/cgroup or /host/sys/fs/cgroup
	// when host is mounted). When set in daemonset mode on EC2, the receiver will read CPU and
	// memory usage from cgroups for tasks on this instance, populating ecs.task.cpu.utilized,
	// ecs.task.memory.utilized, etc. Requires the collector to have host cgroup filesystem mounted.
	// Only supported on Linux.
	CGroupsMountPath string `mapstructure:"cgroups_mount_path"`

	// DockerSocketPath is the path to the Docker daemon socket (e.g. /var/run/docker.sock or
	// /host/var/run/docker.sock when host is mounted). When set in daemonset mode, the receiver
	// will fetch full container stats (CPU, memory, network, disk) via the Docker API for tasks
	// on this instance, covering network and disk metrics that cgroups do not provide. Requires
	// the collector to have the Docker socket mounted. When both cgroups_mount_path and
	// docker_socket_path are set, Docker is used (full metrics); otherwise cgroups is used.
	DockerSocketPath string `mapstructure:"docker_socket_path"`

	// prevent unkeyed literal initialization
	_ struct{}
}
