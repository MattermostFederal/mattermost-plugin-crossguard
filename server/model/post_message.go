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
	PostID      string `json:"post_id"            xml:"PostID"`
	RootID      string `json:"root_id,omitempty"  xml:"RootID,omitempty"`
	ChannelID   string `json:"channel_id"         xml:"ChannelID"`
	ChannelName string `json:"channel_name"       xml:"ChannelName"`
	TeamID      string `json:"team_id"            xml:"TeamID"`
	TeamName    string `json:"team_name"          xml:"TeamName"`
	UserID      string `json:"user_id"            xml:"UserID"`
	Username    string `json:"username"           xml:"Username"`
	MessageText string `json:"message"            xml:"MessageText"`
	CreateAt    int64  `json:"create_at"          xml:"CreateAt"`
}

// DeleteMessage identifies a post that was deleted.
type DeleteMessage struct {
	PostID      string `json:"post_id"       xml:"PostID"`
	ChannelID   string `json:"channel_id"    xml:"ChannelID"`
	ChannelName string `json:"channel_name"  xml:"ChannelName"`
	TeamID      string `json:"team_id"       xml:"TeamID"`
	TeamName    string `json:"team_name"     xml:"TeamName"`
}

// ReactionMessage represents a reaction added or removed from a post.
type ReactionMessage struct {
	PostID      string `json:"post_id"       xml:"PostID"`
	ChannelID   string `json:"channel_id"    xml:"ChannelID"`
	ChannelName string `json:"channel_name"  xml:"ChannelName"`
	TeamID      string `json:"team_id"       xml:"TeamID"`
	TeamName    string `json:"team_name"     xml:"TeamName"`
	UserID      string `json:"user_id"       xml:"UserID"`
	Username    string `json:"username"      xml:"Username"`
	EmojiName   string `json:"emoji_name"    xml:"EmojiName"`
}
