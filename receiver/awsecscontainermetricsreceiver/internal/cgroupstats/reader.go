// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package cgroupstats // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/cgroupstats"

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/awsecscontainermetrics"
)

type cgroupReader struct {
	mountPath      string
	clusterName    string
	prevCPU        map[string]uint64
	prevRead       map[string]time.Time
	mu             sync.Mutex
	logger         *zap.Logger
	cpuControllers []string
}

// NewReader creates a cgroup reader. mountPath is the cgroup mount root (e.g. /sys/fs/cgroup or /host/sys/fs/cgroup).
func NewReader(mountPath string, clusterName string, logger *zap.Logger) (Reader, error) {
	if mountPath == "" {
		return nil, fmt.Errorf("cgroup mount path is required")
	}
	if info, err := os.Stat(mountPath); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("cgroup mount path %s is not a directory: %w", mountPath, err)
	}
	cpuControllers := findCPUController(mountPath)
	if len(cpuControllers) == 0 {
		return nil, fmt.Errorf("could not find cpu cgroup controller under %s", mountPath)
	}
	if !hasMemoryController(mountPath) {
		return nil, fmt.Errorf("could not find memory cgroup controller under %s", mountPath)
	}
	return &cgroupReader{
		mountPath:      mountPath,
		clusterName:    clusterName,
		prevCPU:        make(map[string]uint64),
		prevRead:       make(map[string]time.Time),
		logger:         logger,
		cpuControllers: cpuControllers,
	}, nil
}

func findCPUController(mountPath string) []string {
	// Try common cgroup v1 paths for CPU
	candidates := []string{"cpu,cpuacct", "cpuacct", "cpu"}
	for _, c := range candidates {
		p := filepath.Join(mountPath, c)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return []string{c}
		}
	}
	return nil
}

func hasMemoryController(mountPath string) bool {
	p := filepath.Join(mountPath, "memory")
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func getTaskCgroupPath(basePath, controller, taskID, clusterName string) string {
	// ECS cgroup path: ecs/<taskID> or ecs/<clusterName>/<taskID> (legacy)
	path1 := filepath.Join(basePath, controller, "ecs", taskID)
	if _, err := os.Stat(path1); err == nil {
		return path1
	}
	path2 := filepath.Join(basePath, controller, "ecs", clusterName, taskID)
	if _, err := os.Stat(path2); err == nil {
		return path2
	}
	return path1
}

func getTaskIDFromARN(arn string) (string, error) {
	parts := strings.Split(arn, ":")
	if len(parts) < 6 {
		return "", fmt.Errorf("invalid task arn: %s", arn)
	}
	subParts := strings.Split(parts[5], "/")
	switch len(subParts) {
	case 2:
		return subParts[1], nil
	case 3:
		return subParts[2], nil
	default:
		return "", fmt.Errorf("invalid task arn: %s", arn)
	}
}

func readUint64(filePath string) (uint64, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, err
	}
	val, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, err
	}
	return val, nil
}

func readMemoryStat(filePath string) (map[string]uint64, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	result := make(map[string]uint64)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if v, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				result[fields[0]] = v
			}
		}
	}
	return result, nil
}

// GetContainerStats reads CPU and memory from cgroup and returns ContainerStats.
func (r *cgroupReader) GetContainerStats(taskARN, clusterName, dockerID, containerName string) (*awsecscontainermetrics.ContainerStats, error) {
	taskID, err := getTaskIDFromARN(taskARN)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	key := dockerID

	stats := &awsecscontainermetrics.ContainerStats{
		ID:   dockerID,
		Name: containerName,
		Read: now,
	}

	// CPU: read cpuacct.usage (nanoseconds)
	cpuBase := getTaskCgroupPath(r.mountPath, r.cpuControllers[0], taskID, clusterName)
	cpuPath := filepath.Join(cpuBase, dockerID)
	cpuUsagePath := filepath.Join(cpuPath, "cpuacct.usage")
	cpuUsage, err := readUint64(cpuUsagePath)
	if err != nil {
		r.logger.Debug("Could not read cpu usage", zap.String("path", cpuUsagePath), zap.Error(err))
	} else {
		r.mu.Lock()
		prevUsage := r.prevCPU[key]
		prevTime := r.prevRead[key]
		r.prevCPU[key] = cpuUsage
		r.prevRead[key] = now
		r.mu.Unlock()

		stats.CPU = &awsecscontainermetrics.CPUStats{
			CPUUsage: &awsecscontainermetrics.CPUUsage{
				TotalUsage: &cpuUsage,
			},
		}
		if !prevTime.IsZero() {
			stats.PreviousRead = prevTime
			stats.PreviousCPU = &awsecscontainermetrics.CPUStats{
				CPUUsage: &awsecscontainermetrics.CPUUsage{
					TotalUsage: &prevUsage,
				},
			}
		}
	}

	// Memory
	{
		memPath := getTaskCgroupPath(r.mountPath, "memory", taskID, clusterName)
		memPath = filepath.Join(memPath, dockerID)
		usagePath := filepath.Join(memPath, "memory.usage_in_bytes")
		if _, err := os.Stat(usagePath); err != nil {
			usagePath = filepath.Join(memPath, "memory.current")
		}
		usage, err := readUint64(usagePath)
		if err != nil {
			r.logger.Debug("Could not read memory usage", zap.String("path", usagePath), zap.Error(err))
		} else {
			memStats := &awsecscontainermetrics.MemoryStats{
				Usage: &usage,
			}
			maxPath := filepath.Join(memPath, "memory.max_usage_in_bytes")
			if maxUsage, err := readUint64(maxPath); err == nil {
				memStats.MaxUsage = &maxUsage
			}
			limitPath := filepath.Join(memPath, "memory.limit_in_bytes")
			if limit, err := readUint64(limitPath); err == nil && limit < 9223372036854771712 {
				memStats.Limit = &limit
			}
			statPath := filepath.Join(memPath, "memory.stat")
			if stat, err := readMemoryStat(statPath); err == nil {
				memStats.Stats = stat
			}
			stats.Memory = memStats
		}
	}

	if stats.CPU == nil && stats.Memory == nil {
		return nil, fmt.Errorf("no cgroup stats available for container %s", dockerID)
	}
	return stats, nil
}
