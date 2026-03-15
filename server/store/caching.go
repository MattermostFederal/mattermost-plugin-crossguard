package store

import (
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

const (
	cacheTeamInitSize    = 64
	cacheChannelInitSize = 64
	cacheTTL             = 10 * time.Minute
)

const (
	ClusterEventInvalidateTeamInit    = "cache_inv_teaminit"
	ClusterEventInvalidateInitTeams   = "cache_inv_initteams"
	ClusterEventInvalidateChannelInit = "cache_inv_chaninit"
)

// CachingKVStore wraps a KVStore with per-entity LRU caches and publishes
// cluster events on writes for cross-node cache invalidation.
type CachingKVStore struct {
	KVStore
	api plugin.API

	teamInitCache    *expirable.LRU[string, []string]
	channelInitCache *expirable.LRU[string, bool]
	initTeamsCache   *expirable.LRU[string, []string]
}

// NewCachingKVStore creates a caching wrapper around the given KVStore.
func NewCachingKVStore(inner KVStore, api plugin.API) *CachingKVStore {
	return &CachingKVStore{
		KVStore:          inner,
		api:              api,
		teamInitCache:    expirable.NewLRU[string, []string](cacheTeamInitSize, nil, cacheTTL),
		channelInitCache: expirable.NewLRU[string, bool](cacheChannelInitSize, nil, cacheTTL),
		initTeamsCache:   expirable.NewLRU[string, []string](1, nil, cacheTTL),
	}
}

// GetTeamConnections checks the cache first, then falls back to the inner store.
func (c *CachingKVStore) GetTeamConnections(teamID string) ([]string, error) {
	if val, ok := c.teamInitCache.Get(teamID); ok {
		result := make([]string, len(val))
		copy(result, val)
		return result, nil
	}
	conns, err := c.KVStore.GetTeamConnections(teamID)
	if err != nil {
		return nil, err
	}
	cached := make([]string, len(conns))
	copy(cached, conns)
	c.teamInitCache.Add(teamID, cached)
	return conns, nil
}

// SetTeamConnections writes to the inner store and invalidates the cache.
func (c *CachingKVStore) SetTeamConnections(teamID string, connNames []string) error {
	if err := c.KVStore.SetTeamConnections(teamID, connNames); err != nil {
		return err
	}
	c.invalidate(ClusterEventInvalidateTeamInit, teamID)
	return nil
}

// DeleteTeamConnections removes team connections and invalidates the cache.
func (c *CachingKVStore) DeleteTeamConnections(teamID string) error {
	if err := c.KVStore.DeleteTeamConnections(teamID); err != nil {
		return err
	}
	c.invalidate(ClusterEventInvalidateTeamInit, teamID)
	return nil
}

// IsTeamInitialized checks cache for connections, returns true if any exist.
func (c *CachingKVStore) IsTeamInitialized(teamID string) (bool, error) {
	conns, err := c.GetTeamConnections(teamID)
	if err != nil {
		return false, err
	}
	return len(conns) > 0, nil
}

// AddTeamConnection adds a connection and invalidates the cache.
func (c *CachingKVStore) AddTeamConnection(teamID, connName string) error {
	if err := c.KVStore.AddTeamConnection(teamID, connName); err != nil {
		return err
	}
	c.invalidate(ClusterEventInvalidateTeamInit, teamID)
	return nil
}

// RemoveTeamConnection removes a connection and invalidates the cache.
func (c *CachingKVStore) RemoveTeamConnection(teamID, connName string) error {
	if err := c.KVStore.RemoveTeamConnection(teamID, connName); err != nil {
		return err
	}
	c.invalidate(ClusterEventInvalidateTeamInit, teamID)
	return nil
}

const initTeamsCacheKey = "_all"

// GetInitializedTeamIDs checks the cache first, then falls back to the inner store.
func (c *CachingKVStore) GetInitializedTeamIDs() ([]string, error) {
	if val, ok := c.initTeamsCache.Get(initTeamsCacheKey); ok {
		result := make([]string, len(val))
		copy(result, val)
		return result, nil
	}
	ids, err := c.KVStore.GetInitializedTeamIDs()
	if err != nil {
		return nil, err
	}
	cached := make([]string, len(ids))
	copy(cached, ids)
	c.initTeamsCache.Add(initTeamsCacheKey, cached)
	return ids, nil
}

// AddInitializedTeamID writes to the inner store and invalidates the cache.
func (c *CachingKVStore) AddInitializedTeamID(teamID string) error {
	if err := c.KVStore.AddInitializedTeamID(teamID); err != nil {
		return err
	}
	c.invalidate(ClusterEventInvalidateInitTeams, initTeamsCacheKey)
	return nil
}

// RemoveInitializedTeamID removes a team from the list and invalidates the cache.
func (c *CachingKVStore) RemoveInitializedTeamID(teamID string) error {
	if err := c.KVStore.RemoveInitializedTeamID(teamID); err != nil {
		return err
	}
	c.invalidate(ClusterEventInvalidateInitTeams, initTeamsCacheKey)
	return nil
}

// GetChannelInitialized checks the cache first, then falls back to the inner store.
func (c *CachingKVStore) GetChannelInitialized(channelID string) (bool, error) {
	if val, ok := c.channelInitCache.Get(channelID); ok {
		return val, nil
	}
	initialized, err := c.KVStore.GetChannelInitialized(channelID)
	if err != nil {
		return false, err
	}
	c.channelInitCache.Add(channelID, initialized)
	return initialized, nil
}

// SetChannelInitialized writes to the inner store and invalidates the cache.
func (c *CachingKVStore) SetChannelInitialized(channelID string) error {
	if err := c.KVStore.SetChannelInitialized(channelID); err != nil {
		return err
	}
	c.invalidate(ClusterEventInvalidateChannelInit, channelID)
	return nil
}

// DeleteChannelInitialized removes the channel init flag and invalidates the cache.
func (c *CachingKVStore) DeleteChannelInitialized(channelID string) error {
	if err := c.KVStore.DeleteChannelInitialized(channelID); err != nil {
		return err
	}
	c.invalidate(ClusterEventInvalidateChannelInit, channelID)
	return nil
}

// HandleClusterEvent processes cache invalidation events from other nodes.
func (c *CachingKVStore) HandleClusterEvent(ev model.PluginClusterEvent) {
	c.removeFromCache(ev.Id, string(ev.Data))
}

func (c *CachingKVStore) removeFromCache(eventID, key string) {
	switch eventID {
	case ClusterEventInvalidateTeamInit:
		c.teamInitCache.Remove(key)
	case ClusterEventInvalidateInitTeams:
		c.initTeamsCache.Remove(key)
	case ClusterEventInvalidateChannelInit:
		c.channelInitCache.Remove(key)
	}
}

func (c *CachingKVStore) invalidate(eventID, key string) {
	c.removeFromCache(eventID, key)

	if err := c.api.PublishPluginClusterEvent(model.PluginClusterEvent{
		Id:   eventID,
		Data: []byte(key),
	}, model.PluginClusterEventSendOptions{
		SendType: model.PluginClusterEventSendTypeBestEffort,
	}); err != nil {
		c.api.LogWarn("Failed to publish cache invalidation event",
			"event", eventID, "key", key, "error", err.Error())
	}
}
