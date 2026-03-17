package main

import (
	"testing"

	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

func TestResolveConnectionName(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	t.Run("auto-select single connection", func(t *testing.T) {
		conn, avail, errMsg := p.resolveConnectionName("", []store.TeamConnection{
			{Direction: "outbound", Connection: "high"},
		})
		assert.Equal(t, "outbound:high", connKey(conn))
		assert.Equal(t, []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, avail)
		assert.Empty(t, errMsg)
	})

	t.Run("ambiguous with multiple connections", func(t *testing.T) {
		conn, avail, errMsg := p.resolveConnectionName("", []store.TeamConnection{
			{Direction: "outbound", Connection: "high"},
			{Direction: "inbound", Connection: "high"},
		})
		assert.Equal(t, store.TeamConnection{}, conn)
		assert.Len(t, avail, 2)
		assert.Contains(t, errMsg, "multiple")
	})

	t.Run("explicit valid name", func(t *testing.T) {
		conn, _, errMsg := p.resolveConnectionName("outbound:high", []store.TeamConnection{
			{Direction: "outbound", Connection: "high"},
			{Direction: "inbound", Connection: "high"},
		})
		assert.Equal(t, "outbound:high", connKey(conn))
		assert.Empty(t, errMsg)
	})

	t.Run("explicit invalid name", func(t *testing.T) {
		conn, _, errMsg := p.resolveConnectionName("unknown", []store.TeamConnection{
			{Direction: "outbound", Connection: "high"},
		})
		assert.Equal(t, store.TeamConnection{}, conn)
		assert.Contains(t, errMsg, "not found")
	})

	t.Run("no connections configured", func(t *testing.T) {
		conn, _, errMsg := p.resolveConnectionName("", nil)
		assert.Equal(t, store.TeamConnection{}, conn)
		assert.Contains(t, errMsg, "no NATS connections configured")
	})
}
