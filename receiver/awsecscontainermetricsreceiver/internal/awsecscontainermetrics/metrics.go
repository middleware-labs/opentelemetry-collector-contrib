// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package awsecscontainermetrics // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awsecscontainermetricsreceiver/internal/awsecscontainermetrics"

import (
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"

	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/aws/ecsutil"
)

// MetricsData generates OTLP metrics from endpoint raw data
func MetricsData(containerStatsMap map[string]*ContainerStats, metadata ecsutil.TaskMetadata, logger *zap.Logger, awsEcsClient *ecs.Client) []pmetric.Metrics {
	acc := &metricDataAccumulator{}
	acc.getMetricsData(containerStatsMap, metadata, logger, awsEcsClient)

	return acc.mds
}
