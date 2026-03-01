// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package awsecscontainermetricsreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver"

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/aws/ecsutil"
	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/aws/ecsutil/endpoints"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/ecsclient"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/metadata"
)

// Factory for awscontainermetrics
const (
	// Default collection interval. Every 20s the receiver will collect metrics from Amazon ECS Task Metadata Endpoint
	defaultCollectionInterval = 20 * time.Second
)

// NewFactory creates a factory for AWS ECS Container Metrics receiver.
func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		metadata.Type,
		createDefaultConfig,
		receiver.WithMetrics(createMetricsReceiver, metadata.MetricsStability))
}

// createDefaultConfig returns a default config for the receiver.
func createDefaultConfig() component.Config {
	return &Config{
		CollectionInterval: defaultCollectionInterval,
		RunAs:              RunAsSidecar,
	}
}

// CreateMetrics creates an AWS ECS Container Metrics receiver.
func createMetricsReceiver(
	ctx context.Context,
	params receiver.Settings,
	baseCfg component.Config,
	consumer consumer.Metrics,
) (receiver.Metrics, error) {
	rCfg := baseCfg.(*Config)
	logger := params.Logger

	switch rCfg.RunAs {
	case RunAsSidecar:
		endpoint, err := endpoints.GetTMEV4FromEnv()
		if err != nil || endpoint == nil {
			return nil, fmt.Errorf("unable to detect task metadata endpoint (run_as=sidecar requires ECS_CONTAINER_METADATA_URI_V4): %w", err)
		}
		clientSettings := confighttp.ClientConfig{}
		rest, err := ecsutil.NewRestClient(*endpoint, clientSettings, params.TelemetrySettings)
		if err != nil {
			return nil, err
		}
		return newAWSECSContainermetrics(logger, rCfg, consumer, rest, nil)
	case RunAsDaemonSet:
		if rCfg.Cluster == "" {
			return nil, fmt.Errorf("cluster is required when run_as is daemonset")
		}
		region := rCfg.Region
		if region == "" {
			region = getRegionFromEnv()
		}
		if region == "" {
			return nil, fmt.Errorf("region is required for run_as=daemonset (set region in config or AWS_REGION env)")
		}
		ecsClient, err := ecsclient.NewECSClient(ctx, region, params.Logger)
		if err != nil {
			return nil, err
		}
		return newAWSECSContainermetrics(logger, rCfg, consumer, nil, ecsClient)
	default:
		return nil, fmt.Errorf("invalid run_as %q, must be sidecar or daemonset", rCfg.RunAs)
	}
}

func getRegionFromEnv() string {
	if r := os.Getenv("AWS_REGION"); r != "" {
		return r
	}
	return os.Getenv("AWS_DEFAULT_REGION")
}
