package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostMessage_RoundTrip(t *testing.T) {
	original := PostMessage{
		PostID:      "post-abc123",
		RootID:      "root-xyz789",
		ChannelID:   "channel-001",
		ChannelName: "town-square",
		TeamID:      "team-aaa",
		TeamName:    "test-team",
		UserID:      "user-111",
		Username:    "alice",
		MessageText: "Hello from the other side",
		CreateAt:    1700000000000,
	}

	for _, format := range []Format{FormatJSON, FormatXML} {
		t.Run(string(format), func(t *testing.T) {
			env := &Envelope{
				Type:        MessageTypePost,
				Timestamp:   "2026-04-06T12:00:00Z",
				PostMessage: &original,
			}

			data, err := Marshal(env, format)
			require.NoError(t, err)

			restored, err := Unmarshal(data, format)
			require.NoError(t, err)

			assert.Equal(t, MessageTypePost, restored.Type)
			assert.NotEmpty(t, restored.Timestamp)
			require.NotNil(t, restored.PostMessage)
			assert.Equal(t, original, *restored.PostMessage)
		})
	}
}

func TestDeleteMessage_RoundTrip(t *testing.T) {
	original := DeleteMessage{
		PostID:      "post-del456",
		ChannelID:   "channel-002",
		ChannelName: "off-topic",
		TeamID:      "team-bbb",
		TeamName:    "other-team",
	}

	for _, format := range []Format{FormatJSON, FormatXML} {
		t.Run(string(format), func(t *testing.T) {
			env := &Envelope{
				Type:          MessageTypeDelete,
				Timestamp:     "2026-04-06T12:00:00Z",
				DeleteMessage: &original,
			}

			data, err := Marshal(env, format)
			require.NoError(t, err)

			restored, err := Unmarshal(data, format)
			require.NoError(t, err)

			assert.Equal(t, MessageTypeDelete, restored.Type)
			require.NotNil(t, restored.DeleteMessage)
			assert.Equal(t, original, *restored.DeleteMessage)
		})
	}
}

func TestReactionMessage_RoundTrip(t *testing.T) {
	original := ReactionMessage{
		PostID:      "post-react789",
		ChannelID:   "channel-003",
		ChannelName: "general",
		TeamID:      "team-ccc",
		TeamName:    "react-team",
		UserID:      "user-222",
		Username:    "bob",
		EmojiName:   "thumbsup",
	}

	for _, format := range []Format{FormatJSON, FormatXML} {
		t.Run(string(format), func(t *testing.T) {
			env := &Envelope{
				Type:            MessageTypeReactionAdd,
				Timestamp:       "2026-04-06T12:00:00Z",
				ReactionMessage: &original,
			}

			data, err := Marshal(env, format)
			require.NoError(t, err)

			restored, err := Unmarshal(data, format)
			require.NoError(t, err)

			assert.Equal(t, MessageTypeReactionAdd, restored.Type)
			require.NotNil(t, restored.ReactionMessage)
			assert.Equal(t, original, *restored.ReactionMessage)
		})
	}
}

func TestPostMessage_UpdateType(t *testing.T) {
	original := PostMessage{
		PostID:      "post-upd001",
		ChannelID:   "channel-004",
		ChannelName: "updates",
		TeamID:      "team-ddd",
		TeamName:    "update-team",
		UserID:      "user-333",
		Username:    "carol",
		MessageText: "Edited message content",
		CreateAt:    1700000001000,
	}

	env := &Envelope{
		Type:        MessageTypeUpdate,
		Timestamp:   "2026-04-06T12:00:00Z",
		PostMessage: &original,
	}

	data, err := Marshal(env, FormatJSON)
	require.NoError(t, err)

	restored, err := Unmarshal(data, FormatJSON)
	require.NoError(t, err)

	assert.Equal(t, MessageTypeUpdate, restored.Type)
	require.NotNil(t, restored.PostMessage)
	assert.Equal(t, original, *restored.PostMessage)
}
