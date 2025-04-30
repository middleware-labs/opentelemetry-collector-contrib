// pkg/stanza/operator/parser/severitydetect/config_test.go
package severitydetect

import (
	"path/filepath"
	"testing"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/operator/operatortest"
)

func TestUnmarshal(t *testing.T) {
	operatortest.ConfigUnmarshalTests{
		DefaultConfig: NewConfig(),
		TestsFile:     filepath.Join(".", "testdata", "config.yaml"),
		Tests: []operatortest.ConfigUnmarshalTest{
			{
				Name:   "default",
				Expect: NewConfig(),
			},
			{
				Name: "on_error_drop",
				Expect: func() *Config {
					cfg := NewConfig()
					cfg.OnError = "drop"
					return cfg
				}(),
			},
			{
				Name: "parse_with_preset",
				Expect: func() *Config {
					cfg := NewConfig()
					cfg.Preset = "detection"
					return cfg
				}(),
			},
			{
				Name: "custom_mapping",
				Expect: func() *Config {
					cfg := NewConfig()
					cfg.Mapping = map[string]any{
						"error": []any{"exception", "failed"},
						"warn":  []any{"warning:", "attention"},
						"info":  []any{"success", "completed"},
					}
					return cfg
				}(),
			},
			{
				Name: "overwrite_text",
				Expect: func() *Config {
					cfg := NewConfig()
					cfg.OverwriteText = true
					cfg.Mapping = map[string]any{
						"error": "error occurred",
					}
					return cfg
				}(),
			},
		},
	}.Run(t)
}
