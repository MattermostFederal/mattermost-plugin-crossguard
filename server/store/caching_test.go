package store

import (
	"fmt"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockKVStore struct {
	getTeamInitializedFn    func(string) (bool, error)
	setTeamInitializedFn    func(string) error
	getInitializedTeamIDsFn func() ([]string, error)
	addInitializedTeamIDFn  func(string) error
}

func (m *mockKVStore) GetTeamInitialized(teamID string) (bool, error) {
	if m.getTeamInitializedFn != nil {
		return m.getTeamInitializedFn(teamID)
	}
	return false, nil
}

func (m *mockKVStore) SetTeamInitialized(teamID string) error {
	if m.setTeamInitializedFn != nil {
		return m.setTeamInitializedFn(teamID)
	}
	return nil
}

func (m *mockKVStore) GetInitializedTeamIDs() ([]string, error) {
	if m.getInitializedTeamIDsFn != nil {
		return m.getInitializedTeamIDsFn()
	}
	return []string{}, nil
}

func (m *mockKVStore) AddInitializedTeamID(teamID string) error {
	if m.addInitializedTeamIDFn != nil {
		return m.addInitializedTeamIDFn(teamID)
	}
	return nil
}

func newTestCaching(inner *mockKVStore) (*CachingKVStore, *plugintest.API) {
	api := &plugintest.API{}
	api.On("PublishPluginClusterEvent", mock.Anything, mock.Anything).Return(nil)
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	c := NewCachingKVStore(inner, api)
	return c, api
}

func TestGetTeamInitialized_CacheHitMiss(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getTeamInitializedFn: func(teamID string) (bool, error) {
			calls++
			return true, nil
		},
	}
	c, _ := newTestCaching(inner)

	val, err := c.GetTeamInitialized("team1")
	require.NoError(t, err)
	assert.True(t, val)
	assert.Equal(t, 1, calls)

	val2, err := c.GetTeamInitialized("team1")
	require.NoError(t, err)
	assert.True(t, val2)
	assert.Equal(t, 1, calls)
}

func TestGetTeamInitialized_CachesFalse(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getTeamInitializedFn: func(teamID string) (bool, error) {
			calls++
			return false, nil
		},
	}
	c, _ := newTestCaching(inner)

	val, err := c.GetTeamInitialized("team1")
	require.NoError(t, err)
	assert.False(t, val)

	val, err = c.GetTeamInitialized("team1")
	require.NoError(t, err)
	assert.False(t, val)
	assert.Equal(t, 1, calls)
}

func TestGetTeamInitialized_ErrorNotCached(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getTeamInitializedFn: func(teamID string) (bool, error) {
			calls++
			return false, fmt.Errorf("kv store unavailable")
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetTeamInitialized("team1")
	require.Error(t, err)

	_, err = c.GetTeamInitialized("team1")
	require.Error(t, err)
	assert.Equal(t, 2, calls)
}

func TestSetTeamInitialized_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getTeamInitializedFn: func(teamID string) (bool, error) {
			getCalls++
			return true, nil
		},
		setTeamInitializedFn: func(teamID string) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetTeamInitialized("team1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.SetTeamInitialized("team1")
	require.NoError(t, err)

	_, err = c.GetTeamInitialized("team1")
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestSetTeamInitialized_ErrorDoesNotInvalidate(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getTeamInitializedFn: func(teamID string) (bool, error) {
			getCalls++
			return false, nil
		},
		setTeamInitializedFn: func(teamID string) error {
			return fmt.Errorf("write failed")
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetTeamInitialized("team1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.SetTeamInitialized("team1")
	require.Error(t, err)

	_, err = c.GetTeamInitialized("team1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)
}

func TestSetTeamInitialized_PublishesClusterEvent(t *testing.T) {
	inner := &mockKVStore{
		setTeamInitializedFn: func(teamID string) error { return nil },
	}
	api := &plugintest.API{}
	api.On("PublishPluginClusterEvent", model.PluginClusterEvent{
		Id:   ClusterEventInvalidateTeamInit,
		Data: []byte("team1"),
	}, model.PluginClusterEventSendOptions{
		SendType: model.PluginClusterEventSendTypeBestEffort,
	}).Return(nil).Once()

	c := NewCachingKVStore(inner, api)

	err := c.SetTeamInitialized("team1")
	require.NoError(t, err)

	api.AssertExpectations(t)
}

func TestHandleClusterEvent_InvalidatesTeamInit(t *testing.T) {
	inner := &mockKVStore{
		getTeamInitializedFn: func(teamID string) (bool, error) {
			return true, nil
		},
	}
	c, _ := newTestCaching(inner)

	_, _ = c.GetTeamInitialized("team1")
	assert.Equal(t, 1, c.teamInitCache.Len())

	c.HandleClusterEvent(model.PluginClusterEvent{
		Id:   ClusterEventInvalidateTeamInit,
		Data: []byte("team1"),
	})
	assert.Equal(t, 0, c.teamInitCache.Len())
}

func TestGetInitializedTeamIDs_CacheHitMiss(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getInitializedTeamIDsFn: func() ([]string, error) {
			calls++
			return []string{"team1", "team2"}, nil
		},
	}
	c, _ := newTestCaching(inner)

	ids, err := c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, []string{"team1", "team2"}, ids)
	assert.Equal(t, 1, calls)

	ids2, err := c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, []string{"team1", "team2"}, ids2)
	assert.Equal(t, 1, calls)
}

func TestGetInitializedTeamIDs_ReturnsDeepCopy(t *testing.T) {
	inner := &mockKVStore{
		getInitializedTeamIDsFn: func() ([]string, error) {
			return []string{"team1"}, nil
		},
	}
	c, _ := newTestCaching(inner)

	ids1, err := c.GetInitializedTeamIDs()
	require.NoError(t, err)
	_ = append(ids1, "team2")

	ids2, err := c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, []string{"team1"}, ids2)
}

func TestGetInitializedTeamIDs_ErrorNotCached(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getInitializedTeamIDsFn: func() ([]string, error) {
			calls++
			return nil, fmt.Errorf("kv store unavailable")
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetInitializedTeamIDs()
	require.Error(t, err)

	_, err = c.GetInitializedTeamIDs()
	require.Error(t, err)
	assert.Equal(t, 2, calls)
}

func TestAddInitializedTeamID_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getInitializedTeamIDsFn: func() ([]string, error) {
			getCalls++
			return []string{"team1"}, nil
		},
		addInitializedTeamIDFn: func(teamID string) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.AddInitializedTeamID("team2")
	require.NoError(t, err)

	_, err = c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestAddInitializedTeamID_ErrorDoesNotInvalidate(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getInitializedTeamIDsFn: func() ([]string, error) {
			getCalls++
			return []string{"team1"}, nil
		},
		addInitializedTeamIDFn: func(teamID string) error {
			return fmt.Errorf("write failed")
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.AddInitializedTeamID("team2")
	require.Error(t, err)

	_, err = c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)
}

func TestHandleClusterEvent_InvalidatesInitTeams(t *testing.T) {
	inner := &mockKVStore{
		getInitializedTeamIDsFn: func() ([]string, error) {
			return []string{"team1"}, nil
		},
	}
	c, _ := newTestCaching(inner)

	_, _ = c.GetInitializedTeamIDs()
	assert.Equal(t, 1, c.initTeamsCache.Len())

	c.HandleClusterEvent(model.PluginClusterEvent{
		Id:   ClusterEventInvalidateInitTeams,
		Data: []byte(initTeamsCacheKey),
	})
	assert.Equal(t, 0, c.initTeamsCache.Len())
}
