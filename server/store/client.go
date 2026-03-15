package store

import (
	"slices"

	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/pkg/errors"
)

// Client wraps the Mattermost pluginapi KV store.
type Client struct {
	client              *pluginapi.Client
	teamInitPrefix      string
	channelInitPrefix   string
	initializedTeamsKey string
}

// NewKVStore creates a new KV store client.
func NewKVStore(client *pluginapi.Client, pluginID string) KVStore {
	return Client{
		client:              client,
		teamInitPrefix:      pluginID + "-teaminit-",
		channelInitPrefix:   pluginID + "-channelinit-",
		initializedTeamsKey: pluginID + "-initialized-teams",
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

// GetChannelInitialized returns true if the channel has been initialized for relay.
func (kv Client) GetChannelInitialized(channelID string) (bool, error) {
	var initialized bool
	err := kv.client.KV.Get(kv.channelInitPrefix+channelID, &initialized)
	if err != nil {
		return false, errors.Wrap(err, "failed to get channel initialized flag")
	}
	return initialized, nil
}

// SetChannelInitialized marks a channel as initialized for relay.
func (kv Client) SetChannelInitialized(channelID string) error {
	_, err := kv.client.KV.Set(kv.channelInitPrefix+channelID, true)
	if err != nil {
		return errors.Wrap(err, "failed to set channel initialized flag")
	}
	return nil
}

// DeleteChannelInitialized removes the channel initialized flag.
func (kv Client) DeleteChannelInitialized(channelID string) error {
	if err := kv.client.KV.Delete(kv.channelInitPrefix + channelID); err != nil {
		return errors.Wrap(err, "failed to delete channel initialized flag")
	}
	return nil
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
