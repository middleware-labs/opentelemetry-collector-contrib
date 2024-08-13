package apachedruidreceiver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tj/assert"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

func TestCreateDefaultConfig(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()

	assert.NotNil(t, cfg, "failed to create default config")
	assert.NoError(t, componenttest.CheckConfigStruct(cfg))

	dcfg, ok := cfg.(*Config)
	assert.True(t, ok, "config is not of type *Config")
	assert.Equal(t, "localhost:8081", dcfg.ServerConfig.Endpoint)
	assert.Equal(t, 60*time.Second, dcfg.ReadTimeout)
}

func TestCreateMetricsReceiver(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()

	consumer := consumertest.NewNop()
	params := receivertest.NewNopCreateSettings()

	mr, err := factory.CreateMetricsReceiver(context.Background(), params, cfg, consumer)

	assert.NoError(t, err, "failed to create metrics receiver")
	assert.NotNil(t, mr, "metrics receiver is nil")
}

func TestCreateMetricsReceiverMultipleTimes(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()

	consumer := consumertest.NewNop()
	params := receivertest.NewNopCreateSettings()

	mr1, err := factory.CreateMetricsReceiver(context.Background(), params, cfg, consumer)
	require.NoError(t, err, "failed to create metrics receiver")
	require.NotNil(t, mr1, "metrics receiver is nil")

	mr2, err := factory.CreateMetricsReceiver(context.Background(), params, cfg, consumer)
	require.NoError(t, err, "failed to create metrics receiver")
	require.NotNil(t, mr2, "metrics receiver is nil")

	// The factory should return the same instance
	assert.Same(t, mr1, mr2, "factory did not return the same instance")
}
