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

// GetTeamInitialized returns true if the team has been initialized.
func (kv Client) GetTeamInitialized(teamID string) (bool, error) {
	var initialized bool
	err := kv.client.KV.Get(kv.teamInitPrefix+teamID, &initialized)
	if err != nil {
		return false, errors.Wrap(err, "failed to get team initialized flag")
	}
	return initialized, nil
}

// SetTeamInitialized marks a team as having been initialized.
func (kv Client) SetTeamInitialized(teamID string) error {
	_, err := kv.client.KV.Set(kv.teamInitPrefix+teamID, true)
	if err != nil {
		return errors.Wrap(err, "failed to set team initialized flag")
	}
	return nil
}

// DeleteTeamInitialized removes the team initialized flag.
func (kv Client) DeleteTeamInitialized(teamID string) error {
	if err := kv.client.KV.Delete(kv.teamInitPrefix + teamID); err != nil {
		return errors.Wrap(err, "failed to delete team initialized flag")
	}
	return nil
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
