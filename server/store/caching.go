package store

import (
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

const (
	cacheTeamInitSize = 64
	cacheTTL          = 10 * time.Minute
)

const (
	ClusterEventInvalidateTeamInit  = "cache_inv_teaminit"
	ClusterEventInvalidateInitTeams = "cache_inv_initteams"
)

// CachingKVStore wraps a KVStore with per-entity LRU caches and publishes
// cluster events on writes for cross-node cache invalidation.
type CachingKVStore struct {
	KVStore
	api plugin.API

	teamInitCache  *expirable.LRU[string, bool]
	initTeamsCache *expirable.LRU[string, []string]
}

// NewCachingKVStore creates a caching wrapper around the given KVStore.
func NewCachingKVStore(inner KVStore, api plugin.API) *CachingKVStore {
	return &CachingKVStore{
		KVStore:        inner,
		api:            api,
		teamInitCache:  expirable.NewLRU[string, bool](cacheTeamInitSize, nil, cacheTTL),
		initTeamsCache: expirable.NewLRU[string, []string](1, nil, cacheTTL),
	}
}

// GetTeamInitialized checks the cache first, then falls back to the inner store.
func (c *CachingKVStore) GetTeamInitialized(teamID string) (bool, error) {
	if val, ok := c.teamInitCache.Get(teamID); ok {
		return val, nil
	}
	initialized, err := c.KVStore.GetTeamInitialized(teamID)
	if err != nil {
		return false, err
	}
	c.teamInitCache.Add(teamID, initialized)
	return initialized, nil
}

// SetTeamInitialized writes to the inner store and invalidates the cache.
func (c *CachingKVStore) SetTeamInitialized(teamID string) error {
	if err := c.KVStore.SetTeamInitialized(teamID); err != nil {
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
