// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package awscontainerinsightreceiver

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap/confmaptest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awscontainerinsightreceiver/internal/metadata"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id       component.ID
		expected component.Config
	}{
		{
			id: component.NewIDWithName(metadata.Type, ""),
			expected: &Config{
				CollectionInterval:        60 * time.Second,
				ContainerOrchestrator:     "eks",
				TagService:                true,
				PrefFullPodName:           false,
				AddFullPodNameMetricLabel: false,
			},
		},
		{
			id: component.NewIDWithName(metadata.Type, "customname"),
			expected: &Config{
				CollectionInterval:        10 * time.Second,
				ContainerOrchestrator:     "ecs",
				TagService:                false,
				PrefFullPodName:           true,
				AddFullPodNameMetricLabel: true,
			},
		},
		{
			id: component.NewIDWithName(metadata.Type, "role_delegation"),
			expected: &Config{
				CollectionInterval:        60 * time.Second,
				ContainerOrchestrator:     "eks",
				TagService:                true,
				PrefFullPodName:           false,
				AddFullPodNameMetricLabel: false,
				Region:                    "us-west-2",
				RoleARN:                   "arn:aws:iam::123456789012:role/CrossAccountRole",
				ExternalID:                "unique-id-for-secondary",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.id.String(), func(t *testing.T) {
			cm, err := confmaptest.LoadConf(filepath.Join("testdata", "config.yaml"))
			require.NoError(t, err)

			factory := NewFactory()
			cfg := factory.CreateDefaultConfig()

			sub, err := cm.Sub(tt.id.String())
			require.NoError(t, err)
			require.NoError(t, sub.Unmarshal(cfg))

			assert.NoError(t, component.ValidateConfig(cfg))
			assert.Equal(t, tt.expected, cfg)
		})
	}
}
