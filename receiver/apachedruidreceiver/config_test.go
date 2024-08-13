package apachedruidreceiver

import (
	"testing"

	"github.com/tj/assert"
)

func TestDefaultCreateConfig(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()
	assert.NotNil(t, cfg, "failed to create default config")
}
