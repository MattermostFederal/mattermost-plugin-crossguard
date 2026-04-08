package main

import (
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

func TestTruncateToFit(t *testing.T) {
	const indicator = "\n[message truncated]"

	t.Run("short message fits within limit", func(t *testing.T) {
		text := "hello world"
		env := makeEnvelope(text)
		overhead := measureOverhead(t, env, model.FormatJSON)

		// Provide enough room for the message text.
		maxSize := overhead + safetyMargin + len(text) + 100

		result := truncateToFit(env, model.FormatJSON, maxSize)

		assert.Equal(t, text+indicator, result)
		assert.Contains(t, result, text)
		assert.True(t, strings.HasSuffix(result, indicator))
	})

	t.Run("long message is truncated", func(t *testing.T) {
		text := strings.Repeat("a", 5000)
		env := makeEnvelope(text)
		overhead := measureOverhead(t, env, model.FormatJSON)

		// Allow only 200 bytes for text (well under the 5000 byte message).
		maxSize := overhead + safetyMargin + 200

		result := truncateToFit(env, model.FormatJSON, maxSize)

		assert.True(t, len(result) < len(text), "result should be shorter than original")
		assert.True(t, strings.HasSuffix(result, indicator))
		// The text portion (before indicator) must be at most 200 bytes.
		textPortion := strings.TrimSuffix(result, indicator)
		assert.LessOrEqual(t, len(textPortion), 200)
	})

	t.Run("UTF-8 multibyte boundary is safe", func(t *testing.T) {
		// Mix of 1-byte ASCII, 3-byte CJK, and 4-byte emoji characters.
		text := strings.Repeat("\U0001F600", 300) + strings.Repeat("\u4e16", 300) + "end"
		env := makeEnvelope(text)
		overhead := measureOverhead(t, env, model.FormatJSON)

		// Force truncation somewhere in the middle of the multibyte sequence.
		maxSize := overhead + safetyMargin + 500

		result := truncateToFit(env, model.FormatJSON, maxSize)

		assert.True(t, utf8.ValidString(result), "result must be valid UTF-8")
		assert.True(t, strings.HasSuffix(result, indicator))
	})

	t.Run("very small maxSize returns only indicator", func(t *testing.T) {
		text := "some message"
		env := makeEnvelope(text)

		// maxSize smaller than overhead + safetyMargin, so available <= 0.
		result := truncateToFit(env, model.FormatJSON, 10)

		assert.Equal(t, indicator, result)
	})

	t.Run("exact fit message", func(t *testing.T) {
		env := makeEnvelope("")
		overhead := measureOverhead(t, env, model.FormatJSON)

		// Create a message that is exactly the available size.
		available := 150
		text := strings.Repeat("x", available)
		env.PostMessage.MessageText = text

		maxSize := overhead + safetyMargin + available

		result := truncateToFit(env, model.FormatJSON, maxSize)

		assert.Equal(t, text+indicator, result)
		// Verify the full text is preserved (not truncated).
		textPortion := strings.TrimSuffix(result, indicator)
		assert.Equal(t, text, textPortion)
	})
}
