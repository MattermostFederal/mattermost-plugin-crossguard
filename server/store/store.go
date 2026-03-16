package store

const (
	PromptStatePending = "pending"
	PromptStateBlocked = "blocked"
)

// ConnectionPrompt represents a pending or blocked inbound connection prompt.
type ConnectionPrompt struct {
	State  string `json:"state"`
	PostID string `json:"post_id"`
}

// KVStore defines the key-value operations used by the plugin.
type KVStore interface {
	GetTeamConnections(teamID string) ([]string, error)
	SetTeamConnections(teamID string, connNames []string) error
	DeleteTeamConnections(teamID string) error
	IsTeamInitialized(teamID string) (bool, error)
	AddTeamConnection(teamID, connName string) error
	RemoveTeamConnection(teamID, connName string) error
	GetInitializedTeamIDs() ([]string, error)
	AddInitializedTeamID(teamID string) error
	RemoveInitializedTeamID(teamID string) error
	GetChannelConnections(channelID string) ([]string, error)
	SetChannelConnections(channelID string, connNames []string) error
	DeleteChannelConnections(channelID string) error
	IsChannelInitialized(channelID string) (bool, error)
	AddChannelConnection(channelID, connName string) error
	RemoveChannelConnection(channelID, connName string) error
	SetPostMapping(connName, remotePostID, localPostID string) error
	GetPostMapping(connName, remotePostID string) (string, error)
	DeletePostMapping(connName, remotePostID string) error
	SetDeletingFlag(postID string) error
	IsDeletingFlagSet(postID string) (bool, error)
	ClearDeletingFlag(postID string) error
	GetConnectionPrompt(teamID, connName string) (*ConnectionPrompt, error)
	SetConnectionPrompt(teamID, connName string, prompt *ConnectionPrompt) error
	DeleteConnectionPrompt(teamID, connName string) error
	CreateConnectionPrompt(teamID, connName string, prompt *ConnectionPrompt) (bool, error)
	GetChannelConnectionPrompt(channelID, connName string) (*ConnectionPrompt, error)
	SetChannelConnectionPrompt(channelID, connName string, prompt *ConnectionPrompt) error
	DeleteChannelConnectionPrompt(channelID, connName string) error
	CreateChannelConnectionPrompt(channelID, connName string, prompt *ConnectionPrompt) (bool, error)
}
