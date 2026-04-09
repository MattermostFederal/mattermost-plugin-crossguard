package main

import (
	"context"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

func TestOnDeactivate_NilCancel(t *testing.T) {
	p := &Plugin{}
	// cancel is nil, should not panic
	err := p.OnDeactivate()
	assert.NoError(t, err)
}

func TestOnDeactivate_CancelsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Plugin{}
	p.ctx = ctx
	p.cancel = cancel

	err := p.OnDeactivate()
	assert.NoError(t, err)
	// Verify context was cancelled
	assert.Error(t, ctx.Err())
}

func TestOnPluginClusterEvent_CachingStore(t *testing.T) {
	api := &plugintest.API{}
	api.On("LogWarn", "Unexpected cluster event", "id", "unknown-event").Maybe()
	p := &Plugin{}
	p.SetAPI(api)

	inner := &store.Client{}
	caching := store.NewCachingKVStore(inner, api)
	p.kvstore = caching

	// Should not panic, and should delegate to CachingKVStore.HandleClusterEvent
	p.OnPluginClusterEvent(context.Background(), model.PluginClusterEvent{
		Id:   "unknown-event",
		Data: []byte("test"),
	})
}

func TestOnPluginClusterEvent_NonCachingStore(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)

	// Use a non-caching store (mockKVStore via flexibleKVStore wrapper)
	kvs := &flexibleKVStore{testKVStore: newTestKVStore()}
	p.kvstore = kvs

	// Should not panic when kvstore is not *CachingKVStore
	p.OnPluginClusterEvent(context.Background(), model.PluginClusterEvent{
		Id:   "test-event",
		Data: []byte("data"),
	})
}
