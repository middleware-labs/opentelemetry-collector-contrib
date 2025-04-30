// pkg/stanza/operator/parser/severity_detect/README.md
# Severity Detection Parser Operator

The `severity_detect_parser` operator parses severity levels from log messages by scanning the body content for known patterns.

## Configuration Fields

| Field         | Default          | Description |
|---------------|------------------|-------------|
| `id`          | `severity_detect_parser` | A unique identifier for the operator. |
| `type`        | `severity_detect_parser` | The operator type. |
| `output`      | Next operator    | The connected operator(s) that will receive all outbound entries. |
| `on_error`    | `send`          | The behavior of the operator if it encounters an error. See [on_error](../../types/on_error.md). |
| `preset`      | `""`            | A predefined set of severity mappings. Available options: "detection". |
| `mapping`     | `{}`            | A map of severity levels to pattern lists. |
| `overwrite_text` | `false`      | If true, overwrites the severity text with the standard severity level string. |

## Examples

### Basic Configuration
```yaml
- type: severity_detect_parser
  preset: detection
```

### Custom Pattern Mapping
```yaml
- type: severity_detect_parser
  mapping:
    error:
      - "exception"
      - "failed"
      - "error occurred"
    warn:
      - "warning:"
      - "attention required"
    info:
      - "success"
      - "completed"
  overwrite_text: true
```
```

4. Add example configuration to the filelog receiver's examples:

```yaml
# examples/filelog/config.yaml
receivers:
  filelog:
    operators:
      - type: severity_detect_parser
        preset: detection
```

5. Add tests for the filelog receiver integration:

```go
// pkg/stanza/operator/parser/severity_detect/integration_test.go
package severity_detect

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/pdata/plog"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/entry"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/operator"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/testutil"
)

func TestSeverityDetectIntegration(t *testing.T) {
	cases := []struct {
		name           string
		config         *Config
		inputLogs      []string
		expectedLevels []plog.SeverityNumber
	}{
		{
			name: "DefaultPreset",
			config: func() *Config {
				cfg := NewConfig()
				cfg.Preset = "detection"
				return cfg
			}(),
			inputLogs: []string{
				"ERROR: System failure",
				"WARNING: Disk space low",
				"INFO: Operation completed",
			},
			expectedLevels: []plog.SeverityNumber{
				plog.SeverityNumberError,
				plog.SeverityNumberWarn,
				plog.SeverityNumberInfo,
			},
		},
		{
			name: "CustomMapping",
			config: func() *Config {
				cfg := NewConfig()
				cfg.Mapping = map[string]any{
					"error": []any{"failure", "exception"},
					"warn":  "attention",
				}
				return cfg
			}(),
			inputLogs: []string{
				"System failure detected",
				"Attention required",
				"Normal operation",
			},
			expectedLevels: []plog.SeverityNumber{
				plog.SeverityNumberError,
				plog.SeverityNumberWarn,
				plog.SeverityNumberUnspecified,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op, err := tc.config.Build(componenttest.NewNopTelemetrySettings())
			require.NoError(t, err)

			mockOutput := testutil.NewMockOperator("mock")
			op.SetOutputOperators([]operator.Operator{mockOutput})

			for i, logLine := range tc.inputLogs {
				entry := entry.New()
				entry.Timestamp = time.Now()
				entry.Body = logLine

				err = op.Process(context.Background(), entry)
				require.NoError(t, err)

				logs := mockOutput.Received()
				require.Equal(t, tc.expectedLevels[i], mapSeverityToPlog(logs[i].Severity))
			}
		})
	}
}

func mapSeverityToPlog(sev entry.Severity) plog.SeverityNumber {
	switch sev {
	case entry.Fatal, entry.Fatal2, entry.Fatal3, entry.Fatal4:
		return plog.SeverityNumberFatal
	case entry.Error, entry.Error2, entry.Error3, entry.Error4:
		return plog.SeverityNumberError
	case entry.Warn, entry.Warn2, entry.Warn3, entry.Warn4:
		return plog.SeverityNumberWarn
	case entry.Info, entry.Info2, entry.Info3, entry.Info4:
		return plog.SeverityNumberInfo
	case entry.Debug, entry.Debug2, entry.Debug3, entry.Debug4:
		return plog.SeverityNumberDebug
	case entry.Trace, entry.Trace2, entry.Trace3, entry.Trace4:
		return plog.SeverityNumberTrace
	default:
		return plog.SeverityNumberUnspecified
	}
}
```

6. Update the module's `go.mod` to ensure all dependencies are properly included:

```go
// go.mod
require (
    // ... existing requirements ...
    go.opentelemetry.io/collector/pdata v1.0.0
    // ... other requirements ...
)
```

These changes will:
1. Properly integrate the severity detection parser with the filelog receiver
2. Provide documentation and examples for users
3. Ensure the operator is properly registered and available
4. Add integration tests to verify the operator works with the OpenTelemetry logging pipeline
5. Map severity levels correctly to OpenTelemetry's severity numbers

The key aspects we've addressed are:
- Proper package naming and organization
- Registration with the operator system
- Documentation and examples
- Integration testing
- Severity level mapping to OpenTelemetry format
- Configuration compatibility

Would you like me to explain any of these changes in more detail?