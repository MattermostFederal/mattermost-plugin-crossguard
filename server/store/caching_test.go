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
	getTeamConnectionsFn       func(string) ([]string, error)
	setTeamConnectionsFn       func(string, []string) error
	deleteTeamConnectionsFn    func(string) error
	isTeamInitializedFn        func(string) (bool, error)
	addTeamConnectionFn        func(string, string) error
	removeTeamConnectionFn     func(string, string) error
	getInitializedTeamIDsFn    func() ([]string, error)
	addInitializedTeamIDFn     func(string) error
	removeInitializedTeamIDFn  func(string) error
	getChannelConnectionsFn    func(string) ([]string, error)
	setChannelConnectionsFn    func(string, []string) error
	deleteChannelConnectionsFn func(string) error
	isChannelInitializedFn     func(string) (bool, error)
	addChannelConnectionFn     func(string, string) error
	removeChannelConnectionFn  func(string, string) error
	setPostMappingFn           func(string, string, string) error
	getPostMappingFn           func(string, string) (string, error)
	deletePostMappingFn        func(string, string) error
	setDeletingFlagFn          func(string) error
	isDeletingFlagSetFn        func(string) (bool, error)
	clearDeletingFlagFn        func(string) error
}

func (m *mockKVStore) GetTeamConnections(teamID string) ([]string, error) {
	if m.getTeamConnectionsFn != nil {
		return m.getTeamConnectionsFn(teamID)
	}
	return []string{}, nil
}

func (m *mockKVStore) SetTeamConnections(teamID string, connNames []string) error {
	if m.setTeamConnectionsFn != nil {
		return m.setTeamConnectionsFn(teamID, connNames)
	}
	return nil
}

func (m *mockKVStore) DeleteTeamConnections(teamID string) error {
	if m.deleteTeamConnectionsFn != nil {
		return m.deleteTeamConnectionsFn(teamID)
	}
	return nil
}

func (m *mockKVStore) IsTeamInitialized(teamID string) (bool, error) {
	if m.isTeamInitializedFn != nil {
		return m.isTeamInitializedFn(teamID)
	}
	return false, nil
}

func (m *mockKVStore) AddTeamConnection(teamID, connName string) error {
	if m.addTeamConnectionFn != nil {
		return m.addTeamConnectionFn(teamID, connName)
	}
	return nil
}

func (m *mockKVStore) RemoveTeamConnection(teamID, connName string) error {
	if m.removeTeamConnectionFn != nil {
		return m.removeTeamConnectionFn(teamID, connName)
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

func (m *mockKVStore) RemoveInitializedTeamID(teamID string) error {
	if m.removeInitializedTeamIDFn != nil {
		return m.removeInitializedTeamIDFn(teamID)
	}
	return nil
}

func (m *mockKVStore) GetChannelConnections(channelID string) ([]string, error) {
	if m.getChannelConnectionsFn != nil {
		return m.getChannelConnectionsFn(channelID)
	}
	return []string{}, nil
}

func (m *mockKVStore) SetChannelConnections(channelID string, connNames []string) error {
	if m.setChannelConnectionsFn != nil {
		return m.setChannelConnectionsFn(channelID, connNames)
	}
	return nil
}

func (m *mockKVStore) DeleteChannelConnections(channelID string) error {
	if m.deleteChannelConnectionsFn != nil {
		return m.deleteChannelConnectionsFn(channelID)
	}
	return nil
}

func (m *mockKVStore) IsChannelInitialized(channelID string) (bool, error) {
	if m.isChannelInitializedFn != nil {
		return m.isChannelInitializedFn(channelID)
	}
	return false, nil
}

func (m *mockKVStore) AddChannelConnection(channelID, connName string) error {
	if m.addChannelConnectionFn != nil {
		return m.addChannelConnectionFn(channelID, connName)
	}
	return nil
}

func (m *mockKVStore) RemoveChannelConnection(channelID, connName string) error {
	if m.removeChannelConnectionFn != nil {
		return m.removeChannelConnectionFn(channelID, connName)
	}
	return nil
}

func (m *mockKVStore) SetPostMapping(connName, remotePostID, localPostID string) error {
	if m.setPostMappingFn != nil {
		return m.setPostMappingFn(connName, remotePostID, localPostID)
	}
	return nil
}

func (m *mockKVStore) GetPostMapping(connName, remotePostID string) (string, error) {
	if m.getPostMappingFn != nil {
		return m.getPostMappingFn(connName, remotePostID)
	}
	return "", nil
}

func (m *mockKVStore) DeletePostMapping(connName, remotePostID string) error {
	if m.deletePostMappingFn != nil {
		return m.deletePostMappingFn(connName, remotePostID)
	}
	return nil
}

func (m *mockKVStore) SetDeletingFlag(postID string) error {
	if m.setDeletingFlagFn != nil {
		return m.setDeletingFlagFn(postID)
	}
	return nil
}

func (m *mockKVStore) IsDeletingFlagSet(postID string) (bool, error) {
	if m.isDeletingFlagSetFn != nil {
		return m.isDeletingFlagSetFn(postID)
	}
	return false, nil
}

func (m *mockKVStore) ClearDeletingFlag(postID string) error {
	if m.clearDeletingFlagFn != nil {
		return m.clearDeletingFlagFn(postID)
	}
	return nil
}

func (m *mockKVStore) GetConnectionPrompt(teamID, connName string) (*ConnectionPrompt, error) {
	return nil, nil
}

func (m *mockKVStore) SetConnectionPrompt(teamID, connName string, prompt *ConnectionPrompt) error {
	return nil
}

func (m *mockKVStore) DeleteConnectionPrompt(teamID, connName string) error {
	return nil
}

func (m *mockKVStore) CreateConnectionPrompt(teamID, connName string, prompt *ConnectionPrompt) (bool, error) {
	return true, nil
}

func (m *mockKVStore) GetChannelConnectionPrompt(channelID, connName string) (*ConnectionPrompt, error) {
	return nil, nil
}

func (m *mockKVStore) SetChannelConnectionPrompt(channelID, connName string, prompt *ConnectionPrompt) error {
	return nil
}

func (m *mockKVStore) DeleteChannelConnectionPrompt(channelID, connName string) error {
	return nil
}

func (m *mockKVStore) CreateChannelConnectionPrompt(channelID, connName string, prompt *ConnectionPrompt) (bool, error) {
	return true, nil
}

func newTestCaching(inner *mockKVStore) (*CachingKVStore, *plugintest.API) {
	api := &plugintest.API{}
	api.On("PublishPluginClusterEvent", mock.Anything, mock.Anything).Return(nil)
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	c := NewCachingKVStore(inner, api)
	return c, api
}

func TestGetTeamConnections_CacheHitMiss(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]string, error) {
			calls++
			return []string{"outbound:a"}, nil
		},
	}
	c, _ := newTestCaching(inner)

	val, err := c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, []string{"outbound:a"}, val)
	assert.Equal(t, 1, calls)

	val2, err := c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, []string{"outbound:a"}, val2)
	assert.Equal(t, 1, calls)
}

func TestGetTeamConnections_CachesEmpty(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]string, error) {
			calls++
			return []string{}, nil
		},
	}
	c, _ := newTestCaching(inner)

	val, err := c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Empty(t, val)

	val, err = c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Empty(t, val)
	assert.Equal(t, 1, calls)
}

func TestGetTeamConnections_ErrorNotCached(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]string, error) {
			calls++
			return nil, fmt.Errorf("kv store unavailable")
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetTeamConnections("team1")
	require.Error(t, err)

	_, err = c.GetTeamConnections("team1")
	require.Error(t, err)
	assert.Equal(t, 2, calls)
}

func TestAddTeamConnection_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]string, error) {
			getCalls++
			return []string{"outbound:a"}, nil
		},
		addTeamConnectionFn: func(teamID, connName string) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.AddTeamConnection("team1", "inbound:a")
	require.NoError(t, err)

	_, err = c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestAddTeamConnection_ErrorDoesNotInvalidate(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]string, error) {
			getCalls++
			return []string{}, nil
		},
		addTeamConnectionFn: func(teamID, connName string) error {
			return fmt.Errorf("write failed")
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.AddTeamConnection("team1", "outbound:a")
	require.Error(t, err)

	_, err = c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)
}

func TestAddTeamConnection_PublishesClusterEvent(t *testing.T) {
	inner := &mockKVStore{
		addTeamConnectionFn: func(teamID, connName string) error { return nil },
	}
	api := &plugintest.API{}
	api.On("PublishPluginClusterEvent", model.PluginClusterEvent{
		Id:   ClusterEventInvalidateTeamInit,
		Data: []byte("team1"),
	}, model.PluginClusterEventSendOptions{
		SendType: model.PluginClusterEventSendTypeBestEffort,
	}).Return(nil).Once()

	c := NewCachingKVStore(inner, api)

	err := c.AddTeamConnection("team1", "outbound:a")
	require.NoError(t, err)

	api.AssertExpectations(t)
}

func TestHandleClusterEvent_InvalidatesTeamInit(t *testing.T) {
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]string, error) {
			return []string{"outbound:a"}, nil
		},
	}
	c, _ := newTestCaching(inner)

	_, _ = c.GetTeamConnections("team1")
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

func TestDeleteTeamConnections_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]string, error) {
			getCalls++
			return []string{"outbound:a"}, nil
		},
		deleteTeamConnectionsFn: func(teamID string) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.DeleteTeamConnections("team1")
	require.NoError(t, err)

	_, err = c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestRemoveTeamConnection_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]string, error) {
			getCalls++
			return []string{"outbound:a"}, nil
		},
		removeTeamConnectionFn: func(teamID, connName string) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.RemoveTeamConnection("team1", "outbound:a")
	require.NoError(t, err)

	_, err = c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestRemoveInitializedTeamID_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getInitializedTeamIDsFn: func() ([]string, error) {
			getCalls++
			return []string{"team1"}, nil
		},
		removeInitializedTeamIDFn: func(teamID string) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.RemoveInitializedTeamID("team1")
	require.NoError(t, err)

	_, err = c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestGetChannelConnections_CacheHitMiss(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]string, error) {
			calls++
			return []string{"outbound:a"}, nil
		},
	}
	c, _ := newTestCaching(inner)

	val, err := c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, []string{"outbound:a"}, val)
	assert.Equal(t, 1, calls)

	val2, err := c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, []string{"outbound:a"}, val2)
	assert.Equal(t, 1, calls)
}

func TestGetChannelConnections_CachesEmpty(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]string, error) {
			calls++
			return []string{}, nil
		},
	}
	c, _ := newTestCaching(inner)

	val, err := c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Empty(t, val)

	val, err = c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Empty(t, val)
	assert.Equal(t, 1, calls)
}

func TestGetChannelConnections_ErrorNotCached(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]string, error) {
			calls++
			return nil, fmt.Errorf("kv store unavailable")
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetChannelConnections("chan1")
	require.Error(t, err)

	_, err = c.GetChannelConnections("chan1")
	require.Error(t, err)
	assert.Equal(t, 2, calls)
}

func TestAddChannelConnection_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]string, error) {
			getCalls++
			return []string{"outbound:a"}, nil
		},
		addChannelConnectionFn: func(channelID, connName string) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.AddChannelConnection("chan1", "inbound:a")
	require.NoError(t, err)

	_, err = c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestAddChannelConnection_ErrorDoesNotInvalidate(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]string, error) {
			getCalls++
			return []string{}, nil
		},
		addChannelConnectionFn: func(channelID, connName string) error {
			return fmt.Errorf("write failed")
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.AddChannelConnection("chan1", "outbound:a")
	require.Error(t, err)

	_, err = c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)
}

func TestAddChannelConnection_PublishesClusterEvent(t *testing.T) {
	inner := &mockKVStore{
		addChannelConnectionFn: func(channelID, connName string) error { return nil },
	}
	api := &plugintest.API{}
	api.On("PublishPluginClusterEvent", model.PluginClusterEvent{
		Id:   ClusterEventInvalidateChannelInit,
		Data: []byte("chan1"),
	}, model.PluginClusterEventSendOptions{
		SendType: model.PluginClusterEventSendTypeBestEffort,
	}).Return(nil).Once()

	c := NewCachingKVStore(inner, api)

	err := c.AddChannelConnection("chan1", "outbound:a")
	require.NoError(t, err)

	api.AssertExpectations(t)
}

func TestSetChannelConnections_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]string, error) {
			getCalls++
			return []string{"outbound:a"}, nil
		},
		setChannelConnectionsFn: func(channelID string, connNames []string) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.SetChannelConnections("chan1", []string{"outbound:a", "inbound:a"})
	require.NoError(t, err)

	_, err = c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestDeleteChannelConnections_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]string, error) {
			getCalls++
			return []string{"outbound:a"}, nil
		},
		deleteChannelConnectionsFn: func(channelID string) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.DeleteChannelConnections("chan1")
	require.NoError(t, err)

	_, err = c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestRemoveChannelConnection_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]string, error) {
			getCalls++
			return []string{"outbound:a"}, nil
		},
		removeChannelConnectionFn: func(channelID, connName string) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.RemoveChannelConnection("chan1", "outbound:a")
	require.NoError(t, err)

	_, err = c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestHandleClusterEvent_InvalidatesChannelInit(t *testing.T) {
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]string, error) {
			return []string{"outbound:a"}, nil
		},
	}
	c, _ := newTestCaching(inner)

	_, _ = c.GetChannelConnections("chan1")
	assert.Equal(t, 1, c.channelInitCache.Len())

	c.HandleClusterEvent(model.PluginClusterEvent{
		Id:   ClusterEventInvalidateChannelInit,
		Data: []byte("chan1"),
	})
	assert.Equal(t, 0, c.channelInitCache.Len())
}
