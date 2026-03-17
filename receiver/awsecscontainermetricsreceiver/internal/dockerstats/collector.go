// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dockerstats // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/dockerstats"

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	ctypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/awsecscontainermetrics"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/ecsagent"
)

const defaultDockerTimeout = 5 * time.Second

// CollectTaskStats fetches container stats from the Docker API for ECS tasks on this instance.
// Returns a TaskStatsMap keyed by task ARN and container DockerID.
// When Docker socket is available, this provides full metrics including network and disk.
func CollectTaskStats(
	ctx context.Context,
	dockerSocketPath string,
	ecsTasks []ecsagent.ECSAgentTask,
	clusterName string,
	logger *zap.Logger,
) awsecscontainermetrics.TaskStatsMap {
	if dockerSocketPath == "" || len(ecsTasks) == 0 {
		logger.Debug("Docker stats skipped", zap.Bool("socket_set", dockerSocketPath != ""), zap.Int("ecs_tasks", len(ecsTasks)))
		return nil
	}
	dockerClient, err := client.NewClientWithOpts(client.WithHost("unix://"+dockerSocketPath), client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Warn("Failed to create Docker client for stats, skipping Docker stats", zap.String("socket", dockerSocketPath), zap.Error(err))
		return nil
	}
	defer dockerClient.Close()
	logger.Debug("Docker client created, fetching stats for containers")
	result := make(awsecscontainermetrics.TaskStatsMap)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var failedCount atomic.Int32
	for _, task := range ecsTasks {
		if task.KnownStatus != "RUNNING" {
			continue
		}
		for _, c := range task.Containers {
			wg.Add(1)
			go func(taskARN, dockerID, containerName string) {
				defer wg.Done()
				statsResp, err := fetchContainerStats(ctx, dockerClient, dockerID)
				if err != nil {
					failedCount.Add(1)
					logger.Debug("Could not get Docker stats for container",
						zap.String("task", taskARN),
						zap.String("container", containerName),
						zap.String("docker_id", dockerID),
						zap.Error(err))
					return
				}
				stats := dockerStatsToContainerStats(statsResp, dockerID, containerName)
				if stats == nil {
					return
				}
				var cpuVal, memVal uint64
				if stats.CPU != nil && stats.CPU.CPUUsage != nil && stats.CPU.CPUUsage.TotalUsage != nil {
					cpuVal = *stats.CPU.CPUUsage.TotalUsage
				}
				if stats.Memory != nil && stats.Memory.Usage != nil {
					memVal = *stats.Memory.Usage
				}
				logger.Debug("Docker stats for container",
					zap.String("container", containerName),
					zap.String("docker_id", dockerID),
					zap.Uint64("cpu_total_usage_ns", cpuVal),
					zap.Uint64("memory_usage_bytes", memVal))
				mu.Lock()
				if result[taskARN] == nil {
					result[taskARN] = make(map[string]*awsecscontainermetrics.ContainerStats)
				}
				result[taskARN][dockerID] = stats
				mu.Unlock()
			}(task.ARN, c.DockerID, c.Name)
		}
	}
	wg.Wait()
	if failedCount.Load() > 0 {
		logger.Debug("Docker stats summary", zap.Int("containers_failed", int(failedCount.Load())), zap.Int("containers_ok", totalContainerStats(result)))
	}
	return result
}

func totalContainerStats(m awsecscontainermetrics.TaskStatsMap) int {
	n := 0
	for _, cm := range m {
		n += len(cm)
	}
	return n
}

// fetchContainerStats fetches stats for a container from the Docker API.
func fetchContainerStats(ctx context.Context, dockerClient *client.Client, containerID string) (*ctypes.StatsResponse, error) {
	statsCtx, cancel := context.WithTimeout(ctx, defaultDockerTimeout)
	defer cancel()
	statsReader, err := dockerClient.ContainerStats(statsCtx, containerID, false)
	if err != nil {
		return nil, err
	}
	defer statsReader.Body.Close()
	var resp ctypes.StatsResponse
	if err := json.NewDecoder(statsReader.Body).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// dockerStatsToContainerStats converts Docker API StatsResponse to awsecscontainermetrics.ContainerStats.
func dockerStatsToContainerStats(resp *ctypes.StatsResponse, dockerID, containerName string) *awsecscontainermetrics.ContainerStats {
	if resp == nil {
		return nil
	}
	stats := &awsecscontainermetrics.ContainerStats{
		ID:           dockerID,
		Name:         containerName,
		Read:         resp.Read,
		PreviousRead: resp.PreRead,
	}

	// Always set memory from Docker response (Usage can be 0 for idle containers)
	usage := resp.MemoryStats.Usage
	maxUsage := resp.MemoryStats.MaxUsage
	limit := resp.MemoryStats.Limit
	stats.Memory = &awsecscontainermetrics.MemoryStats{
		Usage:    &usage,
		MaxUsage: &maxUsage,
		Limit:    &limit,
		Stats:    resp.MemoryStats.Stats,
	}

	if resp.CPUStats.CPUUsage.TotalUsage != 0 || resp.PreCPUStats.CPUUsage.TotalUsage != 0 {
		totalUsage := resp.CPUStats.CPUUsage.TotalUsage
		kernelMode := resp.CPUStats.CPUUsage.UsageInKernelmode
		userMode := resp.CPUStats.CPUUsage.UsageInUsermode
		onlineCpus := uint64(resp.CPUStats.OnlineCPUs)
		systemUsage := resp.CPUStats.SystemUsage
		stats.CPU = &awsecscontainermetrics.CPUStats{
			CPUUsage: &awsecscontainermetrics.CPUUsage{
				TotalUsage:        &totalUsage,
				UsageInKernelmode: &kernelMode,
				UsageInUserMode:   &userMode,
				PerCPUUsage:       ptrSlice(resp.CPUStats.CPUUsage.PercpuUsage),
			},
			OnlineCpus:     &onlineCpus,
			SystemCPUUsage: &systemUsage,
		}
		prevTotal := resp.PreCPUStats.CPUUsage.TotalUsage
		prevKernel := resp.PreCPUStats.CPUUsage.UsageInKernelmode
		prevUser := resp.PreCPUStats.CPUUsage.UsageInUsermode
		stats.PreviousCPU = &awsecscontainermetrics.CPUStats{
			CPUUsage: &awsecscontainermetrics.CPUUsage{
				TotalUsage:        &prevTotal,
				UsageInKernelmode: &prevKernel,
				UsageInUserMode:   &prevUser,
			},
		}
	}

	if len(resp.Networks) > 0 {
		stats.Network = make(map[string]awsecscontainermetrics.NetworkStats)
		for iface, n := range resp.Networks {
			stats.Network[iface] = awsecscontainermetrics.NetworkStats{
				RxBytes:   &n.RxBytes,
				RxPackets: &n.RxPackets,
				RxErrors:  &n.RxErrors,
				RxDropped: &n.RxDropped,
				TxBytes:   &n.TxBytes,
				TxPackets: &n.TxPackets,
				TxErrors:  &n.TxErrors,
				TxDropped: &n.TxDropped,
			}
		}
	}

	if len(resp.BlkioStats.IoServiceBytesRecursive) > 0 {
		entries := make([]awsecscontainermetrics.IoServiceBytesRecursive, 0, len(resp.BlkioStats.IoServiceBytesRecursive))
		for _, e := range resp.BlkioStats.IoServiceBytesRecursive {
			major, minor, value := e.Major, e.Minor, e.Value
			entries = append(entries, awsecscontainermetrics.IoServiceBytesRecursive{
				Major: &major,
				Minor: &minor,
				Op:    e.Op,
				Value: &value,
			})
		}
		stats.Disk = &awsecscontainermetrics.DiskStats{
			IoServiceBytesRecursives: entries,
		}
	}

	return stats
}

func ptrSlice(u []uint64) []*uint64 {
	if len(u) == 0 {
		return nil
	}
	out := make([]*uint64, len(u))
	for i := range u {
		v := u[i]
		out[i] = &v
	}
	return out
}
