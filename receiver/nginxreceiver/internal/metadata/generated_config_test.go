// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/confmap/confmaptest"
)

func TestMetricsBuilderConfig(t *testing.T) {
	tests := []struct {
		name string
		want MetricsBuilderConfig
	}{
		{
			name: "default",
			want: DefaultMetricsBuilderConfig(),
		},
		{
			name: "all_set",
			want: MetricsBuilderConfig{
				Metrics: MetricsConfig{
					NginxConnectionsAccepted:       MetricConfig{Enabled: true},
					NginxConnectionsCurrent:        MetricConfig{Enabled: true},
					NginxConnectionsHandled:        MetricConfig{Enabled: true},
					NginxLoadTimestamp:             MetricConfig{Enabled: true},
					NginxNetReading:                MetricConfig{Enabled: true},
					NginxNetWaiting:                MetricConfig{Enabled: true},
					NginxNetWriting:                MetricConfig{Enabled: true},
					NginxRequests:                  MetricConfig{Enabled: true},
					NginxServerZoneResponses1xx:    MetricConfig{Enabled: true},
					NginxServerZoneResponses2xx:    MetricConfig{Enabled: true},
					NginxServerZoneResponses3xx:    MetricConfig{Enabled: true},
					NginxServerZoneResponses4xx:    MetricConfig{Enabled: true},
					NginxServerZoneResponses5xx:    MetricConfig{Enabled: true},
					NginxUpstreamPeersResponseTime: MetricConfig{Enabled: true},
				},
			},
		},
		{
			name: "none_set",
			want: MetricsBuilderConfig{
				Metrics: MetricsConfig{
					NginxConnectionsAccepted:       MetricConfig{Enabled: false},
					NginxConnectionsCurrent:        MetricConfig{Enabled: false},
					NginxConnectionsHandled:        MetricConfig{Enabled: false},
					NginxLoadTimestamp:             MetricConfig{Enabled: false},
					NginxNetReading:                MetricConfig{Enabled: false},
					NginxNetWaiting:                MetricConfig{Enabled: false},
					NginxNetWriting:                MetricConfig{Enabled: false},
					NginxRequests:                  MetricConfig{Enabled: false},
					NginxServerZoneResponses1xx:    MetricConfig{Enabled: false},
					NginxServerZoneResponses2xx:    MetricConfig{Enabled: false},
					NginxServerZoneResponses3xx:    MetricConfig{Enabled: false},
					NginxServerZoneResponses4xx:    MetricConfig{Enabled: false},
					NginxServerZoneResponses5xx:    MetricConfig{Enabled: false},
					NginxUpstreamPeersResponseTime: MetricConfig{Enabled: false},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadMetricsBuilderConfig(t, tt.name)
			diff := cmp.Diff(tt.want, cfg, cmpopts.IgnoreUnexported(MetricConfig{}))
			require.Emptyf(t, diff, "Config mismatch (-expected +actual):\n%s", diff)
		})
	}
}

func loadMetricsBuilderConfig(t *testing.T, name string) MetricsBuilderConfig {
	cm, err := confmaptest.LoadConf(filepath.Join("testdata", "config.yaml"))
	require.NoError(t, err)
	sub, err := cm.Sub(name)
	require.NoError(t, err)
	cfg := DefaultMetricsBuilderConfig()
	require.NoError(t, sub.Unmarshal(&cfg))
	return cfg
}
