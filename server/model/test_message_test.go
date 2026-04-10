package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTestMessage_JSONRoundTrip(t *testing.T) {
	env := &Envelope{
		Type:        MessageTypeTest,
		Timestamp:   "2026-04-08T10:00:00Z",
		TestMessage: &TestMessage{ID: "abc123"},
	}

	data, err := Marshal(env, FormatJSON)
	require.NoError(t, err)

	restored, err := Unmarshal(data, FormatJSON)
	require.NoError(t, err)

	assert.Equal(t, MessageTypeTest, restored.Type)
	assert.Equal(t, env.Timestamp, restored.Timestamp)
	require.NotNil(t, restored.TestMessage)
	assert.Equal(t, "abc123", restored.TestMessage.ID)
	assert.Nil(t, restored.PostMessage)
	assert.Nil(t, restored.DeleteMessage)
	assert.Nil(t, restored.ReactionMessage)
}

func TestTestMessage_XMLRoundTrip(t *testing.T) {
	env := &Envelope{
		Type:        MessageTypeTest,
		Timestamp:   "2026-04-08T10:00:00Z",
		TestMessage: &TestMessage{ID: "abc123"},
	}

	data, err := Marshal(env, FormatXML)
	require.NoError(t, err)

	restored, err := Unmarshal(data, FormatXML)
	require.NoError(t, err)

	assert.Equal(t, MessageTypeTest, restored.Type)
	assert.Equal(t, env.Timestamp, restored.Timestamp)
	require.NotNil(t, restored.TestMessage)
	assert.Equal(t, "abc123", restored.TestMessage.ID)
	assert.Nil(t, restored.PostMessage)
	assert.Nil(t, restored.DeleteMessage)
	assert.Nil(t, restored.ReactionMessage)
}

func TestTestMessage_EmptyID(t *testing.T) {
	env := &Envelope{
		Type:        MessageTypeTest,
		Timestamp:   "2026-04-08T10:00:00Z",
		TestMessage: &TestMessage{ID: ""},
	}

	for _, format := range []Format{FormatJSON, FormatXML} {
		t.Run(string(format), func(t *testing.T) {
			data, err := Marshal(env, format)
			require.NoError(t, err)

			restored, err := Unmarshal(data, format)
			require.NoError(t, err)

			assert.Equal(t, MessageTypeTest, restored.Type)
			require.NotNil(t, restored.TestMessage)
			assert.Equal(t, "", restored.TestMessage.ID)
		})
	}
}

func TestMessageTypeTestConstant(t *testing.T) {
	assert.Equal(t, "crossguard_test", MessageTypeTest)
}
