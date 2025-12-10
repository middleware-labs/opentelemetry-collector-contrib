// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package awsecscontainermetricsreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver"

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"

	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/aws/ecsutil"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/awsecscontainermetrics"
)

var _ receiver.Metrics = (*awsEcsContainerMetricsReceiver)(nil)

// awsEcsContainerMetricsReceiver implements the receiver.Metrics for aws ecs container metrics.
type awsEcsContainerMetricsReceiver struct {
	logger       *zap.Logger
	nextConsumer consumer.Metrics
	config       *Config
	cancel       context.CancelFunc
	restClient   ecsutil.RestClient
	provider     *awsecscontainermetrics.StatsProvider
	awsEcsClient *ecs.Client
}

// New creates the aws ecs container metrics receiver with the given parameters.
func newAWSECSContainermetrics(
	logger *zap.Logger,
	config *Config,
	nextConsumer consumer.Metrics,
	rest ecsutil.RestClient,
) (receiver.Metrics, error) {

	// // ECS API Client
	// ctx := context.TODO()
	// cfg, err := awsConfig.LoadDefaultConfig(ctx)
	// if err != nil {
	// 	logger.Error("Failed to load AWS configuration", zap.Error(err))
	// 	return nil, err
	// }
	// awsEcsClient := ecs.NewFromConfig(cfg)

	r := &awsEcsContainerMetricsReceiver{
		logger:       logger,
		nextConsumer: nextConsumer,
		config:       config,
		restClient:   rest,
		// awsEcsClient: awsEcsClient,
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

// collectDataFromEndpoint collects container stats from Amazon ECS Task Metadata Endpoint
func (aecmr *awsEcsContainerMetricsReceiver) collectDataFromEndpoint(ctx context.Context) error {
	aecmr.provider = awsecscontainermetrics.NewStatsProvider(aecmr.restClient, aecmr.logger)
	// stats, metadata, err := aecmr.provider.GetStats()
	// if err != nil {
	// 	aecmr.logger.Error("Failed to collect stats", zap.Error(err))
	// 	return err
	// }

	// ECS API Client
	ecsCtx := context.TODO()
	cfg, err := awsConfig.LoadDefaultConfig(ecsCtx)
	if err != nil {
		aecmr.logger.Error("Failed to load AWS configuration", zap.Error(err))
		return err
	}
	awsEcsClient := ecs.NewFromConfig(cfg)


	// Fetch cluster metadata from ECS API
	var clusterArns []string
	input := &ecs.ListClustersInput{}
	for {
		output, err := awsEcsClient.ListClusters(ctx, input)
		if err != nil {
			aecmr.logger.Warn("[awsecscontainermetrics] : failed to list clusters via ECS API", zap.Error(err))
			break
		}
		clusterArns = append(clusterArns, output.ClusterArns...)
		if output.NextToken == nil {
			break
		}
		input.NextToken = output.NextToken
	}

	// Loop through each cluster ARNs
	for _, clusterArn := range clusterArns {
		listTasks, err := awsEcsClient.ListTasks(ctx, &ecs.ListTasksInput{
			Cluster: &clusterArn,
		})
		if err != nil {
			aecmr.logger.Warn("[awsecscontainermetrics] : failed to list tasks via ECS API\n", zap.String("ClusterARN",clusterArn + "\n"), zap.Error(err))
		}
		listStoppedTasks, err := awsEcsClient.ListTasks(ctx, &ecs.ListTasksInput{
			Cluster: &clusterArn,
			DesiredStatus: "STOPPED",
		})
		if err != nil {
			aecmr.logger.Warn("[awsecscontainermetrics] : failed to list stopped tasks via ECS API\n", zap.String("ClusterARN",clusterArn + "\n"), zap.Error(err))
		}

		var activeTasks int64 = int64(len(listTasks.TaskArns))
		var stoppedTasks int64 = int64(len(listStoppedTasks.TaskArns))
		var totalTasks int64 = activeTasks + stoppedTasks
		
		if totalTasks == 0 {
			aecmr.logger.Warn("[awsecscontainermetrics] : No tasks found in cluster", zap.String("ClusterARN", clusterArn))
		} else {
			clusterTaskArns := append(listTasks.TaskArns, listStoppedTasks.TaskArns...)
			describeTasks, err := awsEcsClient.DescribeTasks(ctx, &ecs.DescribeTasksInput{
				Cluster: &clusterArn,
				Tasks:   clusterTaskArns,
			})
			if err != nil {
				aecmr.logger.Warn("[awsecscontainermetrics] : failed to describe tasks via ECS API\n", zap.String("ClusterARN", clusterArn + "\n"), zap.Error(err))
				continue
			}

			for _, task := range describeTasks.Tasks {
				taskJson, err := json.Marshal(task)
				if err != nil {
					aecmr.logger.Warn("[awsecscontainermetrics] : failed to marshal task metadata", zap.Error(err))
				}
				taskMetadata := ecsutil.TaskMetadata{}
				err = json.NewDecoder(bytes.NewReader(taskJson)).Decode(&taskMetadata)
				if err != nil {
					aecmr.logger.Warn("[awsecscontainermetrics] : failed to decode task metadata", zap.Error(err))
				}

				taskMetadata.ActiveTasks = activeTasks
				taskMetadata.StoppedTasks = stoppedTasks
				taskMetadata.TotalTasks = totalTasks
				
				// containers
				// task.Containers[0].NetworkInterfaces[0].Ipv6Address
				var taskIP *string
				for _, container := range task.Containers {
					taskIP = container.NetworkInterfaces[0].PrivateIpv4Address

					fmt.Printf("\n\nTask IP: %s\n\n", *taskIP)
	
					stats, metadata, err := aecmr.provider.GetStatsWithTaskIP(*taskIP)
					if err != nil {
						aecmr.logger.Error("Failed to collect stats", zap.Error(err))
						return err
					}
	
					for dockerID, containerStats := range stats {
						if containerStats == nil || containerStats.ID == "" {
							aecmr.logger.Warn("Skipping empty container stats", zap.String("DockerID", dockerID))
							continue
						}
						fmt.Printf("\n\nContainer Stats for Docker ID %s: %+v\n", dockerID, containerStats)
					}
	
					fmt.Printf("\n\n[Metadata]: %+v\n\n", metadata)
					
					// TODO: report self metrics using obsreport
					mds := awsecscontainermetrics.MetricsData(stats, metadata, aecmr.logger, aecmr.awsEcsClient)
					for _, md := range mds {
						err = aecmr.nextConsumer.ConsumeMetrics(ctx, md)
						if err != nil {
							return err
						}
					}
				}

			}
		}
	}
	return nil
}
