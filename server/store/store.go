package store

// KVStore defines the key-value operations used by the plugin.
type KVStore interface {
	GetTeamInitialized(teamID string) (bool, error)
	SetTeamInitialized(teamID string) error
	DeleteTeamInitialized(teamID string) error
	GetInitializedTeamIDs() ([]string, error)
	AddInitializedTeamID(teamID string) error
	RemoveInitializedTeamID(teamID string) error
	GetChannelInitialized(channelID string) (bool, error)
	SetChannelInitialized(channelID string) error
	DeleteChannelInitialized(channelID string) error
	SetPostMapping(connName, remotePostID, localPostID string) error
	GetPostMapping(connName, remotePostID string) (string, error)
	DeletePostMapping(connName, remotePostID string) error
	SetDeletingFlag(postID string) error
	IsDeletingFlagSet(postID string) (bool, error)
	ClearDeletingFlag(postID string) error
}
