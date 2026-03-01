// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package cgroupstats

import (
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/awsecscontainermetrics"
)

// Reader reads CPU and memory stats from cgroup files for ECS tasks.
type Reader interface {
	GetContainerStats(taskARN string, clusterName string, dockerID string, containerName string) (*awsecscontainermetrics.ContainerStats, error)
}
