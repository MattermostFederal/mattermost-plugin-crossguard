package store

import (
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
)

const (
	cacheTeamInitSize     = 64
	cacheChannelInitSize  = 1024
	cacheRewriteIndexSize = 256
	cacheTTL              = 15 * time.Minute

	// Sentinel value cached when no rewrite rule exists for a (conn, team) pair.
	// Avoids repeated KV store lookups for non-rewritten teams.
	rewriteIndexNegative = "<none>"
)

const (
	ClusterEventInvalidateTeamInit     = "cache_inv_teaminit"
	ClusterEventInvalidateInitTeams    = "cache_inv_initteams"
	ClusterEventInvalidateChannelInit  = "cache_inv_chaninit"
	ClusterEventInvalidateRewriteIndex = "cache_inv_rwindex"
)

// CachingKVStore wraps a KVStore with per-entity LRU caches and publishes
// cluster events on writes for cross-node cache invalidation.
type CachingKVStore struct {
	KVStore
	api plugin.API

	teamInitCache     *expirable.LRU[string, []TeamConnection]
	channelInitCache  *expirable.LRU[string, []TeamConnection]
	initTeamsCache    *expirable.LRU[string, []string]
	rewriteIndexCache *expirable.LRU[string, string]
}

// NewCachingKVStore creates a caching wrapper around the given KVStore.
func NewCachingKVStore(inner KVStore, api plugin.API) *CachingKVStore {
	return &CachingKVStore{
		KVStore:           inner,
		api:               api,
		teamInitCache:     expirable.NewLRU[string, []TeamConnection](cacheTeamInitSize, nil, cacheTTL),
		channelInitCache:  expirable.NewLRU[string, []TeamConnection](cacheChannelInitSize, nil, cacheTTL),
		initTeamsCache:    expirable.NewLRU[string, []string](1, nil, cacheTTL),
		rewriteIndexCache: expirable.NewLRU[string, string](cacheRewriteIndexSize, nil, cacheTTL),
	}
}

// GetTeamConnections checks the cache first, then falls back to the inner store.
func (c *CachingKVStore) GetTeamConnections(teamID string) ([]TeamConnection, error) {
	if val, ok := c.teamInitCache.Get(teamID); ok {
		result := make([]TeamConnection, len(val))
		copy(result, val)
		return result, nil
	}
	conns, err := c.KVStore.GetTeamConnections(teamID)
	if err != nil {
		return nil, err
	}
	cached := make([]TeamConnection, len(conns))
	copy(cached, conns)
	c.teamInitCache.Add(teamID, cached)
	return conns, nil
}

// SetTeamConnections writes to the inner store and invalidates the cache.
func (c *CachingKVStore) SetTeamConnections(teamID string, conns []TeamConnection) error {
	if err := c.KVStore.SetTeamConnections(teamID, conns); err != nil {
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
func (c *CachingKVStore) AddTeamConnection(teamID string, conn TeamConnection) error {
	if err := c.KVStore.AddTeamConnection(teamID, conn); err != nil {
		return err
	}
	c.invalidate(ClusterEventInvalidateTeamInit, teamID)
	return nil
}

// RemoveTeamConnection removes a connection and invalidates the cache.
func (c *CachingKVStore) RemoveTeamConnection(teamID string, conn TeamConnection) error {
	if err := c.KVStore.RemoveTeamConnection(teamID, conn); err != nil {
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

// GetChannelConnections checks the cache first, then falls back to the inner store.
func (c *CachingKVStore) GetChannelConnections(channelID string) ([]TeamConnection, error) {
	if val, ok := c.channelInitCache.Get(channelID); ok {
		result := make([]TeamConnection, len(val))
		copy(result, val)
		return result, nil
	}
	conns, err := c.KVStore.GetChannelConnections(channelID)
	if err != nil {
		return nil, err
	}
	cached := make([]TeamConnection, len(conns))
	copy(cached, conns)
	c.channelInitCache.Add(channelID, cached)
	return conns, nil
}

// SetChannelConnections writes to the inner store and invalidates the cache.
func (c *CachingKVStore) SetChannelConnections(channelID string, conns []TeamConnection) error {
	if err := c.KVStore.SetChannelConnections(channelID, conns); err != nil {
		return err
	}
	c.invalidate(ClusterEventInvalidateChannelInit, channelID)
	return nil
}

// DeleteChannelConnections removes channel connections and invalidates the cache.
func (c *CachingKVStore) DeleteChannelConnections(channelID string) error {
	if err := c.KVStore.DeleteChannelConnections(channelID); err != nil {
		return err
	}
	c.invalidate(ClusterEventInvalidateChannelInit, channelID)
	return nil
}

// IsChannelInitialized checks cache for connections, returns true if any exist.
func (c *CachingKVStore) IsChannelInitialized(channelID string) (bool, error) {
	conns, err := c.GetChannelConnections(channelID)
	if err != nil {
		return false, err
	}
	return len(conns) > 0, nil
}

// AddChannelConnection adds a connection and invalidates the cache.
func (c *CachingKVStore) AddChannelConnection(channelID string, conn TeamConnection) error {
	if err := c.KVStore.AddChannelConnection(channelID, conn); err != nil {
		return err
	}
	c.invalidate(ClusterEventInvalidateChannelInit, channelID)
	return nil
}

// RemoveChannelConnection removes a connection and invalidates the cache.
func (c *CachingKVStore) RemoveChannelConnection(channelID string, conn TeamConnection) error {
	if err := c.KVStore.RemoveChannelConnection(channelID, conn); err != nil {
		return err
	}
	c.invalidate(ClusterEventInvalidateChannelInit, channelID)
	return nil
}

// GetTeamRewriteIndex checks the cache first, then falls back to the inner store.
func (c *CachingKVStore) GetTeamRewriteIndex(connName, remoteTeamName string) (string, error) {
	cacheKey := connName + "/" + remoteTeamName
	if val, ok := c.rewriteIndexCache.Get(cacheKey); ok {
		if val == rewriteIndexNegative {
			return "", nil
		}
		return val, nil
	}
	localTeamID, err := c.KVStore.GetTeamRewriteIndex(connName, remoteTeamName)
	if err != nil {
		return "", err
	}
	if localTeamID == "" {
		c.rewriteIndexCache.Add(cacheKey, rewriteIndexNegative)
	} else {
		c.rewriteIndexCache.Add(cacheKey, localTeamID)
	}
	return localTeamID, nil
}

// SetTeamRewriteIndex writes to the inner store and invalidates the cache.
func (c *CachingKVStore) SetTeamRewriteIndex(connName, remoteTeamName, localTeamID string) error {
	if err := c.KVStore.SetTeamRewriteIndex(connName, remoteTeamName, localTeamID); err != nil {
		return err
	}
	cacheKey := connName + "/" + remoteTeamName
	c.invalidate(ClusterEventInvalidateRewriteIndex, cacheKey)
	return nil
}

// DeleteTeamRewriteIndex removes the rewrite index and invalidates the cache.
func (c *CachingKVStore) DeleteTeamRewriteIndex(connName, remoteTeamName string) error {
	if err := c.KVStore.DeleteTeamRewriteIndex(connName, remoteTeamName); err != nil {
		return err
	}
	cacheKey := connName + "/" + remoteTeamName
	c.invalidate(ClusterEventInvalidateRewriteIndex, cacheKey)
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
	case ClusterEventInvalidateRewriteIndex:
		c.rewriteIndexCache.Remove(key)
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
			"error_code", errcode.StoreCachePublishInvalidationFailed,
			"event", eventID, "key", key, "error", err.Error())
	}
}
