package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

func makeEnvelope(text string) *model.Envelope {
	return &model.Envelope{
		Type:      model.MessageTypePost,
		Timestamp: "2026-01-01T00:00:00Z",
		PostMessage: &model.PostMessage{
			PostID:      "post1",
			ChannelID:   "ch1",
			ChannelName: "town-square",
			TeamID:      "team1",
			TeamName:    "test-team",
			UserID:      "user1",
			Username:    "testuser",
			MessageText: text,
			CreateAt:    1000000,
		},
	}
}

// measureOverhead returns the serialized size of the envelope with empty message text.
func measureOverhead(t *testing.T, env *model.Envelope, format model.Format) int {
	t.Helper()
	orig := env.PostMessage.MessageText
	env.PostMessage.MessageText = ""
	data, err := model.Marshal(env, format)
	env.PostMessage.MessageText = orig
	require.NoError(t, err)
	return len(data)
}

func TestSplitMessage(t *testing.T) {
	t.Run("short message returns single part without label", func(t *testing.T) {
		text := "hello world"
		env := makeEnvelope(text)
		overhead := measureOverhead(t, env, model.FormatJSON)
		maxSize := overhead + safetyMargin + len(text) + 100

		parts := splitMessage(env, model.FormatJSON, maxSize)

		require.Len(t, parts, 1)
		assert.Equal(t, text, parts[0])
	})

	t.Run("long message splits into labeled parts", func(t *testing.T) {
		text := strings.Repeat("a", 5000)
		env := makeEnvelope(text)
		overhead := measureOverhead(t, env, model.FormatJSON)
		// Allow ~200 bytes for text per part.
		maxSize := overhead + safetyMargin + 200

		parts := splitMessage(env, model.FormatJSON, maxSize)

		require.Greater(t, len(parts), 1)
		// Every part should have a label.
		for i, p := range parts {
			assert.Contains(t, p, fmt.Sprintf("[Part %d/%d]", i+1, len(parts)))
		}
		// Reconstruct: strip labels and rejoin should equal original.
		var rebuilt strings.Builder
		for _, p := range parts {
			// Strip the "[Part N/M] " prefix.
			if _, after, ok := strings.Cut(p, "] "); ok {
				rebuilt.WriteString(after)
			}
		}
		assert.Equal(t, text, rebuilt.String())
	})

	t.Run("UTF-8 multibyte boundary is safe", func(t *testing.T) {
		// Mix of 1-byte ASCII, 3-byte CJK, and 4-byte emoji characters.
		text := strings.Repeat("\U0001F600", 300) + strings.Repeat("\u4e16", 300) + "end"
		env := makeEnvelope(text)
		overhead := measureOverhead(t, env, model.FormatJSON)
		maxSize := overhead + safetyMargin + 500

		parts := splitMessage(env, model.FormatJSON, maxSize)

		require.Greater(t, len(parts), 1)
		for _, p := range parts {
			assert.True(t, utf8.ValidString(p), "each part must be valid UTF-8")
		}
	})

	t.Run("very small maxSize still produces parts", func(t *testing.T) {
		text := "some message that should still work"
		env := makeEnvelope(text)

		// maxSize barely larger than overhead.
		parts := splitMessage(env, model.FormatJSON, 10)

		require.Greater(t, len(parts), 0)
	})

	t.Run("already-threaded message preserves original text in parts", func(t *testing.T) {
		text := strings.Repeat("b", 3000)
		env := makeEnvelope(text)
		env.PostMessage.RootID = "existing-root-id"
		overhead := measureOverhead(t, env, model.FormatJSON)
		maxSize := overhead + safetyMargin + 200

		parts := splitMessage(env, model.FormatJSON, maxSize)

		require.Greater(t, len(parts), 1)
		for i, p := range parts {
			assert.Contains(t, p, fmt.Sprintf("[Part %d/%d]", i+1, len(parts)))
		}
	})
}

// addLogMocks registers permissive log expectations on the mock API.
func addLogMocks(api *plugintest.API) {
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
}

func TestBuildPostEnvelope(t *testing.T) {
	t.Run("creates correct post envelope", func(t *testing.T) {
		post := &mmModel.Post{
			Id:        "post-id",
			RootId:    "root-id",
			ChannelId: "chan-id",
			UserId:    "user-id",
			Message:   "hello",
			CreateAt:  1234567890,
		}
		channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}

		env := buildPostEnvelope(model.MessageTypePost, post, channel, "test-team", "alice")

		assert.Equal(t, model.MessageTypePost, env.Type)
		assert.NotEmpty(t, env.Timestamp)
		require.NotNil(t, env.PostMessage)
		assert.Equal(t, "post-id", env.PostMessage.PostID)
		assert.Equal(t, "root-id", env.PostMessage.RootID)
		assert.Equal(t, "chan-id", env.PostMessage.ChannelID)
		assert.Equal(t, "town-square", env.PostMessage.ChannelName)
		assert.Equal(t, "team-id", env.PostMessage.TeamID)
		assert.Equal(t, "test-team", env.PostMessage.TeamName)
		assert.Equal(t, "user-id", env.PostMessage.UserID)
		assert.Equal(t, "alice", env.PostMessage.Username)
		assert.Equal(t, "hello", env.PostMessage.MessageText)
		assert.Equal(t, int64(1234567890), env.PostMessage.CreateAt)
	})

	t.Run("update type uses correct message type", func(t *testing.T) {
		post := &mmModel.Post{Id: "post-id", ChannelId: "chan-id", Message: "edited"}
		channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}

		env := buildPostEnvelope(model.MessageTypeUpdate, post, channel, "team", "bob")
		assert.Equal(t, model.MessageTypeUpdate, env.Type)
		require.NotNil(t, env.PostMessage)
		assert.Equal(t, "edited", env.PostMessage.MessageText)
	})

	t.Run("timestamp is valid RFC3339", func(t *testing.T) {
		post := &mmModel.Post{Id: "p1", ChannelId: "c1"}
		channel := &mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}

		env := buildPostEnvelope(model.MessageTypePost, post, channel, "team", "user")

		_, err := time.Parse(time.RFC3339, env.Timestamp)
		assert.NoError(t, err, "timestamp should be valid RFC3339")
	})

	t.Run("empty fields are preserved", func(t *testing.T) {
		post := &mmModel.Post{Id: "p1", ChannelId: "c1", Message: ""}
		channel := &mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}

		env := buildPostEnvelope(model.MessageTypePost, post, channel, "", "")

		assert.Empty(t, env.PostMessage.RootID)
		assert.Empty(t, env.PostMessage.TeamName)
		assert.Empty(t, env.PostMessage.Username)
		assert.Empty(t, env.PostMessage.MessageText)
	})
}

func TestBuildDeleteEnvelope(t *testing.T) {
	t.Run("creates correct delete envelope", func(t *testing.T) {
		post := &mmModel.Post{Id: "post-id", ChannelId: "chan-id"}
		channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}

		env := buildDeleteEnvelope(post, channel, "test-team")

		assert.Equal(t, model.MessageTypeDelete, env.Type)
		assert.NotEmpty(t, env.Timestamp)
		require.NotNil(t, env.DeleteMessage)
		assert.Equal(t, "post-id", env.DeleteMessage.PostID)
		assert.Equal(t, "chan-id", env.DeleteMessage.ChannelID)
		assert.Equal(t, "town-square", env.DeleteMessage.ChannelName)
		assert.Equal(t, "team-id", env.DeleteMessage.TeamID)
		assert.Equal(t, "test-team", env.DeleteMessage.TeamName)
	})

	t.Run("no post or reaction message fields set", func(t *testing.T) {
		post := &mmModel.Post{Id: "p1", ChannelId: "c1"}
		channel := &mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}

		env := buildDeleteEnvelope(post, channel, "team")

		assert.Nil(t, env.PostMessage)
		assert.Nil(t, env.ReactionMessage)
	})
}

func TestBuildReactionEnvelope(t *testing.T) {
	t.Run("creates correct add reaction envelope", func(t *testing.T) {
		reaction := &mmModel.Reaction{PostId: "post-id", UserId: "user-id", EmojiName: "thumbsup"}
		channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}

		env := buildReactionEnvelope(model.MessageTypeReactionAdd, reaction, channel, "team", "alice")

		assert.Equal(t, model.MessageTypeReactionAdd, env.Type)
		assert.NotEmpty(t, env.Timestamp)
		require.NotNil(t, env.ReactionMessage)
		assert.Equal(t, "post-id", env.ReactionMessage.PostID)
		assert.Equal(t, "chan-id", env.ReactionMessage.ChannelID)
		assert.Equal(t, "ch", env.ReactionMessage.ChannelName)
		assert.Equal(t, "team-id", env.ReactionMessage.TeamID)
		assert.Equal(t, "team", env.ReactionMessage.TeamName)
		assert.Equal(t, "user-id", env.ReactionMessage.UserID)
		assert.Equal(t, "alice", env.ReactionMessage.Username)
		assert.Equal(t, "thumbsup", env.ReactionMessage.EmojiName)
	})

	t.Run("creates correct remove reaction envelope", func(t *testing.T) {
		reaction := &mmModel.Reaction{PostId: "post-id", UserId: "user-id", EmojiName: "thumbsup"}
		channel := &mmModel.Channel{Id: "chan-id", Name: "ch", TeamId: "team-id"}

		env := buildReactionEnvelope(model.MessageTypeReactionRemove, reaction, channel, "team", "bob")

		assert.Equal(t, model.MessageTypeReactionRemove, env.Type)
		require.NotNil(t, env.ReactionMessage)
		assert.Equal(t, "bob", env.ReactionMessage.Username)
	})

	t.Run("no post or delete message fields set", func(t *testing.T) {
		reaction := &mmModel.Reaction{PostId: "p1", UserId: "u1", EmojiName: "smile"}
		channel := &mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}

		env := buildReactionEnvelope(model.MessageTypeReactionAdd, reaction, channel, "team", "user")

		assert.Nil(t, env.PostMessage)
		assert.Nil(t, env.DeleteMessage)
	})
}

func TestBuildTestMessageFormats(t *testing.T) {
	t.Run("JSON format produces valid envelope", func(t *testing.T) {
		data, msgID, err := buildTestMessage(model.FormatJSON)
		require.NoError(t, err)
		assert.NotEmpty(t, msgID)
		assert.NotEmpty(t, data)

		env, unmarshalErr := model.Unmarshal(data, model.FormatJSON)
		require.NoError(t, unmarshalErr)
		assert.Equal(t, model.MessageTypeTest, env.Type)
		require.NotNil(t, env.TestMessage)
		assert.Equal(t, msgID, env.TestMessage.ID)
	})

	t.Run("XML format produces valid envelope", func(t *testing.T) {
		data, msgID, err := buildTestMessage(model.FormatXML)
		require.NoError(t, err)
		assert.NotEmpty(t, msgID)
		assert.NotEmpty(t, data)

		env, unmarshalErr := model.Unmarshal(data, model.FormatXML)
		require.NoError(t, unmarshalErr)
		assert.Equal(t, model.MessageTypeTest, env.Type)
		require.NotNil(t, env.TestMessage)
		assert.Equal(t, msgID, env.TestMessage.ID)
	})

	t.Run("each call produces a unique message ID", func(t *testing.T) {
		_, id1, err1 := buildTestMessage(model.FormatJSON)
		_, id2, err2 := buildTestMessage(model.FormatJSON)
		require.NoError(t, err1)
		require.NoError(t, err2)
		assert.NotEqual(t, id1, id2)
	})

	t.Run("timestamp is present in serialized data", func(t *testing.T) {
		data, _, err := buildTestMessage(model.FormatJSON)
		require.NoError(t, err)

		env, unmarshalErr := model.Unmarshal(data, model.FormatJSON)
		require.NoError(t, unmarshalErr)
		assert.NotEmpty(t, env.Timestamp)
	})
}

func TestPublishToOutbound(t *testing.T) {
	t.Run("empty outbound pool is no-op", func(t *testing.T) {
		api := &plugintest.API{}
		addLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.outboundConns = nil

		env := buildPostEnvelope(model.MessageTypePost,
			&mmModel.Post{Id: "p1", ChannelId: "c1", Message: "hi"},
			&mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}, "team", "user")

		conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}
		p.publishToOutbound(context.Background(), env, conns)
		// No panic, no provider calls
	})

	t.Run("unlinked connection is skipped", func(t *testing.T) {
		api := &plugintest.API{}
		addLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		published := false
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{publishFn: func(ctx context.Context, data []byte) error {
				published = true
				return nil
			}},
			name:          "high",
			healthy:       true,
			lastCheckTime: time.Now(),
		}}

		env := buildPostEnvelope(model.MessageTypePost,
			&mmModel.Post{Id: "p1", ChannelId: "c1", Message: "hi"},
			&mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}, "team", "user")

		// Link "other" not "high"
		conns := []store.TeamConnection{{Direction: "outbound", Connection: "other"}}
		p.publishToOutbound(context.Background(), env, conns)
		assert.False(t, published)
	})

	t.Run("unhealthy connection within recheck interval is skipped", func(t *testing.T) {
		api := &plugintest.API{}
		addLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		published := false
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{publishFn: func(ctx context.Context, data []byte) error {
				published = true
				return nil
			}},
			name:          "high",
			healthy:       false,
			lastCheckTime: time.Now(), // Just checked, within 30s
		}}

		env := buildPostEnvelope(model.MessageTypePost,
			&mmModel.Post{Id: "p1", ChannelId: "c1", Message: "hi"},
			&mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}, "team", "user")
		conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}
		p.publishToOutbound(context.Background(), env, conns)
		assert.False(t, published)
	})

	t.Run("unhealthy connection past recheck interval is retried", func(t *testing.T) {
		api := &plugintest.API{}
		addLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		published := false
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{publishFn: func(ctx context.Context, data []byte) error {
				published = true
				return nil
			}},
			name:          "high",
			healthy:       false,
			lastCheckTime: time.Now().Add(-31 * time.Second), // Past 30s recheck
		}}

		env := buildPostEnvelope(model.MessageTypePost,
			&mmModel.Post{Id: "p1", ChannelId: "c1", Message: "hi"},
			&mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}, "team", "user")
		conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}
		p.publishToOutbound(context.Background(), env, conns)
		assert.True(t, published)
	})

	t.Run("publish failure marks connection unhealthy", func(t *testing.T) {
		api := &plugintest.API{}
		addLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{publishFn: func(ctx context.Context, data []byte) error {
				return errors.New("connection refused")
			}},
			name:          "high",
			healthy:       true,
			lastCheckTime: time.Now(),
		}}

		env := buildPostEnvelope(model.MessageTypePost,
			&mmModel.Post{Id: "p1", ChannelId: "c1", Message: "hi"},
			&mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}, "team", "user")
		conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}
		p.publishToOutbound(context.Background(), env, conns)

		assert.False(t, p.outboundConns[0].healthy)
	})

	t.Run("publish success marks connection healthy", func(t *testing.T) {
		api := &plugintest.API{}
		addLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		p.outboundConns = []outboundConn{{
			provider:      &mockQueueProvider{},
			name:          "high",
			healthy:       false, // Was unhealthy
			lastCheckTime: time.Now().Add(-31 * time.Second),
		}}

		env := buildPostEnvelope(model.MessageTypePost,
			&mmModel.Post{Id: "p1", ChannelId: "c1", Message: "hi"},
			&mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}, "team", "user")
		conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}
		p.publishToOutbound(context.Background(), env, conns)

		assert.True(t, p.outboundConns[0].healthy)
	})

	t.Run("message exceeding MaxMessageSize triggers split", func(t *testing.T) {
		api := &plugintest.API{}
		addLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		var publishCount int
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{
				publishFn: func(ctx context.Context, data []byte) error {
					publishCount++
					return nil
				},
				maxMsgSize: 200, // Very small limit to force split
			},
			name:          "high",
			healthy:       true,
			lastCheckTime: time.Now(),
		}}

		longText := strings.Repeat("a", 500)
		env := buildPostEnvelope(model.MessageTypePost,
			&mmModel.Post{Id: "p1", ChannelId: "c1", Message: longText},
			&mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}, "team", "user")
		conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}
		p.publishToOutbound(context.Background(), env, conns)

		assert.Greater(t, publishCount, 1, "should publish multiple parts")
	})

	t.Run("non-post message exceeding MaxMessageSize is not split", func(t *testing.T) {
		api := &plugintest.API{}
		addLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		var publishCount int
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{
				publishFn: func(ctx context.Context, data []byte) error {
					publishCount++
					return nil
				},
				maxMsgSize: 10, // Very small, but delete messages cannot be split
			},
			name:          "high",
			healthy:       true,
			lastCheckTime: time.Now(),
		}}

		// Delete envelope has no PostMessage, so split path is not taken.
		env := buildDeleteEnvelope(
			&mmModel.Post{Id: "p1", ChannelId: "c1"},
			&mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}, "team")
		conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}
		p.publishToOutbound(context.Background(), env, conns)

		// Should still publish once (no split for non-post messages).
		assert.Equal(t, 1, publishCount)
	})

	t.Run("healthy connection with zero MaxMessageSize publishes without split", func(t *testing.T) {
		api := &plugintest.API{}
		addLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		var publishCount int
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{
				publishFn: func(ctx context.Context, data []byte) error {
					publishCount++
					return nil
				},
				maxMsgSize: 0, // Zero means no limit
			},
			name:          "high",
			healthy:       true,
			lastCheckTime: time.Now(),
		}}

		longText := strings.Repeat("x", 5000)
		env := buildPostEnvelope(model.MessageTypePost,
			&mmModel.Post{Id: "p1", ChannelId: "c1", Message: longText},
			&mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}, "team", "user")
		conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}
		p.publishToOutbound(context.Background(), env, conns)

		assert.Equal(t, 1, publishCount, "no split when MaxMessageSize is 0")
	})

	t.Run("multiple outbound connections each receive the message", func(t *testing.T) {
		api := &plugintest.API{}
		addLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		var countA, countB int
		p.outboundConns = []outboundConn{
			{
				provider: &mockQueueProvider{publishFn: func(ctx context.Context, data []byte) error {
					countA++
					return nil
				}},
				name:          "conn-a",
				healthy:       true,
				lastCheckTime: time.Now(),
			},
			{
				provider: &mockQueueProvider{publishFn: func(ctx context.Context, data []byte) error {
					countB++
					return nil
				}},
				name:          "conn-b",
				healthy:       true,
				lastCheckTime: time.Now(),
			},
		}

		env := buildPostEnvelope(model.MessageTypePost,
			&mmModel.Post{Id: "p1", ChannelId: "c1", Message: "hi"},
			&mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}, "team", "user")
		conns := []store.TeamConnection{
			{Direction: "outbound", Connection: "conn-a"},
			{Direction: "outbound", Connection: "conn-b"},
		}
		p.publishToOutbound(context.Background(), env, conns)

		assert.Equal(t, 1, countA)
		assert.Equal(t, 1, countB)
	})

	t.Run("inbound direction connection is not published to", func(t *testing.T) {
		api := &plugintest.API{}
		addLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		published := false
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{publishFn: func(ctx context.Context, data []byte) error {
				published = true
				return nil
			}},
			name:          "high",
			healthy:       true,
			lastCheckTime: time.Now(),
		}}

		env := buildPostEnvelope(model.MessageTypePost,
			&mmModel.Post{Id: "p1", ChannelId: "c1", Message: "hi"},
			&mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}, "team", "user")
		// Direction is "inbound", not "outbound"
		conns := []store.TeamConnection{{Direction: "inbound", Connection: "high"}}
		p.publishToOutbound(context.Background(), env, conns)
		assert.False(t, published)
	})

	t.Run("split part failure marks connection unhealthy", func(t *testing.T) {
		api := &plugintest.API{}
		addLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		callNum := 0
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{
				publishFn: func(ctx context.Context, data []byte) error {
					callNum++
					if callNum == 2 {
						return errors.New("part 2 failed")
					}
					return nil
				},
				maxMsgSize: 200,
			},
			name:          "high",
			healthy:       true,
			lastCheckTime: time.Now(),
		}}

		longText := strings.Repeat("z", 500)
		env := buildPostEnvelope(model.MessageTypePost,
			&mmModel.Post{Id: "p1", ChannelId: "c1", Message: longText},
			&mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}, "team", "user")
		conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}
		p.publishToOutbound(context.Background(), env, conns)

		assert.False(t, p.outboundConns[0].healthy)
	})

	t.Run("default format is JSON when messageFormat is empty", func(t *testing.T) {
		api := &plugintest.API{}
		addLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		var capturedData []byte
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{publishFn: func(ctx context.Context, data []byte) error {
				capturedData = data
				return nil
			}},
			name:          "high",
			messageFormat: "", // Empty, should default to JSON
			healthy:       true,
			lastCheckTime: time.Now(),
		}}

		env := buildPostEnvelope(model.MessageTypePost,
			&mmModel.Post{Id: "p1", ChannelId: "c1", Message: "test"},
			&mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}, "team", "user")
		conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}
		p.publishToOutbound(context.Background(), env, conns)

		require.NotNil(t, capturedData)
		// Verify the data is valid JSON by unmarshalling.
		_, err := model.Unmarshal(capturedData, model.FormatJSON)
		assert.NoError(t, err)
	})

	t.Run("XML format connection serializes as XML", func(t *testing.T) {
		api := &plugintest.API{}
		addLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)

		var capturedData []byte
		p.outboundConns = []outboundConn{{
			provider: &mockQueueProvider{publishFn: func(ctx context.Context, data []byte) error {
				capturedData = data
				return nil
			}},
			name:          "high",
			messageFormat: string(model.FormatXML),
			healthy:       true,
			lastCheckTime: time.Now(),
		}}

		env := buildPostEnvelope(model.MessageTypePost,
			&mmModel.Post{Id: "p1", ChannelId: "c1", Message: "test"},
			&mmModel.Channel{Id: "c1", Name: "ch", TeamId: "t1"}, "team", "user")
		conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}
		p.publishToOutbound(context.Background(), env, conns)

		require.NotNil(t, capturedData)
		// Verify the data is valid XML by unmarshalling.
		_, err := model.Unmarshal(capturedData, model.FormatXML)
		assert.NoError(t, err)
	})
}

func TestCreateProvider_UnknownProvider(t *testing.T) {
	api := &plugintest.API{}
	addLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	cfg := ConnectionConfig{
		Name:     "test-conn",
		Provider: "unknown",
	}
	_, err := p.createProvider(cfg, "Outbound")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

func TestCreateProvider_MissingNATSConfig(t *testing.T) {
	api := &plugintest.API{}
	addLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	cfg := ConnectionConfig{
		Name:     "test-conn",
		Provider: "nats",
		NATS:     nil,
	}
	_, err := p.createProvider(cfg, "Outbound")
	require.Error(t, err)
	assert.ErrorIs(t, err, errMissingNATSConfig)
}

func TestCreateProvider_MissingAzureConfig(t *testing.T) {
	api := &plugintest.API{}
	addLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	cfg := ConnectionConfig{
		Name:     "test-conn",
		Provider: "azure",
		Azure:    nil,
	}
	_, err := p.createProvider(cfg, "Outbound")
	require.Error(t, err)
	assert.ErrorIs(t, err, errMissingAzureConfig)
}

func TestUploadPostFiles_NoFileEnabledConns(t *testing.T) {
	api := &plugintest.API{}
	addLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	p.outboundConns = []outboundConn{{
		provider:            &mockQueueProvider{},
		name:                "high",
		fileTransferEnabled: false,
		healthy:             true,
		lastCheckTime:       time.Now(),
	}}

	post := &mmModel.Post{
		Id:        "post-1",
		ChannelId: "ch1",
		FileIds:   mmModel.StringArray{"file-1", "file-2"},
	}
	conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}

	// GetFileInfo should never be called because no connections have file transfer enabled.
	p.uploadPostFiles(post, conns)
	p.wg.Wait()

	api.AssertNotCalled(t, "GetFileInfo", mock.Anything)
}

func TestUploadPostFiles_GetFileInfoError(t *testing.T) {
	api := &plugintest.API{}
	addLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	p.outboundConns = []outboundConn{{
		provider:            &mockQueueProvider{},
		name:                "high",
		fileTransferEnabled: true,
		healthy:             true,
		lastCheckTime:       time.Now(),
	}}

	api.On("GetConfig").Return(&mmModel.Config{}).Maybe()
	api.On("GetFileInfo", "file-1").Return(nil, &mmModel.AppError{Message: "file not found"})

	post := &mmModel.Post{
		Id:        "post-1",
		ChannelId: "ch1",
		FileIds:   mmModel.StringArray{"file-1"},
	}
	conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}

	p.uploadPostFiles(post, conns)
	p.wg.Wait()

	api.AssertCalled(t, "GetFileInfo", "file-1")
	// GetFile should not be called because GetFileInfo failed.
	api.AssertNotCalled(t, "GetFile", mock.Anything)
}

func TestUploadPostFiles_OversizedFile(t *testing.T) {
	api := &plugintest.API{}
	addLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	uploaded := false
	p.outboundConns = []outboundConn{{
		provider: &mockQueueProvider{
			uploadFileFn: func(ctx context.Context, key string, data []byte, headers map[string]string) error {
				uploaded = true
				return nil
			},
		},
		name:                "high",
		fileTransferEnabled: true,
		healthy:             true,
		lastCheckTime:       time.Now(),
	}}

	maxSize := int64(1024)
	api.On("GetConfig").Return(&mmModel.Config{
		FileSettings: mmModel.FileSettings{
			MaxFileSize: &maxSize,
		},
	}).Maybe()
	api.On("GetFileInfo", "file-1").Return(&mmModel.FileInfo{
		Id:   "file-1",
		Name: "bigfile.bin",
		Size: 2048, // Exceeds maxSize of 1024
	}, nil)

	post := &mmModel.Post{
		Id:        "post-1",
		ChannelId: "ch1",
		FileIds:   mmModel.StringArray{"file-1"},
	}
	conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}

	p.uploadPostFiles(post, conns)
	p.wg.Wait()

	// File should be skipped due to size, so no GetFile call and no upload.
	api.AssertNotCalled(t, "GetFile", mock.Anything)
	assert.False(t, uploaded)
}

func TestUploadPostFiles_FileFilteredByPolicy(t *testing.T) {
	api := &plugintest.API{}
	addLogMocks(api)
	p, _ := setupTestPluginWithRouter(api)

	uploaded := false
	p.outboundConns = []outboundConn{{
		provider: &mockQueueProvider{
			uploadFileFn: func(ctx context.Context, key string, data []byte, headers map[string]string) error {
				uploaded = true
				return nil
			},
		},
		name:                "high",
		fileTransferEnabled: true,
		fileFilterMode:      "allow",
		fileFilterTypes:     ".pdf",
		healthy:             true,
		lastCheckTime:       time.Now(),
	}}

	api.On("GetConfig").Return(&mmModel.Config{}).Maybe()
	api.On("GetFileInfo", "file-1").Return(&mmModel.FileInfo{
		Id:   "file-1",
		Name: "test.txt",
		Size: 100,
	}, nil)
	api.On("GetFile", "file-1").Return([]byte("file content"), nil)

	post := &mmModel.Post{
		Id:        "post-1",
		ChannelId: "ch1",
		FileIds:   mmModel.StringArray{"file-1"},
	}
	conns := []store.TeamConnection{{Direction: "outbound", Connection: "high"}}

	p.uploadPostFiles(post, conns)
	p.wg.Wait()

	// File is .txt but only .pdf is allowed, so UploadFile should not be called.
	assert.False(t, uploaded)
}

func TestUpdateOutboundHealth_SetsHealthy(t *testing.T) {
	p := &Plugin{}
	p.outboundConns = []outboundConn{
		{name: "conn-0", healthy: false, lastCheckTime: time.Now().Add(-time.Minute)},
	}

	p.updateOutboundHealth(0, true)

	assert.True(t, p.outboundConns[0].healthy)
	assert.WithinDuration(t, time.Now(), p.outboundConns[0].lastCheckTime, 2*time.Second)
}

func TestUpdateOutboundHealth_SetsUnhealthy(t *testing.T) {
	p := &Plugin{}
	p.outboundConns = []outboundConn{
		{name: "conn-0", healthy: true, lastCheckTime: time.Now().Add(-time.Minute)},
	}

	p.updateOutboundHealth(0, false)

	assert.False(t, p.outboundConns[0].healthy)
	assert.WithinDuration(t, time.Now(), p.outboundConns[0].lastCheckTime, 2*time.Second)
}

func TestUpdateOutboundHealth_IndexOutOfRange(t *testing.T) {
	p := &Plugin{}
	p.outboundConns = []outboundConn{
		{name: "conn-0", healthy: true},
	}

	// Should not panic when index is beyond the slice length.
	assert.NotPanics(t, func() {
		p.updateOutboundHealth(5, false)
	})

	// Original entry should be unchanged.
	assert.True(t, p.outboundConns[0].healthy)
}
