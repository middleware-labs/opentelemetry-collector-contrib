// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package awsecscontainermetricsreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver"

import (
	"context"
	"time"

	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/aws/ecsutil"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/awsecscontainermetrics"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/cgroupstats"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/dockerstats"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/ecsagent"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/ecsclient"
)

var _ receiver.Metrics = (*awsEcsContainerMetricsReceiver)(nil)

// awsEcsContainerMetricsReceiver implements the receiver.Metrics for aws ecs container metrics.
type awsEcsContainerMetricsReceiver struct {
	logger       *zap.Logger
	nextConsumer consumer.Metrics
	config       *Config
	cancel       context.CancelFunc
	restClient   ecsutil.RestClient
	ecsClient    ecsclient.ECSClient
	provider     *awsecscontainermetrics.StatsProvider
}

// newAWSECSContainermetrics creates the receiver. For sidecar mode, rest is non-nil and ecsClient is nil.
// For daemonset mode, rest is nil and ecsClient is non-nil.
func newAWSECSContainermetrics(
	logger *zap.Logger,
	config *Config,
	nextConsumer consumer.Metrics,
	rest ecsutil.RestClient,
	ecs ecsclient.ECSClient,
) (receiver.Metrics, error) {
	r := &awsEcsContainerMetricsReceiver{
		logger:       logger,
		nextConsumer: nextConsumer,
		config:       config,
		restClient:   rest,
		ecsClient:    ecs,
	}
	return r, nil
}

// Start begins collecting metrics from Amazon ECS task metadata endpoint.
func (aecmr *awsEcsContainerMetricsReceiver) Start(ctx context.Context, _ component.Host) error {
	ctx, aecmr.cancel = context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(aecmr.config.CollectionInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				_ = aecmr.collectDataFromEndpoint(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

// Shutdown stops the awsecscontainermetricsreceiver receiver.
func (aecmr *awsEcsContainerMetricsReceiver) Shutdown(context.Context) error {
	if aecmr.cancel != nil {
		aecmr.cancel()
	}
	return nil
}

// collectDataFromEndpoint collects metrics from Task Metadata API (sidecar) or ECS APIs (daemonset).
func (aecmr *awsEcsContainerMetricsReceiver) collectDataFromEndpoint(ctx context.Context) error {
	if aecmr.ecsClient != nil {
		return aecmr.collectDataFromECSAPI(ctx)
	}
	return aecmr.collectDataFromTaskMetadata(ctx)
}

// collectDataFromTaskMetadata collects from ECS Task Metadata API (sidecar mode).
func (aecmr *awsEcsContainerMetricsReceiver) collectDataFromTaskMetadata(ctx context.Context) error {
	aecmr.provider = awsecscontainermetrics.NewStatsProvider(aecmr.restClient, aecmr.logger)
	stats, metadata, err := aecmr.provider.GetStats()
	if err != nil {
		aecmr.logger.Error("Failed to collect stats from task metadata endpoint", zap.Error(err))
		return err
	}

	mds := awsecscontainermetrics.MetricsData(stats, metadata, aecmr.logger)
	for _, md := range mds {
		if err := aecmr.nextConsumer.ConsumeMetrics(ctx, md); err != nil {
			return err
		}
	}
	return nil
}

// collectDataFromECSAPI collects from ECS APIs (daemonset mode).
func (aecmr *awsEcsContainerMetricsReceiver) collectDataFromECSAPI(ctx context.Context) error {
	var tasks []ecstypes.Task
	var taskStats awsecscontainermetrics.TaskStatsMap

	if aecmr.config.CGroupsMountPath != "" || aecmr.config.DockerSocketPath != "" {
		tasks, taskStats = aecmr.collectFromInstanceWithCgroups(ctx)
	}
	if tasks == nil {
		var err error
		tasks, err = aecmr.ecsClient.ListAndDescribeTasks(ctx, aecmr.config.Cluster)
		if err != nil {
			aecmr.logger.Error("Failed to list/describe ECS tasks", zap.Error(err))
			return err
		}
	}

	services, err := aecmr.ecsClient.ListAndDescribeServices(ctx, aecmr.config.Cluster)
	if err != nil {
		aecmr.logger.Error("Failed to list/describe ECS services", zap.Error(err))
		return err
	}

	var taskDefARNs []string
	for i := range tasks {
		if tasks[i].TaskDefinitionArn != nil {
			taskDefARNs = append(taskDefARNs, *tasks[i].TaskDefinitionArn)
		}
	}
	taskDefs, err := aecmr.ecsClient.DescribeTaskDefinitions(ctx, taskDefARNs)
	if err != nil {
		aecmr.logger.Error("Failed to describe task definitions", zap.Error(err))
		return err
	}

	mds := awsecscontainermetrics.MetricsDataFromECSAPI(tasks, taskDefs, services, aecmr.config.Cluster, taskStats, aecmr.logger)
	for _, md := range mds {
		if err := aecmr.nextConsumer.ConsumeMetrics(ctx, md); err != nil {
			return err
		}
	}
	return nil
}

// collectFromInstanceWithCgroups uses ECS agent + Docker socket and/or cgroups for instance-local tasks with usage stats.
// When docker_socket_path is set, Docker API provides full stats (CPU, memory, network, disk).
// Otherwise cgroups provides CPU and memory only. Returns (tasks, taskStats) or (nil, nil) on failure/fallback.
func (aecmr *awsEcsContainerMetricsReceiver) collectFromInstanceWithCgroups(ctx context.Context) ([]ecstypes.Task, awsecscontainermetrics.TaskStatsMap) {
	ipProvider := ecsagent.NewEC2MetadataInstanceIPProvider()
	instanceIP, err := ipProvider.GetInstanceIP(ctx)
	if err != nil {
		aecmr.logger.Debug("Could not get instance IP for cgroup collection, using ECS API only", zap.Error(err))
		return nil, nil
	}
	aecmr.logger.Debug("Got instance IP for ECS agent", zap.String("instance_ip", instanceIP))
	agentClient := ecsagent.NewClient(aecmr.logger)
	metadata, err := agentClient.GetMetadata(ctx, instanceIP)
	if err != nil {
		aecmr.logger.Debug("Could not get ECS agent metadata, using ECS API only", zap.Error(err))
		return nil, nil
	}
	aecmr.logger.Debug("Got ECS agent metadata", zap.String("cluster", metadata.Cluster))
	ecsTasks, err := agentClient.GetTasks(ctx, instanceIP)
	if err != nil || len(ecsTasks) == 0 {
		aecmr.logger.Debug("Could not get ECS agent tasks or no tasks on instance", zap.Error(err), zap.Int("task_count", len(ecsTasks)))
		return nil, nil
	}
	totalContainers := 0
	for _, t := range ecsTasks {
		if t.KnownStatus == "RUNNING" {
			totalContainers += len(t.Containers)
		}
	}
	aecmr.logger.Debug("ECS agent tasks on instance", zap.Int("tasks", len(ecsTasks)), zap.Int("running_containers", totalContainers))
	clusterName := metadata.Cluster
	if clusterName == "" {
		clusterName = aecmr.config.Cluster
	}
	var taskStats awsecscontainermetrics.TaskStatsMap
	if aecmr.config.DockerSocketPath != "" {
		aecmr.logger.Debug("Fetching container stats from Docker", zap.String("docker_socket", aecmr.config.DockerSocketPath))
		taskStats = dockerstats.CollectTaskStats(ctx, aecmr.config.DockerSocketPath, ecsTasks, clusterName, aecmr.logger)
	}
	if taskStats == nil && aecmr.config.CGroupsMountPath != "" {
		aecmr.logger.Debug("Fetching container stats from cgroups", zap.String("cgroups_mount", aecmr.config.CGroupsMountPath))
		taskStats = cgroupstats.CollectTaskStats(ctx, aecmr.config.CGroupsMountPath, ecsTasks, clusterName, aecmr.logger)
	}
	if taskStats != nil {
		statsTasks := len(taskStats)
		statsContainers := 0
		for _, cm := range taskStats {
			statsContainers += len(cm)
		}
		aecmr.logger.Debug("Container stats collected", zap.Int("tasks_with_stats", statsTasks), zap.Int("containers_with_stats", statsContainers))
	} else {
		aecmr.logger.Debug("No container stats collected (usage metrics will be 0)", zap.String("docker_socket_path", aecmr.config.DockerSocketPath), zap.String("cgroups_mount_path", aecmr.config.CGroupsMountPath))
	}
	taskARNs := make([]string, len(ecsTasks))
	for i, t := range ecsTasks {
		taskARNs[i] = t.ARN
	}
	clusterForAPI := metadata.Cluster
	if clusterForAPI == "" {
		clusterForAPI = aecmr.config.Cluster
	}
	tasks, err := aecmr.ecsClient.DescribeTasks(ctx, clusterForAPI, taskARNs)
	if err != nil {
		aecmr.logger.Warn("DescribeTasks for instance-local tasks failed, falling back to full cluster", zap.Error(err))
		return nil, nil
	}
	return tasks, taskStats
}
