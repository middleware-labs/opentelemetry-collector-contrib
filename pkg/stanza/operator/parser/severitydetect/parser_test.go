// pkg/stanza/operator/parser/severitydetect/parser_test.go
package severitydetect

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/entry"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/operator"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/testutil"
)

func TestSeverityDetectionParser(t *testing.T) {
	testCases := []struct {
		name          string
		body          string
		mapping       map[string]any
		preset        string
		expected      entry.Severity
		expectedText  string
		overwriteText bool
	}{
		{
			name: "simple_error",
			body: "An error occurred in the system",
			mapping: map[string]any{
				"error": "error",
			},
			expected:     entry.Error,
			expectedText: "error",
		},
		{
			name: "case_insensitive",
			body: "ERROR: System failure",
			mapping: map[string]any{
				"error": "error",
			},
			expected:     entry.Error,
			expectedText: "error",
		},
		{
			name: "multiple_patterns",
			body: "Warning: Resource usage high",
			mapping: map[string]any{
				"error": []any{"error", "exception"},
				"warn":  []any{"warning", "attention"},
			},
			expected:     entry.Warn,
			expectedText: "warning",
		},
		{
			name:          "preset_detection",
			body:          "CRITICAL: Database connection lost",
			preset:        "detection",
			expected:      entry.Fatal3,
			expectedText:  "CRITICAL",
			overwriteText: true,
		},
		{
			name: "http_status",
			body: "Request failed with status 404",
			mapping: map[string]any{
				"error": "404",
			},
			expected:     entry.Error,
			expectedText: "404",
		},
		{
			name: "no_match",
			body: "Everything is running smoothly",
			mapping: map[string]any{
				"error": []any{"error", "failure"},
			},
			expected:     entry.Default,
			expectedText: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := NewConfigWithID("test")
			cfg.Mapping = tc.mapping
			cfg.Preset = tc.preset
			cfg.OverwriteText = tc.overwriteText

			set := componenttest.NewNopTelemetrySettings()
			op, err := cfg.Build(set)
			require.NoError(t, err)

			mockOutput := &testutil.Operator{}
			resultChan := make(chan *entry.Entry, 1)
			mockOutput.On("Process", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				resultChan <- args.Get(1).(*entry.Entry)
			}).Return(nil)

			parser := op.(*Parser)
			parser.OutputOperators = []operator.Operator{mockOutput}

			entry := entry.New()
			entry.Body = tc.body

			err = parser.Parse(entry)
			require.NoError(t, err)

			require.Equal(t, tc.expected, entry.Severity)
			if tc.expectedText != "" {
				if tc.overwriteText {
					require.Equal(t, tc.expected.String(), entry.SeverityText)
				} else {
					require.Equal(t, tc.expectedText, entry.SeverityText)
				}
			}
		})
	}
}
