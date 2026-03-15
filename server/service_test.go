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
		name, avail, errMsg := p.resolveConnectionName("", []string{"outbound-cgb"})
		assert.Equal(t, "outbound-cgb", name)
		assert.Equal(t, []string{"outbound-cgb"}, avail)
		assert.Empty(t, errMsg)
	})

	t.Run("ambiguous with multiple connections", func(t *testing.T) {
		name, avail, errMsg := p.resolveConnectionName("", []string{"outbound-cgb", "inbound-cgb"})
		assert.Empty(t, name)
		assert.Len(t, avail, 2)
		assert.Contains(t, errMsg, "multiple")
	})

	t.Run("explicit valid name", func(t *testing.T) {
		name, _, errMsg := p.resolveConnectionName("outbound-cgb", []string{"outbound-cgb", "inbound-cgb"})
		assert.Equal(t, "outbound-cgb", name)
		assert.Empty(t, errMsg)
	})

	t.Run("explicit invalid name", func(t *testing.T) {
		name, _, errMsg := p.resolveConnectionName("unknown", []string{"outbound-cgb"})
		assert.Empty(t, name)
		assert.Contains(t, errMsg, "not found")
	})

	t.Run("no connections configured", func(t *testing.T) {
		name, _, errMsg := p.resolveConnectionName("", nil)
		assert.Empty(t, name)
		assert.Contains(t, errMsg, "no NATS connections configured")
	})
}

func TestResolveLinkedConnectionName(t *testing.T) {
	t.Run("auto-select single linked", func(t *testing.T) {
		name, errMsg := resolveLinkedConnectionName("", []string{"outbound-cgb"})
		assert.Equal(t, "outbound-cgb", name)
		assert.Empty(t, errMsg)
	})

	t.Run("ambiguous with multiple linked", func(t *testing.T) {
		name, errMsg := resolveLinkedConnectionName("", []string{"outbound-cgb", "inbound-cgb"})
		assert.Empty(t, name)
		assert.Contains(t, errMsg, "multiple")
	})

	t.Run("explicit valid linked name", func(t *testing.T) {
		name, errMsg := resolveLinkedConnectionName("outbound-cgb", []string{"outbound-cgb", "inbound-cgb"})
		assert.Equal(t, "outbound-cgb", name)
		assert.Empty(t, errMsg)
	})

	t.Run("explicit invalid linked name", func(t *testing.T) {
		name, errMsg := resolveLinkedConnectionName("unknown", []string{"outbound-cgb"})
		assert.Empty(t, name)
		assert.Contains(t, errMsg, "not linked")
	})

	t.Run("no linked connections", func(t *testing.T) {
		name, errMsg := resolveLinkedConnectionName("", nil)
		assert.Empty(t, name)
		assert.Contains(t, errMsg, "no connections linked")
	})
}
