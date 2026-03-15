package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMessage(t *testing.T) {
	t.Run("creates envelope with serialized payload", func(t *testing.T) {
		payload := TestMessage{ID: "test-123"}
		msg, err := NewMessage(MessageTypeTest, payload)
		require.NoError(t, err)

		assert.Equal(t, MessageTypeTest, msg.Type)
		assert.NotEmpty(t, msg.Timestamp)
		assert.Contains(t, msg.JSON, `"id":"test-123"`)

		_, err = time.Parse(time.RFC3339, msg.Timestamp)
		require.NoError(t, err)
	})

	t.Run("returns error for unmarshalable payload", func(t *testing.T) {
		_, err := NewMessage("test", make(chan int))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to marshal payload")
	})
}

func TestMessageDecode(t *testing.T) {
	t.Run("decodes payload from JSON field", func(t *testing.T) {
		msg := &Message{
			Type: MessageTypeTest,
			JSON: `{"id":"decoded-456"}`,
		}

		var tm TestMessage
		err := msg.Decode(&tm)
		require.NoError(t, err)
		assert.Equal(t, "decoded-456", tm.ID)
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		msg := &Message{JSON: "not json"}
		var tm TestMessage
		err := msg.Decode(&tm)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode message payload")
	})
}

func TestMarshalUnmarshal(t *testing.T) {
	t.Run("round-trip preserves data", func(t *testing.T) {
		original, err := NewMessage(MessageTypeTest, TestMessage{ID: "round-trip"})
		require.NoError(t, err)

		data, err := Marshal(original)
		require.NoError(t, err)

		restored, err := UnmarshalMessage(data)
		require.NoError(t, err)

		assert.Equal(t, original.Type, restored.Type)
		assert.Equal(t, original.Timestamp, restored.Timestamp)
		assert.Equal(t, original.JSON, restored.JSON)

		var tm TestMessage
		err = restored.Decode(&tm)
		require.NoError(t, err)
		assert.Equal(t, "round-trip", tm.ID)
	})

	t.Run("marshal returns error for nil", func(t *testing.T) {
		data, err := Marshal(nil)
		require.NoError(t, err)
		assert.Equal(t, "null", string(data))
	})
}

func TestUnmarshalMessage(t *testing.T) {
	t.Run("returns error for invalid JSON", func(t *testing.T) {
		_, err := UnmarshalMessage([]byte("not json"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal message envelope")
	})

	t.Run("unmarshals valid envelope", func(t *testing.T) {
		raw := `{"type":"crossguard_test","timestamp":"2026-03-14T12:00:00Z","json":"{\"id\":\"abc\"}"}`
		msg, err := UnmarshalMessage([]byte(raw))
		require.NoError(t, err)
		assert.Equal(t, MessageTypeTest, msg.Type)
		assert.Equal(t, "2026-03-14T12:00:00Z", msg.Timestamp)

		var tm TestMessage
		require.NoError(t, msg.Decode(&tm))
		assert.Equal(t, "abc", tm.ID)
	})
}

func TestEnvelopeFormat(t *testing.T) {
	t.Run("envelope JSON has expected structure", func(t *testing.T) {
		msg, err := NewMessage(MessageTypeTest, TestMessage{ID: "struct-check"})
		require.NoError(t, err)

		data, err := Marshal(msg)
		require.NoError(t, err)

		var raw map[string]any
		require.NoError(t, json.Unmarshal(data, &raw))

		assert.Contains(t, raw, "type")
		assert.Contains(t, raw, "timestamp")
		assert.Contains(t, raw, "json")
		assert.Equal(t, MessageTypeTest, raw["type"])
		assert.IsType(t, "", raw["json"])
	})
}
