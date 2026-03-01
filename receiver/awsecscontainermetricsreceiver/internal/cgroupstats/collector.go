// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package cgroupstats // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/cgroupstats"

import (
	"context"

	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/awsecscontainermetrics"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/ecsagent"
)

// CollectTaskStats builds a TaskStatsMap from ECS agent tasks by reading cgroup stats.
// Returns nil if collection fails (caller should use empty stats).
func CollectTaskStats(
	ctx context.Context,
	mountPath string,
	ecsTasks []ecsagent.ECSAgentTask,
	clusterName string,
	logger *zap.Logger,
) awsecscontainermetrics.TaskStatsMap {
	if mountPath == "" || len(ecsTasks) == 0 || clusterName == "" {
		return nil
	}
	reader, err := NewReader(mountPath, clusterName, logger)
	if err != nil {
		logger.Warn("Failed to create cgroup reader, usage metrics will be 0", zap.Error(err))
		return nil
	}
	result := make(awsecscontainermetrics.TaskStatsMap)
	for _, task := range ecsTasks {
		if task.KnownStatus != "RUNNING" {
			continue
		}
		containerStats := make(map[string]*awsecscontainermetrics.ContainerStats)
		for _, c := range task.Containers {
			stats, err := reader.GetContainerStats(task.ARN, clusterName, c.DockerID, c.Name)
			if err != nil {
				logger.Debug("Could not get cgroup stats for container",
					zap.String("task", task.ARN),
					zap.String("container", c.Name),
					zap.Error(err))
				continue
			}
			containerStats[c.DockerID] = stats
		}
		if len(containerStats) > 0 {
			result[task.ARN] = containerStats
		}
	}
	return result
}
