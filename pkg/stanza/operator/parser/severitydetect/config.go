// pkg/stanza/operator/parser/severitydetect/config.go
package severitydetect

import (
	"go.opentelemetry.io/collector/component"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/operator"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/operator/helper"
)

const operatorType = "severity_detection_parser"

func init() {
	operator.Register(operatorType, func() operator.Builder { return NewConfig() })
}

// NewConfig creates a new severity detection parser config with default values
func NewConfig() *Config {
	return NewConfigWithID(operatorType)
}

// NewConfigWithID creates a new severity detection parser config with default values
func NewConfigWithID(operatorID string) *Config {
	return &Config{
		TransformerConfig:       helper.NewTransformerConfig(operatorID, operatorType),
		SeverityDetectionConfig: helper.NewSeverityDetectionConfig(),
	}
}

// Config is the configuration of a severity detection parser operator.
type Config struct {
	helper.TransformerConfig       `mapstructure:",squash"`
	helper.SeverityDetectionConfig `mapstructure:",omitempty,squash"`
}

// Build will build a severity detection parser operator.
func (c Config) Build(set component.TelemetrySettings) (operator.Operator, error) {
	transformerOperator, err := c.TransformerConfig.Build(set)
	if err != nil {
		return nil, err
	}

	severityParser, err := c.SeverityDetectionConfig.Build(set)
	if err != nil {
		return nil, err
	}

	return &Parser{
		TransformerOperator:     transformerOperator,
		SeverityDetectionParser: severityParser,
	}, nil
}
