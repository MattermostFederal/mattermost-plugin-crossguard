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
		Message:     "Hello from the other side",
		CreateAt:    1700000000000,
	}

	env, err := NewMessage(MessageTypePost, original)
	require.NoError(t, err)
	assert.Equal(t, MessageTypePost, env.Type)

	data, err := Marshal(env)
	require.NoError(t, err)

	restored, err := UnmarshalMessage(data)
	require.NoError(t, err)
	assert.Equal(t, MessageTypePost, restored.Type)
	assert.NotEmpty(t, restored.Timestamp)

	var decoded PostMessage
	err = restored.Decode(&decoded)
	require.NoError(t, err)

	assert.Equal(t, original, decoded)
}

func TestDeleteMessage_RoundTrip(t *testing.T) {
	original := DeleteMessage{
		PostID:      "post-del456",
		ChannelID:   "channel-002",
		ChannelName: "off-topic",
		TeamID:      "team-bbb",
		TeamName:    "other-team",
	}

	env, err := NewMessage(MessageTypeDelete, original)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeDelete, env.Type)

	data, err := Marshal(env)
	require.NoError(t, err)

	restored, err := UnmarshalMessage(data)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeDelete, restored.Type)
	assert.NotEmpty(t, restored.Timestamp)

	var decoded DeleteMessage
	err = restored.Decode(&decoded)
	require.NoError(t, err)

	assert.Equal(t, original, decoded)
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

	env, err := NewMessage(MessageTypeReactionAdd, original)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeReactionAdd, env.Type)

	data, err := Marshal(env)
	require.NoError(t, err)

	restored, err := UnmarshalMessage(data)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeReactionAdd, restored.Type)
	assert.NotEmpty(t, restored.Timestamp)

	var decoded ReactionMessage
	err = restored.Decode(&decoded)
	require.NoError(t, err)

	assert.Equal(t, original, decoded)
}

func TestPostMessage_UpdateType(t *testing.T) {
	original := PostMessage{
		PostID:      "post-upd001",
		RootID:      "",
		ChannelID:   "channel-004",
		ChannelName: "updates",
		TeamID:      "team-ddd",
		TeamName:    "update-team",
		UserID:      "user-333",
		Username:    "carol",
		Message:     "Edited message content",
		CreateAt:    1700000001000,
	}

	env, err := NewMessage(MessageTypeUpdate, original)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeUpdate, env.Type)

	data, err := Marshal(env)
	require.NoError(t, err)

	restored, err := UnmarshalMessage(data)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeUpdate, restored.Type)
	assert.NotEmpty(t, restored.Timestamp)

	var decoded PostMessage
	err = restored.Decode(&decoded)
	require.NoError(t, err)

	assert.Equal(t, original, decoded)
}
