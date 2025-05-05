// pkg/stanza/operator/parser/severitydetect/parser.go
package severitydetect

import (
	"context"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/entry"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/operator/helper"
)

// Parser is an operator that parses severity by detecting patterns in the log body.
type Parser struct {
	helper.TransformerOperator
	helper.SeverityDetectionParser
}

// Process will parse severity from an entry's body.
func (p *Parser) Process(ctx context.Context, entry *entry.Entry) error {
	return p.ProcessWith(ctx, entry, p.Parse)
}
