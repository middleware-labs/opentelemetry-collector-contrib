package mwsnmpreceiver

import (
	"context"
	"fmt"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/sharedcomponent"
)

// componentType is the name used in a collector configuration.
//
// The receiver started as a copy of github.com/tsuga-dev/networkdevicereceiver
// (type "networkdevice"). It is renamed so it can sit in the same build as the
// upstream snmpreceiver without a clash.
var componentType = component.MustNewType("mwsnmp")

// Stability is alpha: the metric names depend on the hardware semantic
// conventions, which are still in Development and expected to change.
const stability = component.StabilityLevelAlpha

// receivers lets a metrics pipeline and a logs pipeline that use the same
// receiver config share one poller, so devices are not polled twice.
var receivers = sharedcomponent.NewSharedComponents()

// NewFactory returns the receiver factory.
func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		componentType,
		createDefaultConfig,
		receiver.WithMetrics(createMetricsReceiver, stability),
		receiver.WithLogs(createLogsReceiver, stability),
	)
}

func createMetricsReceiver(
	_ context.Context,
	set receiver.Settings,
	rawCfg component.Config,
	next consumer.Metrics,
) (receiver.Metrics, error) {
	shared, r, err := sharedReceiver(set, rawCfg)
	if err != nil {
		return nil, err
	}
	r.metricsConsumer = next
	return shared, nil
}

// createLogsReceiver enables the inventory flow: device, interface and IP
// address records sent as OpenTelemetry entity events (see inventory.go).
func createLogsReceiver(
	_ context.Context,
	set receiver.Settings,
	rawCfg component.Config,
	next consumer.Logs,
) (receiver.Logs, error) {
	shared, r, err := sharedReceiver(set, rawCfg)
	if err != nil {
		return nil, err
	}
	r.logsConsumer = next
	return shared, nil
}

func sharedReceiver(set receiver.Settings, rawCfg component.Config) (*sharedcomponent.SharedComponent, *snmpReceiver, error) {
	cfg, ok := rawCfg.(*Config)
	if !ok {
		return nil, nil, fmt.Errorf("expected a %s config, got %T", componentType, rawCfg)
	}
	var createErr error
	shared := receivers.GetOrAdd(cfg, func() component.Component {
		r, err := newReceiver(cfg, set, nil)
		if err != nil {
			createErr = err
			return nil
		}
		return r
	})
	if createErr != nil {
		return nil, nil, createErr
	}
	r, ok := shared.Unwrap().(*snmpReceiver)
	if !ok {
		return nil, nil, fmt.Errorf("%s receiver for this config failed to initialize", componentType)
	}
	return shared, r, nil
}
