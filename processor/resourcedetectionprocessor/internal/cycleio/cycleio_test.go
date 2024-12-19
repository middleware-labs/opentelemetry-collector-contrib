// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package cycleio

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/processor"
	semconv "go.opentelemetry.io/collector/semconv/v1.5.0"
)

// TODO
func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want map[string]interface{}
	}{
		{
			name: "empty env",
			env:  map[string]string{},
			want: map[string]interface{}{
				semconv.AttributeCloudProvider: "unknown",
				semconv.AttributeCloudRegion:   "unknown",
				semconv.AttributeHostID:        "cycleio-server",
				semconv.AttributeHostName:      "cycleio-server",
				semconv.AttributeOSType:        semconv.AttributeOSTypeLinux,
				"cycle.cluster.id":             "unknown",
			},
		},
		{
			name: "only provider vendor",
			env: map[string]string{
				providerVendorEnvVar: "aws",
			},
			want: map[string]interface{}{
				semconv.AttributeCloudProvider: "aws",
				semconv.AttributeCloudRegion:   "unknown",
				semconv.AttributeHostID:        "cycleio-server",
				semconv.AttributeHostName:      "cycleio-server",
				semconv.AttributeOSType:        semconv.AttributeOSTypeLinux,
				"cycle.cluster.id":             "unknown",
			},
		},
		{
			name: "only provider location",
			env: map[string]string{
				providerVendorEnvVar: "aws",
				providerLocation:     "us-east-1",
			},
			want: map[string]interface{}{
				semconv.AttributeCloudProvider: "aws",
				semconv.AttributeCloudRegion:   "us-east-1",
				semconv.AttributeHostID:        "cycleio-server",
				semconv.AttributeHostName:      "cycleio-server",
				semconv.AttributeOSType:        semconv.AttributeOSTypeLinux,
				"cycle.cluster.id":             "unknown",
			},
		},
		{
			name: "only hostname",
			env: map[string]string{
				hostnameEnvVar: "acme-host",
			},
			want: map[string]interface{}{
				semconv.AttributeCloudProvider: "unknown",
				semconv.AttributeCloudRegion:   "unknown",
				semconv.AttributeHostID:        "acme-host",
				semconv.AttributeHostName:      "acme-host",
				semconv.AttributeOSType:        semconv.AttributeOSTypeLinux,
				"cycle.cluster.id":             "unknown",
			},
		},
		{
			name: "only cluster",
			env: map[string]string{
				clusterEnvVar: "acme-cluster",
			},
			want: map[string]interface{}{
				semconv.AttributeCloudProvider: "unknown",
				semconv.AttributeCloudRegion:   "unknown",
				semconv.AttributeHostID:        "cycleio-server",
				semconv.AttributeHostName:      "cycleio-server",
				semconv.AttributeOSType:        semconv.AttributeOSTypeLinux,
				"cycle.cluster.id":             "acme-cluster",
			},
		},
		{
			name: "all env vars",
			env: map[string]string{
				providerVendorEnvVar: "aws",
				providerLocation:     "us-east-1",
				hostnameEnvVar:       "acme-host",
				clusterEnvVar:        "acme-cluster",
			},
			want: map[string]interface{}{
				semconv.AttributeCloudProvider: "aws",
				semconv.AttributeCloudRegion:   "us-east-1",
				semconv.AttributeHostID:        "acme-host",
				semconv.AttributeHostName:      "acme-host",
				semconv.AttributeOSType:        semconv.AttributeOSTypeLinux,
				"cycle.cluster.id":             "acme-cluster",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.env {
				os.Setenv(key, value)
			}
			defer func() {
				for key := range tt.env {
					os.Unsetenv(key)
				}
			}()

			d, err := NewDetector(processor.Settings{}, CreateDefaultConfig())
			require.NoError(t, err)
			got, _, err := d.Detect(context.Background())
			require.NoError(t, err)

			require.Equal(t, tt.want, got.Attributes().AsRaw())
		})
	}
}
