package helper

// pkg/stanza/operator/helper/severity_detection.go
import (
	"fmt"
	"regexp"
	"strings"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/entry"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/errors"
)

// SeverityDetectionParser is a helper that detects severity by analyzing the log body content
type SeverityDetectionParser struct {
	Mapping                severityMap
	overwriteText          bool
	multiMatchHighSeverity bool
}

// Parse will analyze the log body for severity indicators and set the entry severity accordingly
func (p *SeverityDetectionParser) Parse(ent *entry.Entry) error {
	if ent.Body == nil {
		return errors.NewError(
			"log entry does not have a body field",
			"ensure that all entries forwarded to this parser contain a body field",
		)
	}

	bodyStr, ok := ent.Body.(string)
	if !ok {
		return errors.NewError(
			"log entry body is not a string",
			"ensure that the body field contains string content",
		)
	}

	if ent.SeverityText != "" {
		ent.SeverityText = strings.ToUpper(ent.SeverityText)
		return nil
	}

	// If the severity is already set, don't override it
	if ent.Severity != entry.Default {
		return nil
	}

	// Convert body to lowercase for case-insensitive matching
	bodyLower := strings.ToLower(bodyStr)

	// Find the first matching severity from the mapping
	matchedSeverity := entry.Default
	var matchedText string

	// Check each pattern in the mapping
	for pattern, sev := range p.Mapping {
		// Create a regex pattern that matches the word with word boundaries
		wordPattern := fmt.Sprintf(`\b%s\b`, regexp.QuoteMeta(strings.ToLower(pattern)))
		matched, err := regexp.MatchString(wordPattern, bodyLower)
		if err != nil {
			return errors.NewError(
				"failed to compile regex pattern",
				"check the severity mapping patterns",
				"pattern", pattern,
				"error", err.Error(),
			)
		}
		if matched {
			if p.multiMatchHighSeverity {
				if sev > matchedSeverity {
					matchedSeverity = sev
					matchedText = pattern
				}
				continue
			}

			matchedSeverity = sev
			matchedText = pattern
			break

		}
	}

	if matchedSeverity == entry.Default {
		matchedSeverity = entry.Info
		matchedText = "INFO"
	}

	ent.Severity = matchedSeverity
	if p.overwriteText {
		ent.SeverityText = matchedSeverity.String()
	} else {
		ent.SeverityText = matchedText
	}

	return nil
}
