package store

import (
	"slices"

	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/pkg/errors"
)

// Client wraps the Mattermost pluginapi KV store.
type Client struct {
	client               *pluginapi.Client
	teamInitPrefix       string
	channelInitPrefix    string
	initializedTeamsKey  string
	connPromptPrefix     string
	chanConnPromptPrefix string
}

// NewKVStore creates a new KV store client.
func NewKVStore(client *pluginapi.Client, pluginID string) KVStore {
	return Client{
		client:               client,
		teamInitPrefix:       pluginID + "-teaminit-",
		channelInitPrefix:    pluginID + "-channelinit-",
		initializedTeamsKey:  pluginID + "-initialized-teams",
		connPromptPrefix:     pluginID + "-connprompt-",
		chanConnPromptPrefix: pluginID + "-chanprompt-",
	}
}

// GetTeamConnections returns the list of connection names linked to a team.
func (kv Client) GetTeamConnections(teamID string) ([]string, error) {
	var connNames []string
	err := kv.client.KV.Get(kv.teamInitPrefix+teamID, &connNames)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get team connections")
	}
	if connNames == nil {
		return []string{}, nil
	}
	return connNames, nil
}

// SetTeamConnections stores the full list of connection names for a team.
func (kv Client) SetTeamConnections(teamID string, connNames []string) error {
	_, err := kv.client.KV.Set(kv.teamInitPrefix+teamID, connNames)
	if err != nil {
		return errors.Wrap(err, "failed to set team connections")
	}
	return nil
}

// DeleteTeamConnections removes all connection links for a team.
func (kv Client) DeleteTeamConnections(teamID string) error {
	if err := kv.client.KV.Delete(kv.teamInitPrefix + teamID); err != nil {
		return errors.Wrap(err, "failed to delete team connections")
	}
	return nil
}

// IsTeamInitialized returns true if the team has at least one connection linked.
func (kv Client) IsTeamInitialized(teamID string) (bool, error) {
	conns, err := kv.GetTeamConnections(teamID)
	if err != nil {
		return false, err
	}
	return len(conns) > 0, nil
}

// AddTeamConnection atomically adds a connection name to a team's connection list.
func (kv Client) AddTeamConnection(teamID, connName string) error {
	return kv.casModifyStringList(kv.teamInitPrefix+teamID, func(names []string) ([]string, bool) {
		if slices.Contains(names, connName) {
			return names, false
		}
		return append(names, connName), true
	})
}

// RemoveTeamConnection atomically removes a connection name from a team's connection list.
func (kv Client) RemoveTeamConnection(teamID, connName string) error {
	return kv.casModifyStringList(kv.teamInitPrefix+teamID, func(names []string) ([]string, bool) {
		idx := slices.Index(names, connName)
		if idx < 0 {
			return names, false
		}
		result := make([]string, 0, len(names)-1)
		result = append(result, names[:idx]...)
		result = append(result, names[idx+1:]...)
		return result, true
	})
}

// GetInitializedTeamIDs returns the list of team IDs that have been initialized.
func (kv Client) GetInitializedTeamIDs() ([]string, error) {
	var teamIDs []string
	if err := kv.client.KV.Get(kv.initializedTeamsKey, &teamIDs); err != nil {
		return nil, errors.Wrap(err, "failed to get initialized team IDs")
	}
	if teamIDs == nil {
		return []string{}, nil
	}
	return teamIDs, nil
}

// AddInitializedTeamID atomically adds a team ID to the initialized teams list
// using compare-and-set to prevent overwrites from concurrent writes.
func (kv Client) AddInitializedTeamID(teamID string) error {
	return kv.casModifyStringList(kv.initializedTeamsKey, func(ids []string) ([]string, bool) {
		if slices.Contains(ids, teamID) {
			return ids, false
		}
		return append(ids, teamID), true
	})
}

// RemoveInitializedTeamID atomically removes a team ID from the initialized teams list.
func (kv Client) RemoveInitializedTeamID(teamID string) error {
	return kv.casModifyStringList(kv.initializedTeamsKey, func(ids []string) ([]string, bool) {
		idx := slices.Index(ids, teamID)
		if idx < 0 {
			return ids, false
		}
		result := make([]string, 0, len(ids)-1)
		result = append(result, ids[:idx]...)
		result = append(result, ids[idx+1:]...)
		return result, true
	})
}

// GetChannelConnections returns the list of connection names linked to a channel.
func (kv Client) GetChannelConnections(channelID string) ([]string, error) {
	var connNames []string
	err := kv.client.KV.Get(kv.channelInitPrefix+channelID, &connNames)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get channel connections")
	}
	if connNames == nil {
		return []string{}, nil
	}
	return connNames, nil
}

// SetChannelConnections stores the full list of connection names for a channel.
func (kv Client) SetChannelConnections(channelID string, connNames []string) error {
	_, err := kv.client.KV.Set(kv.channelInitPrefix+channelID, connNames)
	if err != nil {
		return errors.Wrap(err, "failed to set channel connections")
	}
	return nil
}

// DeleteChannelConnections removes all connection links for a channel.
func (kv Client) DeleteChannelConnections(channelID string) error {
	if err := kv.client.KV.Delete(kv.channelInitPrefix + channelID); err != nil {
		return errors.Wrap(err, "failed to delete channel connections")
	}
	return nil
}

// IsChannelInitialized returns true if the channel has at least one connection linked.
func (kv Client) IsChannelInitialized(channelID string) (bool, error) {
	conns, err := kv.GetChannelConnections(channelID)
	if err != nil {
		return false, err
	}
	return len(conns) > 0, nil
}

// AddChannelConnection atomically adds a connection name to a channel's connection list.
func (kv Client) AddChannelConnection(channelID, connName string) error {
	return kv.casModifyStringList(kv.channelInitPrefix+channelID, func(names []string) ([]string, bool) {
		if slices.Contains(names, connName) {
			return names, false
		}
		return append(names, connName), true
	})
}

// RemoveChannelConnection atomically removes a connection name from a channel's connection list.
func (kv Client) RemoveChannelConnection(channelID, connName string) error {
	return kv.casModifyStringList(kv.channelInitPrefix+channelID, func(names []string) ([]string, bool) {
		idx := slices.Index(names, connName)
		if idx < 0 {
			return names, false
		}
		result := make([]string, 0, len(names)-1)
		result = append(result, names[:idx]...)
		result = append(result, names[idx+1:]...)
		return result, true
	})
}

// postMappingKey returns the KV key for a remote-to-local post ID mapping.
func postMappingKey(connName, remotePostID string) string {
	return "pm-" + connName + "-" + remotePostID
}

// SetPostMapping stores a remote-to-local post ID mapping for a connection.
func (kv Client) SetPostMapping(connName, remotePostID, localPostID string) error {
	_, err := kv.client.KV.Set(postMappingKey(connName, remotePostID), localPostID)
	if err != nil {
		return errors.Wrap(err, "failed to set post mapping")
	}
	return nil
}

// GetPostMapping retrieves the local post ID for a remote post on a connection.
func (kv Client) GetPostMapping(connName, remotePostID string) (string, error) {
	var localPostID string
	err := kv.client.KV.Get(postMappingKey(connName, remotePostID), &localPostID)
	if err != nil {
		return "", errors.Wrap(err, "failed to get post mapping")
	}
	return localPostID, nil
}

// DeletePostMapping removes a remote-to-local post ID mapping.
func (kv Client) DeletePostMapping(connName, remotePostID string) error {
	if err := kv.client.KV.Delete(postMappingKey(connName, remotePostID)); err != nil {
		return errors.Wrap(err, "failed to delete post mapping")
	}
	return nil
}

func deletingFlagKey(postID string) string {
	return "crossguard-deleting-" + postID
}

// SetDeletingFlag marks a post as being deleted by the inbound handler.
func (kv Client) SetDeletingFlag(postID string) error {
	_, err := kv.client.KV.Set(deletingFlagKey(postID), true)
	if err != nil {
		return errors.Wrap(err, "failed to set deleting flag")
	}
	return nil
}

// IsDeletingFlagSet returns true if the post is being deleted by the inbound handler.
func (kv Client) IsDeletingFlagSet(postID string) (bool, error) {
	var set bool
	err := kv.client.KV.Get(deletingFlagKey(postID), &set)
	if err != nil {
		return false, errors.Wrap(err, "failed to get deleting flag")
	}
	return set, nil
}

// ClearDeletingFlag removes the deleting flag for a post.
func (kv Client) ClearDeletingFlag(postID string) error {
	if err := kv.client.KV.Delete(deletingFlagKey(postID)); err != nil {
		return errors.Wrap(err, "failed to clear deleting flag")
	}
	return nil
}

// GetConnectionPrompt retrieves the connection prompt state for a team+connection.
func (kv Client) GetConnectionPrompt(teamID, connName string) (*ConnectionPrompt, error) {
	var prompt ConnectionPrompt
	err := kv.client.KV.Get(kv.connPromptPrefix+teamID+"-"+connName, &prompt)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get connection prompt")
	}
	if prompt.State == "" {
		return nil, nil
	}
	return &prompt, nil
}

// SetConnectionPrompt stores the connection prompt state for a team+connection.
func (kv Client) SetConnectionPrompt(teamID, connName string, prompt *ConnectionPrompt) error {
	_, err := kv.client.KV.Set(kv.connPromptPrefix+teamID+"-"+connName, prompt)
	if err != nil {
		return errors.Wrap(err, "failed to set connection prompt")
	}
	return nil
}

// DeleteConnectionPrompt removes the connection prompt state for a team+connection.
func (kv Client) DeleteConnectionPrompt(teamID, connName string) error {
	if err := kv.client.KV.Delete(kv.connPromptPrefix + teamID + "-" + connName); err != nil {
		return errors.Wrap(err, "failed to delete connection prompt")
	}
	return nil
}

// CreateConnectionPrompt atomically creates a connection prompt only if none exists.
// Returns true if created, false if a prompt already exists for this team+connection.
func (kv Client) CreateConnectionPrompt(teamID, connName string, prompt *ConnectionPrompt) (bool, error) {
	saved, err := kv.client.KV.Set(kv.connPromptPrefix+teamID+"-"+connName, prompt, pluginapi.SetAtomic(nil))
	if err != nil {
		return false, errors.Wrap(err, "failed to create connection prompt")
	}
	return saved, nil
}

// GetChannelConnectionPrompt retrieves the connection prompt state for a channel+connection.
func (kv Client) GetChannelConnectionPrompt(channelID, connName string) (*ConnectionPrompt, error) {
	var prompt ConnectionPrompt
	err := kv.client.KV.Get(kv.chanConnPromptPrefix+channelID+"-"+connName, &prompt)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get channel connection prompt")
	}
	if prompt.State == "" {
		return nil, nil
	}
	return &prompt, nil
}

// SetChannelConnectionPrompt stores the connection prompt state for a channel+connection.
func (kv Client) SetChannelConnectionPrompt(channelID, connName string, prompt *ConnectionPrompt) error {
	_, err := kv.client.KV.Set(kv.chanConnPromptPrefix+channelID+"-"+connName, prompt)
	if err != nil {
		return errors.Wrap(err, "failed to set channel connection prompt")
	}
	return nil
}

// DeleteChannelConnectionPrompt removes the connection prompt state for a channel+connection.
func (kv Client) DeleteChannelConnectionPrompt(channelID, connName string) error {
	if err := kv.client.KV.Delete(kv.chanConnPromptPrefix + channelID + "-" + connName); err != nil {
		return errors.Wrap(err, "failed to delete channel connection prompt")
	}
	return nil
}

// CreateChannelConnectionPrompt atomically creates a channel connection prompt only if none exists.
// Returns true if created, false if a prompt already exists for this channel+connection.
func (kv Client) CreateChannelConnectionPrompt(channelID, connName string, prompt *ConnectionPrompt) (bool, error) {
	saved, err := kv.client.KV.Set(kv.chanConnPromptPrefix+channelID+"-"+connName, prompt, pluginapi.SetAtomic(nil))
	if err != nil {
		return false, errors.Wrap(err, "failed to create channel connection prompt")
	}
	return saved, nil
}

// casModifyStringList atomically modifies a string list stored in KV.
// The modify function receives the current list and returns the new list and
// whether a change was made. If no change, the function returns nil immediately.
func (kv Client) casModifyStringList(key string, modify func([]string) ([]string, bool)) error {
	const maxRetries = 5
	for range maxRetries {
		var items []string
		if err := kv.client.KV.Get(key, &items); err != nil {
			return errors.Wrap(err, "failed to read list for CAS")
		}

		newItems, changed := modify(items)
		if !changed {
			return nil
		}

		var saved bool
		var err error
		if items == nil {
			saved, err = kv.client.KV.Set(key, newItems, pluginapi.SetAtomic(nil))
		} else {
			saved, err = kv.client.KV.Set(key, newItems, pluginapi.SetAtomic(items))
		}
		if err != nil {
			return errors.Wrap(err, "failed to CAS list")
		}
		if saved {
			return nil
		}
	}
	return errors.New("failed to modify list after max retries")
}
