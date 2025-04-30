// pkg/stanza/operator/helper/severity_detect_test.go
package helper

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/entry"
)

type severityDetectionTestCase struct {
	name          string
	body          string
	mapping       map[string]any
	expected      entry.Severity
	expectedText  string
	overwriteText bool
	parseErr      bool
}

func TestSeverityDetectionParser(t *testing.T) {
	testCases := []severityDetectionTestCase{
		{
			name: "simple_error_match",
			body: "This is an error message",
			mapping: map[string]any{
				"error": "error",
			},
			expected:     entry.Error,
			expectedText: "error",
		},
		{
			name: "case_insensitive_match",
			body: "This is an ERROR message",
			mapping: map[string]any{
				"error": "error",
			},
			expected:     entry.Error,
			expectedText: "error",
		},
		{
			name: "overwrite_text",
			body: "This is an ERROR message",
			mapping: map[string]any{
				"error": "error",
			},
			expected:      entry.Error,
			expectedText:  "ERROR",
			overwriteText: true,
		},
		{
			name: "multiple_patterns_first_match",
			body: "Warning: This could be dangerous",
			mapping: map[string]any{
				"error":   "error",
				"warn":    "warning",
				"info":    "info",
				"debug":   "debug",
				"fatal":   "fatal",
				"panic":   "panic",
				"trace":   "trace",
				"verbose": "verbose",
			},
			expected:     entry.Warn,
			expectedText: "warn",
		},
		{
			name: "no_match",
			body: "This is a regular message",
			mapping: map[string]any{
				"error": "error",
				"warn":  "warning",
			},
			expected:     entry.Default,
			expectedText: "",
		},
		{
			name: "empty_body",
			body: "",
			mapping: map[string]any{
				"error": "error",
			},
			expected:     entry.Default,
			expectedText: "",
		},
		{
			name: "multiple_severity_words",
			body: "Error occurred: Fatal system crash",
			mapping: map[string]any{
				"error": "error",
				"fatal": "fatal",
			},
			expected:     entry.Error, // Should match first occurrence
			expectedText: "error",
		},
		{
			name: "partial_word_match",
			body: "Errorless operation completed",
			mapping: map[string]any{
				"error": "error",
			},
			expected:     entry.Error,
			expectedText: "error",
		},
		{
			name:     "nil_body",
			body:     "", // Will be set to nil in test
			mapping:  map[string]any{"error": "error"},
			parseErr: true,
		},
		{
			name: "custom_severity_levels",
			body: "CRITICAL: System failure",
			mapping: map[string]any{
				"error2":   "critical",
				"warning3": "warning",
			},
			expected:     entry.Error2,
			expectedText: "critical",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &SeverityConfig{
				Mapping:       tc.mapping,
				OverwriteText: tc.overwriteText,
			}

			set := componenttest.NewNopTelemetrySettings()
			parser, err := cfg.Build(set)
			require.NoError(t, err, "failed to build severity parser")

			ent := entry.New()
			if tc.body != "" {
				ent.Body = tc.body
			}

			err = parser.Parse(ent)

			if tc.parseErr {
				require.Error(t, err, "expected error when parsing")
				return
			}
			require.NoError(t, err)

			require.Equal(t, tc.expected, ent.Severity)
			if tc.overwriteText {
				require.Equal(t, tc.expectedText, ent.SeverityText)
			} else if tc.expected != entry.Default {
				require.Equal(t, tc.expectedText, ent.SeverityText)
			}
		})
	}
}

func TestSeverityDetectionWithInvalidBody(t *testing.T) {
	cfg := &SeverityConfig{
		Mapping: map[string]any{
			"error": "error",
		},
	}

	set := componenttest.NewNopTelemetrySettings()
	parser, err := cfg.Build(set)
	require.NoError(t, err, "failed to build severity parser")

	ent := entry.New()
	ent.Body = 123 // Non-string body

	err = parser.Parse(ent)
	require.Error(t, err)
	require.Contains(t, err.Error(), "log entry body is not a string")
}

func TestSeverityDetectionWithComplexMapping(t *testing.T) {
	complexMapping := map[string]any{
		"fatal4": []any{"critical", "panic", "fatal"},
		"error3": []any{"error", "failed", "failure"},
		"warn2":  []any{"warning", "warn"},
		"info":   []any{"info", "notice"},
		"debug":  []any{"debug", "trace"},
	}

	testCases := []struct {
		name     string
		body     string
		expected entry.Severity
	}{
		{
			name:     "match_fatal",
			body:     "PANIC: System is down",
			expected: entry.Fatal4,
		},
		{
			name:     "match_error",
			body:     "Operation FAILED",
			expected: entry.Error3,
		},
		{
			name:     "match_warning",
			body:     "Warning: Resource usage high",
			expected: entry.Warn2,
		},
		{
			name:     "match_info",
			body:     "Notice: Backup completed",
			expected: entry.Info,
		},
		{
			name:     "match_debug",
			body:     "Debug: Connection attempt",
			expected: entry.Debug,
		},
		{
			name:     "no_match",
			body:     "Regular message",
			expected: entry.Default,
		},
	}

	cfg := &SeverityConfig{
		Mapping: complexMapping,
	}

	set := componenttest.NewNopTelemetrySettings()
	parser, err := cfg.Build(set)
	require.NoError(t, err, "failed to build severity parser")

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ent := entry.New()
			ent.Body = tc.body

			err = parser.Parse(ent)
			require.NoError(t, err)
			require.Equal(t, tc.expected, ent.Severity)
		})
	}
}
