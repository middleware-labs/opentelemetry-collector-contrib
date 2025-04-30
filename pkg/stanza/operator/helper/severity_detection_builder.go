// pkg/stanza/operator/helper/severity_detection_builder.go
package helper

import (
	"fmt"
	"strings"

	"go.opentelemetry.io/collector/component"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/entry"
)

// NewSeverityDetectionConfig creates a new severity detection parser config
func NewSeverityDetectionConfig() SeverityDetectionConfig {
	return SeverityDetectionConfig{}
}

// SeverityDetectionConfig allows users to specify how to detect severity from log body content
type SeverityDetectionConfig struct {
	Preset        string         `mapstructure:"preset,omitempty"`
	Mapping       map[string]any `mapstructure:"mapping,omitempty"`
	OverwriteText bool           `mapstructure:"overwrite_text,omitempty"`
}

// Build builds a SeverityDetectionParser from a SeverityDetectionConfig
func (c *SeverityDetectionConfig) Build(_ component.TelemetrySettings) (SeverityDetectionParser, error) {
	operatorMapping := getSeverityDetectionMapping(c.Preset)

	if len(c.Mapping) > 0 {
		for severity, unknown := range c.Mapping {
			sev, err := validateSeverity(severity)
			if err != nil {
				return SeverityDetectionParser{}, err
			}

			switch u := unknown.(type) {
			case []any: // handle array of patterns
				for _, value := range u {
					patterns, err := parseDetectionPatterns(value)
					if err != nil {
						return SeverityDetectionParser{}, err
					}
					for _, pattern := range patterns {
						operatorMapping.add(sev, pattern)
					}
				}
			case any:
				patterns, err := parseDetectionPatterns(u)
				if err != nil {
					return SeverityDetectionParser{}, err
				}
				for _, pattern := range patterns {
					operatorMapping.add(sev, pattern)
				}
			}
		}

		if c.Preset == "default" {
			defaultMapping := getSeverityDetectionMapping("default")
			for k, v := range defaultMapping {
				operatorMapping.add(v, k)
			}
		}
	} else {
		operatorMapping = getSeverityDetectionMapping("default")
	}

	p := SeverityDetectionParser{
		Mapping:       operatorMapping,
		overwriteText: c.OverwriteText,
	}

	return p, nil
}

// parseDetectionPatterns converts various input types into string patterns for matching
func parseDetectionPatterns(value any) ([]string, error) {
	switch v := value.(type) {
	case string:
		// Handle HTTP status code ranges
		return []string{strings.ToLower(v)}, nil

	case []byte:
		return []string{strings.ToLower(string(v))}, nil
	case int:
		return []string{fmt.Sprintf("%d", v)}, nil
	case float64:
		if v != float64(int(v)) {
			return nil, fmt.Errorf("floating-point numbers must be whole numbers, got %f", v)
		}
		return []string{fmt.Sprintf("%d", int(v))}, nil
	default:
		return nil, fmt.Errorf("type %T cannot be used as a detection pattern", v)
	}
}

// getDefaultDetectionMapping returns a set of common patterns for severity detection
func getDefaultDetectionMapping() severityMap {
	return severityMap{
		"panic":     entry.Fatal,
		"fatal":     entry.Fatal,
		"emerg":     entry.Fatal,
		"emergency": entry.Fatal,
		"alert":     entry.Fatal,
		"critical":  entry.Fatal,
		"crit":      entry.Fatal,
		"error":     entry.Error,
		"err":       entry.Error,
		"warning":   entry.Warn,
		"warn":      entry.Warn,
		"notice":    entry.Info,
		"info":      entry.Info,
		"debug":     entry.Debug,
		"trace":     entry.Trace,
	}
}

func getSeverityDetectionMapping(preset string) severityMap {

	switch preset {
	case "default":
		return getDefaultDetectionMapping()
	default:
		return severityMap{}
	}
}
