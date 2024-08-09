package apachedruidreceiver

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/sharedcomponent"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/apachedruidreceiver/internal/metadata"
)

// NewFactory creates a factory for DataDog receiver.
func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		metadata.Type,
		createDefaultConfig,
		receiver.WithMetrics(createMetricsReceiver, metadata.MetricStability))

}

func createDefaultConfig() component.Config {
	return &Config{
		ServerConfig: confighttp.ServerConfig{
			Endpoint: "localhost:8081",
		},
		ReadTimeout: 60 * time.Second,
	}
}

func createMetricsReceiver(_ context.Context, params receiver.CreateSettings, cfg component.Config, consumer consumer.Metrics) (r receiver.Metrics, err error) {
	rcfg := cfg.(*Config)
	r = receivers.GetOrAdd(cfg, func() component.Component {
		dd, _ := newApacheDruidMetricReceiver(rcfg, consumer, params)
		return dd
	})
	return r, nil
}

var receivers = sharedcomponent.NewSharedComponents()
