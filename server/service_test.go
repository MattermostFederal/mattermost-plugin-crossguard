package main

import (
	"testing"

	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
)

func TestResolveConnectionName(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	t.Run("auto-select single connection", func(t *testing.T) {
		name, avail, errMsg := p.resolveConnectionName("", []string{"outbound:high"})
		assert.Equal(t, "outbound:high", name)
		assert.Equal(t, []string{"outbound:high"}, avail)
		assert.Empty(t, errMsg)
	})

	t.Run("ambiguous with multiple connections", func(t *testing.T) {
		name, avail, errMsg := p.resolveConnectionName("", []string{"outbound:high", "inbound:high"})
		assert.Empty(t, name)
		assert.Len(t, avail, 2)
		assert.Contains(t, errMsg, "multiple")
	})

	t.Run("explicit valid name", func(t *testing.T) {
		name, _, errMsg := p.resolveConnectionName("outbound:high", []string{"outbound:high", "inbound:high"})
		assert.Equal(t, "outbound:high", name)
		assert.Empty(t, errMsg)
	})

	t.Run("explicit invalid name", func(t *testing.T) {
		name, _, errMsg := p.resolveConnectionName("unknown", []string{"outbound:high"})
		assert.Empty(t, name)
		assert.Contains(t, errMsg, "not found")
	})

	t.Run("no connections configured", func(t *testing.T) {
		name, _, errMsg := p.resolveConnectionName("", nil)
		assert.Empty(t, name)
		assert.Contains(t, errMsg, "no NATS connections configured")
	})
}
