package model

const (
	MessageTypePost           = "crossguard_post"
	MessageTypeUpdate         = "crossguard_update"
	MessageTypeDelete         = "crossguard_delete"
	MessageTypeReactionAdd    = "crossguard_reaction_add"
	MessageTypeReactionRemove = "crossguard_reaction_remove"
)

// PostMessage is used for both new posts (MessageTypePost) and updates (MessageTypeUpdate).
// For updates, the fields reflect the post's current state after the edit.
type PostMessage struct {
	PostID      string `json:"post_id"`
	RootID      string `json:"root_id,omitempty"`
	ChannelID   string `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	TeamID      string `json:"team_id"`
	TeamName    string `json:"team_name"`
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	Message     string `json:"message"`
	CreateAt    int64  `json:"create_at"`
}

// DeleteMessage identifies a post that was deleted.
type DeleteMessage struct {
	PostID      string `json:"post_id"`
	ChannelID   string `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	TeamID      string `json:"team_id"`
	TeamName    string `json:"team_name"`
}

// ReactionMessage represents a reaction added or removed from a post.
type ReactionMessage struct {
	PostID      string `json:"post_id"`
	ChannelID   string `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	TeamID      string `json:"team_id"`
	TeamName    string `json:"team_name"`
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	EmojiName   string `json:"emoji_name"`
}
