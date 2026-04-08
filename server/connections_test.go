package main

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
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
