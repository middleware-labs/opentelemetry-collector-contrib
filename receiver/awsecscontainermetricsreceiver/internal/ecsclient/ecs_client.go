// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ecsclient // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/ecsclient"

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"go.uber.org/zap"
)

const (
	describeTasksLimit      = 100
	describeServicesLimit   = 10
	deploymentStatusActive  = "ACTIVE"
	deploymentStatusPrimary = "PRIMARY"
)

// ECSClient provides ECS API operations for daemonset mode.
type ECSClient interface {
	// ListAndDescribeTasks returns all running tasks in the cluster with full details.
	ListAndDescribeTasks(ctx context.Context, cluster string) ([]ecstypes.Task, error)
	// DescribeTasks returns full details for the given task ARNs.
	DescribeTasks(ctx context.Context, cluster string, taskARNs []string) ([]ecstypes.Task, error)
	// ListAndDescribeServices returns all services in the cluster with full details.
	ListAndDescribeServices(ctx context.Context, cluster string) ([]ecstypes.Service, error)
	// DescribeTaskDefinitions returns task definitions for the given ARNs.
	DescribeTaskDefinitions(ctx context.Context, taskDefARNs []string) (map[string]*ecstypes.TaskDefinition, error)
}

type ecsClientImpl struct {
	client *ecs.Client
	logger *zap.Logger
}

// NewECSClient creates an ECS API client for the given region.
func NewECSClient(ctx context.Context, region string, logger *zap.Logger) (ECSClient, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return &ecsClientImpl{
		client: ecs.NewFromConfig(cfg),
		logger: logger,
	}, nil
}

func (c *ecsClientImpl) ListAndDescribeTasks(ctx context.Context, cluster string) ([]ecstypes.Task, error) {
	clusterPtr := aws.String(cluster)
	var allTasks []ecstypes.Task
	var nextToken *string

	for {
		listOut, err := c.client.ListTasks(ctx, &ecs.ListTasksInput{
			Cluster:   clusterPtr,
			NextToken: nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("ecs.ListTasks failed: %w", err)
		}

		if len(listOut.TaskArns) == 0 {
			if nextToken == nil {
				return allTasks, nil
			}
			nextToken = listOut.NextToken
			continue
		}

		// DescribeTasks accepts up to 100 task ARNs per call
		for i := 0; i < len(listOut.TaskArns); i += describeTasksLimit {
			end := i + describeTasksLimit
			if end > len(listOut.TaskArns) {
				end = len(listOut.TaskArns)
			}
			batch := listOut.TaskArns[i:end]

			descOut, err := c.client.DescribeTasks(ctx, &ecs.DescribeTasksInput{
				Cluster: clusterPtr,
				Tasks:   batch,
				Include: []ecstypes.TaskField{ecstypes.TaskFieldTags},
			})
			if err != nil {
				return nil, fmt.Errorf("ecs.DescribeTasks failed: %w", err)
			}

			// Log the response for debugging
			c.logger.Info("DescribeTasks response", zap.Any("response", descOut.Tasks))

			for _, t := range descOut.Tasks {
				for _, container := range t.Containers {
					c.logger.Info("container identity",
						zap.String("taskArn", aws.ToString(t.TaskArn)),
						zap.String("name", aws.ToString(container.Name)),
						zap.String("runtimeId", aws.ToString(container.RuntimeId)),
						zap.String("status", aws.ToString(container.LastStatus)),
					)
				}
			}

			for _, f := range descOut.Failures {
				reason := aws.ToString(f.Reason)
				if reason == "MISSING" {
					c.logger.Debug("DescribeTasks skipped task (no longer exists)",
						zap.String("arn", aws.ToString(f.Arn)))
				} else {
					c.logger.Warn("DescribeTasks partial failure",
						zap.String("arn", aws.ToString(f.Arn)),
						zap.String("reason", reason))
				}
			}
			allTasks = append(allTasks, descOut.Tasks...)
		}

		nextToken = listOut.NextToken
		if nextToken == nil {
			break
		}
	}

	return allTasks, nil
}

func (c *ecsClientImpl) DescribeTasks(ctx context.Context, cluster string, taskARNs []string) ([]ecstypes.Task, error) {
	if len(taskARNs) == 0 {
		return nil, nil
	}
	clusterPtr := aws.String(cluster)
	var allTasks []ecstypes.Task
	for i := 0; i < len(taskARNs); i += describeTasksLimit {
		end := i + describeTasksLimit
		if end > len(taskARNs) {
			end = len(taskARNs)
		}
		batch := taskARNs[i:end]
		descOut, err := c.client.DescribeTasks(ctx, &ecs.DescribeTasksInput{
			Cluster: clusterPtr,
			Tasks:   batch,
			Include: []ecstypes.TaskField{ecstypes.TaskFieldTags},
		})
		if err != nil {
			return nil, fmt.Errorf("ecs.DescribeTasks failed: %w", err)
		}
		for _, f := range descOut.Failures {
			reason := aws.ToString(f.Reason)
			if reason == "MISSING" {
				c.logger.Debug("DescribeTasks skipped task (no longer exists)",
					zap.String("arn", aws.ToString(f.Arn)))
			} else {
				c.logger.Warn("DescribeTasks partial failure",
					zap.String("arn", aws.ToString(f.Arn)),
					zap.String("reason", reason))
			}
		}
		allTasks = append(allTasks, descOut.Tasks...)
	}
	return allTasks, nil
}

func (c *ecsClientImpl) ListAndDescribeServices(ctx context.Context, cluster string) ([]ecstypes.Service, error) {
	clusterPtr := aws.String(cluster)
	var allServiceArns []string
	var nextToken *string

	for {
		listOut, err := c.client.ListServices(ctx, &ecs.ListServicesInput{
			Cluster:   clusterPtr,
			NextToken: nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("ecs.ListServices failed: %w", err)
		}
		allServiceArns = append(allServiceArns, listOut.ServiceArns...)
		nextToken = listOut.NextToken
		if nextToken == nil {
			break
		}
	}

	if len(allServiceArns) == 0 {
		return nil, nil
	}

	var allServices []ecstypes.Service
	for i := 0; i < len(allServiceArns); i += describeServicesLimit {
		end := i + describeServicesLimit
		if end > len(allServiceArns) {
			end = len(allServiceArns)
		}
		batch := allServiceArns[i:end]

		descOut, err := c.client.DescribeServices(ctx, &ecs.DescribeServicesInput{
			Cluster:  clusterPtr,
			Services: batch,
		})
		if err != nil {
			return nil, fmt.Errorf("ecs.DescribeServices failed: %w", err)
		}

		for _, f := range descOut.Failures {
			c.logger.Warn("DescribeServices partial failure",
				zap.String("arn", aws.ToString(f.Arn)),
				zap.String("reason", aws.ToString(f.Reason)))
		}
		allServices = append(allServices, descOut.Services...)
	}

	return allServices, nil
}

func (c *ecsClientImpl) DescribeTaskDefinitions(ctx context.Context, taskDefARNs []string) (map[string]*ecstypes.TaskDefinition, error) {
	result := make(map[string]*ecstypes.TaskDefinition)
	seen := make(map[string]struct{})

	for _, arn := range taskDefARNs {
		if arn != "" {
			seen[arn] = struct{}{}
		}
	}

	for arn := range seen {
		out, err := c.client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
			TaskDefinition: aws.String(arn),
		})
		if err != nil {
			return nil, fmt.Errorf("ecs.DescribeTaskDefinition failed for %s: %w", arn, err)
		}
		result[arn] = out.TaskDefinition
	}

	return result, nil
}

// GetServiceNameForTask returns the service name for a task based on StartedBy (deployment ID).
func GetServiceNameForTask(task *ecstypes.Task, services []ecstypes.Service) string {
	if task.StartedBy == nil {
		return ""
	}
	deploymentID := *task.StartedBy
	for i := range services {
		svc := &services[i]
		for j := range svc.Deployments {
			d := &svc.Deployments[j]
			status := aws.ToString(d.Status)
			if status == deploymentStatusActive || status == deploymentStatusPrimary {
				if aws.ToString(d.Id) == deploymentID {
					return aws.ToString(svc.ServiceName)
				}
				break
			}
		}
	}
	return ""
}

// GetClusterName extracts cluster name from ARN or returns as-is if already a name.
func GetClusterName(clusterARNOrName string) string {
	if clusterARNOrName == "" || !strings.HasPrefix(clusterARNOrName, "arn:aws") {
		return clusterARNOrName
	}
	parts := strings.Split(clusterARNOrName, "/")
	return parts[len(parts)-1]
}
