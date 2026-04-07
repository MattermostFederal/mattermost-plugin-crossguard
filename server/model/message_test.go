package model

import (
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarshalUnmarshalJSON(t *testing.T) {
	t.Run("round-trip test message", func(t *testing.T) {
		env := &Envelope{
			Type:        MessageTypeTest,
			Timestamp:   "2026-04-06T12:00:00Z",
			TestMessage: &TestMessage{ID: "round-trip"},
		}

		data, err := Marshal(env, FormatJSON)
		require.NoError(t, err)

		restored, err := Unmarshal(data, FormatJSON)
		require.NoError(t, err)

		assert.Equal(t, env.Type, restored.Type)
		assert.Equal(t, env.Timestamp, restored.Timestamp)
		require.NotNil(t, restored.TestMessage)
		assert.Equal(t, "round-trip", restored.TestMessage.ID)
		assert.Nil(t, restored.PostMessage)
		assert.Nil(t, restored.DeleteMessage)
		assert.Nil(t, restored.ReactionMessage)
	})

	t.Run("JSON has expected structure", func(t *testing.T) {
		env := &Envelope{
			Type:        MessageTypeTest,
			Timestamp:   "2026-04-06T12:00:00Z",
			TestMessage: &TestMessage{ID: "struct-check"},
		}

		data, err := Marshal(env, FormatJSON)
		require.NoError(t, err)

		var raw map[string]any
		require.NoError(t, json.Unmarshal(data, &raw))

		assert.Equal(t, MessageTypeTest, raw["type"])
		assert.Equal(t, "2026-04-06T12:00:00Z", raw["timestamp"])
		testMsg, ok := raw["test_message"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "struct-check", testMsg["id"])
		assert.NotContains(t, raw, "post_message")
		assert.NotContains(t, raw, "delete_message")
		assert.NotContains(t, raw, "reaction_message")
	})
}

func TestMarshalUnmarshalXML(t *testing.T) {
	t.Run("round-trip test message", func(t *testing.T) {
		env := &Envelope{
			Type:        MessageTypeTest,
			Timestamp:   "2026-04-06T12:00:00Z",
			TestMessage: &TestMessage{ID: "xml-trip"},
		}

		data, err := Marshal(env, FormatXML)
		require.NoError(t, err)

		assert.True(t, strings.HasPrefix(string(data), xml.Header))
		assert.Contains(t, string(data), "urn:mattermost-crossguard")

		restored, err := Unmarshal(data, FormatXML)
		require.NoError(t, err)

		assert.Equal(t, env.Type, restored.Type)
		assert.Equal(t, env.Timestamp, restored.Timestamp)
		require.NotNil(t, restored.TestMessage)
		assert.Equal(t, "xml-trip", restored.TestMessage.ID)
	})

	t.Run("round-trip post message", func(t *testing.T) {
		env := &Envelope{
			Type:      MessageTypePost,
			Timestamp: "2026-04-06T12:00:00Z",
			PostMessage: &PostMessage{
				PostID:      "p1",
				ChannelID:   "ch1",
				ChannelName: "town-square",
				TeamID:      "t1",
				TeamName:    "test",
				UserID:      "u1",
				Username:    "admin",
				MessageText: "hello",
				CreateAt:    1712404800000,
			},
		}

		data, err := Marshal(env, FormatXML)
		require.NoError(t, err)

		restored, err := Unmarshal(data, FormatXML)
		require.NoError(t, err)

		require.NotNil(t, restored.PostMessage)
		assert.Equal(t, "p1", restored.PostMessage.PostID)
		assert.Equal(t, "hello", restored.PostMessage.MessageText)
		assert.Equal(t, int64(1712404800000), restored.PostMessage.CreateAt)
	})
}

func TestDetectFormat(t *testing.T) {
	t.Run("JSON input", func(t *testing.T) {
		assert.Equal(t, FormatJSON, DetectFormat([]byte(`{"type":"test"}`)))
	})

	t.Run("XML input", func(t *testing.T) {
		assert.Equal(t, FormatXML, DetectFormat([]byte(`<?xml version="1.0"?><root/>`)))
	})

	t.Run("XML with leading whitespace", func(t *testing.T) {
		assert.Equal(t, FormatXML, DetectFormat([]byte("  \t\n<CrossguardMessage/>")))
	})

	t.Run("JSON with leading whitespace", func(t *testing.T) {
		assert.Equal(t, FormatJSON, DetectFormat([]byte("  \n{}")))
	})

	t.Run("empty input defaults to JSON", func(t *testing.T) {
		assert.Equal(t, FormatJSON, DetectFormat([]byte{}))
	})

	t.Run("whitespace only defaults to JSON", func(t *testing.T) {
		assert.Equal(t, FormatJSON, DetectFormat([]byte("   \t\n")))
	})

	t.Run("BOM-prefixed XML detected as XML", func(t *testing.T) {
		bom := []byte{0xEF, 0xBB, 0xBF}
		data := make([]byte, 0, len(bom)+len("<root/>"))
		data = append(data, bom...)
		data = append(data, []byte("<root/>")...)
		assert.Equal(t, FormatXML, DetectFormat(data))
	})

	t.Run("BOM-only defaults to JSON", func(t *testing.T) {
		bom := []byte{0xEF, 0xBB, 0xBF}
		assert.Equal(t, FormatJSON, DetectFormat(bom))
	})
}

func TestCrossFormatDetection(t *testing.T) {
	env := &Envelope{
		Type:        MessageTypeTest,
		Timestamp:   "2026-04-06T12:00:00Z",
		TestMessage: &TestMessage{ID: "detect"},
	}

	jsonData, err := Marshal(env, FormatJSON)
	require.NoError(t, err)
	assert.Equal(t, FormatJSON, DetectFormat(jsonData))

	xmlData, err := Marshal(env, FormatXML)
	require.NoError(t, err)
	assert.Equal(t, FormatXML, DetectFormat(xmlData))
}

func TestUnmarshalErrors(t *testing.T) {
	t.Run("invalid JSON", func(t *testing.T) {
		_, err := Unmarshal([]byte("not json"), FormatJSON)
		require.Error(t, err)
	})

	t.Run("invalid XML", func(t *testing.T) {
		_, err := Unmarshal([]byte("not xml"), FormatXML)
		require.Error(t, err)
	})

	t.Run("unsupported format in Marshal", func(t *testing.T) {
		env := &Envelope{Type: MessageTypeTest, Timestamp: "2026-04-06T12:00:00Z"}
		_, err := Marshal(env, Format("yaml"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported format")
	})

	t.Run("unsupported format in Unmarshal", func(t *testing.T) {
		_, err := Unmarshal([]byte(`{}`), Format("yaml"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported format")
	})
}

func TestEnvelopeNilPayload(t *testing.T) {
	t.Run("post type with nil payload round-trips", func(t *testing.T) {
		env := &Envelope{
			Type:      MessageTypePost,
			Timestamp: "2026-04-06T12:00:00Z",
		}

		for _, format := range []Format{FormatJSON, FormatXML} {
			t.Run(string(format), func(t *testing.T) {
				data, err := Marshal(env, format)
				require.NoError(t, err)

				restored, err := Unmarshal(data, format)
				require.NoError(t, err)
				assert.Equal(t, MessageTypePost, restored.Type)
				assert.Nil(t, restored.PostMessage)
			})
		}
	})

	t.Run("delete type with nil payload round-trips", func(t *testing.T) {
		env := &Envelope{
			Type:      MessageTypeDelete,
			Timestamp: "2026-04-06T12:00:00Z",
		}

		for _, format := range []Format{FormatJSON, FormatXML} {
			t.Run(string(format), func(t *testing.T) {
				data, err := Marshal(env, format)
				require.NoError(t, err)

				restored, err := Unmarshal(data, format)
				require.NoError(t, err)
				assert.Equal(t, MessageTypeDelete, restored.Type)
				assert.Nil(t, restored.DeleteMessage)
			})
		}
	})

	t.Run("reaction type with nil payload round-trips", func(t *testing.T) {
		env := &Envelope{
			Type:      MessageTypeReactionAdd,
			Timestamp: "2026-04-06T12:00:00Z",
		}

		for _, format := range []Format{FormatJSON, FormatXML} {
			t.Run(string(format), func(t *testing.T) {
				data, err := Marshal(env, format)
				require.NoError(t, err)

				restored, err := Unmarshal(data, format)
				require.NoError(t, err)
				assert.Equal(t, MessageTypeReactionAdd, restored.Type)
				assert.Nil(t, restored.ReactionMessage)
			})
		}
	})
}
