package metadata

import (
	"go.opentelemetry.io/collector/component"
)

var (
	Type = component.MustNewType("apachedruid")
)

const (
	MetricStability = component.StabilityLevelAlpha
)