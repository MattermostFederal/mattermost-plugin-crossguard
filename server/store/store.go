package store

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
}
