package store

import (
	"encoding/json"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newTestClient(api *plugintest.API) KVStore {
	client := pluginapi.NewClient(api, nil)
	return NewKVStore(client, "test-plugin")
}

func marshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}

// kvSetOpts returns a non-atomic PluginKVSetOptions.
func kvSetOpts() model.PluginKVSetOptions {
	return model.PluginKVSetOptions{}
}

// kvCASNilOpts returns atomic PluginKVSetOptions for compare-against-nil (new key).
func kvCASNilOpts() model.PluginKVSetOptions {
	return model.PluginKVSetOptions{Atomic: true, OldValue: nil}
}

// kvCASOpts returns atomic PluginKVSetOptions for compare-against-existing.
func kvCASOpts(t *testing.T, old any) model.PluginKVSetOptions {
	t.Helper()
	return model.PluginKVSetOptions{Atomic: true, OldValue: marshalJSON(t, old)}
}

// ---------------------------------------------------------------------------
// Team connection CRUD
// ---------------------------------------------------------------------------

func TestClient_GetTeamConnections_Empty(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-teaminit-team1").Return(nil, nil)
	kv := newTestClient(api)

	conns, err := kv.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, []TeamConnection{}, conns)
}

func TestClient_GetTeamConnections_WithData(t *testing.T) {
	api := &plugintest.API{}
	expected := []TeamConnection{
		{Direction: "outbound", Connection: "high"},
		{Direction: "inbound", Connection: "low"},
	}
	api.On("KVGet", "test-plugin-teaminit-team1").Return(marshalJSON(t, expected), nil)
	kv := newTestClient(api)

	conns, err := kv.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, expected, conns)
}

func TestClient_GetTeamConnections_Error(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-teaminit-team1").Return(nil, &model.AppError{Message: "db error"})
	kv := newTestClient(api)

	_, err := kv.GetTeamConnections("team1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get team connections")
}

func TestClient_SetTeamConnections(t *testing.T) {
	api := &plugintest.API{}
	conns := []TeamConnection{{Direction: "outbound", Connection: "high"}}
	api.On("KVSetWithOptions", "test-plugin-teaminit-team1", marshalJSON(t, conns), kvSetOpts()).Return(true, nil)
	kv := newTestClient(api)

	err := kv.SetTeamConnections("team1", conns)
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_SetTeamConnections_Error(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).
		Return(false, &model.AppError{Message: "write fail"})
	kv := newTestClient(api)

	err := kv.SetTeamConnections("team1", []TeamConnection{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to set team connections")
}

func TestClient_DeleteTeamConnections(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVSetWithOptions", "test-plugin-teaminit-team1", []byte(nil), kvSetOpts()).Return(true, nil)
	kv := newTestClient(api)

	err := kv.DeleteTeamConnections("team1")
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_DeleteTeamConnections_Error(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVSetWithOptions", "test-plugin-teaminit-team1", []byte(nil), kvSetOpts()).
		Return(false, &model.AppError{Message: "del fail"})
	kv := newTestClient(api)

	err := kv.DeleteTeamConnections("team1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete team connections")
}

func TestClient_IsTeamInitialized(t *testing.T) {
	t.Run("true when connections exist", func(t *testing.T) {
		api := &plugintest.API{}
		api.On("KVGet", "test-plugin-teaminit-team1").
			Return(marshalJSON(t, []TeamConnection{{Direction: "outbound", Connection: "a"}}), nil)
		kv := newTestClient(api)

		ok, err := kv.IsTeamInitialized("team1")
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("false when no connections", func(t *testing.T) {
		api := &plugintest.API{}
		api.On("KVGet", "test-plugin-teaminit-team1").Return(nil, nil)
		kv := newTestClient(api)

		ok, err := kv.IsTeamInitialized("team1")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("error propagated", func(t *testing.T) {
		api := &plugintest.API{}
		api.On("KVGet", "test-plugin-teaminit-team1").Return(nil, &model.AppError{Message: "fail"})
		kv := newTestClient(api)

		_, err := kv.IsTeamInitialized("team1")
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// CAS operations: AddTeamConnection / RemoveTeamConnection
// ---------------------------------------------------------------------------

func TestClient_AddTeamConnection_New(t *testing.T) {
	api := &plugintest.API{}
	conn := TeamConnection{Direction: "outbound", Connection: "high"}

	api.On("KVGet", "test-plugin-teaminit-team1").Return(nil, nil).Once()
	api.On("KVSetWithOptions", "test-plugin-teaminit-team1",
		marshalJSON(t, []TeamConnection{conn}), kvCASNilOpts()).Return(true, nil).Once()

	kv := newTestClient(api)
	err := kv.AddTeamConnection("team1", conn)
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_AddTeamConnection_AlreadyExists(t *testing.T) {
	api := &plugintest.API{}
	conn := TeamConnection{Direction: "outbound", Connection: "high"}
	existing := []TeamConnection{conn}

	api.On("KVGet", "test-plugin-teaminit-team1").Return(marshalJSON(t, existing), nil).Once()
	kv := newTestClient(api)

	err := kv.AddTeamConnection("team1", conn)
	require.NoError(t, err)
	api.AssertNotCalled(t, "KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything)
}

func TestClient_AddTeamConnection_CASConflict(t *testing.T) {
	api := &plugintest.API{}
	conn := TeamConnection{Direction: "outbound", Connection: "high"}

	// First attempt: read empty, CAS fails
	api.On("KVGet", "test-plugin-teaminit-team1").Return(nil, nil).Once()
	api.On("KVSetWithOptions", "test-plugin-teaminit-team1", mock.Anything, kvCASNilOpts()).
		Return(false, nil).Once()

	// Second attempt: concurrent writer added something
	other := TeamConnection{Direction: "inbound", Connection: "low"}
	existingData := marshalJSON(t, []TeamConnection{other})
	api.On("KVGet", "test-plugin-teaminit-team1").Return(existingData, nil).Once()
	api.On("KVSetWithOptions", "test-plugin-teaminit-team1",
		marshalJSON(t, []TeamConnection{other, conn}),
		kvCASOpts(t, []TeamConnection{other})).Return(true, nil).Once()

	kv := newTestClient(api)
	err := kv.AddTeamConnection("team1", conn)
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_AddTeamConnection_MaxRetriesExhausted(t *testing.T) {
	api := &plugintest.API{}
	conn := TeamConnection{Direction: "outbound", Connection: "high"}

	api.On("KVGet", "test-plugin-teaminit-team1").Return(nil, nil)
	api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).Return(false, nil)

	kv := newTestClient(api)
	err := kv.AddTeamConnection("team1", conn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to modify connection list after max retries")
}

func TestClient_AddTeamConnection_GetError(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-teaminit-team1").Return(nil, &model.AppError{Message: "read fail"})

	kv := newTestClient(api)
	err := kv.AddTeamConnection("team1", TeamConnection{Direction: "outbound", Connection: "high"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read connection list for CAS")
}

func TestClient_RemoveTeamConnection_Exists(t *testing.T) {
	api := &plugintest.API{}
	conn := TeamConnection{Direction: "outbound", Connection: "high"}
	existing := []TeamConnection{
		{Direction: "inbound", Connection: "low"},
		conn,
	}

	api.On("KVGet", "test-plugin-teaminit-team1").Return(marshalJSON(t, existing), nil).Once()
	api.On("KVSetWithOptions", "test-plugin-teaminit-team1",
		marshalJSON(t, []TeamConnection{{Direction: "inbound", Connection: "low"}}),
		kvCASOpts(t, existing)).Return(true, nil).Once()

	kv := newTestClient(api)
	err := kv.RemoveTeamConnection("team1", conn)
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_RemoveTeamConnection_NotFound(t *testing.T) {
	api := &plugintest.API{}
	existing := []TeamConnection{{Direction: "inbound", Connection: "low"}}
	api.On("KVGet", "test-plugin-teaminit-team1").Return(marshalJSON(t, existing), nil).Once()

	kv := newTestClient(api)
	err := kv.RemoveTeamConnection("team1", TeamConnection{Direction: "outbound", Connection: "high"})
	require.NoError(t, err)
	api.AssertNotCalled(t, "KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything)
}

func TestClient_RemoveTeamConnection_CleansUpRewriteIndex(t *testing.T) {
	api := &plugintest.API{}
	conn := TeamConnection{Direction: "inbound", Connection: "high", RemoteTeamName: "remote-team"}
	existing := []TeamConnection{conn}

	api.On("KVGet", "test-plugin-teaminit-team1").Return(marshalJSON(t, existing), nil).Once()
	api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).Return(true, nil).Once()
	api.On("KVSetWithOptions", "test-plugin-rwi-high::remote-team", []byte(nil), kvSetOpts()).Return(true, nil).Once()

	kv := newTestClient(api)
	err := kv.RemoveTeamConnection("team1", conn)
	require.NoError(t, err)
	api.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Initialized team IDs (CAS string list)
// ---------------------------------------------------------------------------

func TestClient_GetInitializedTeamIDs_Empty(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-initialized-teams").Return(nil, nil)
	kv := newTestClient(api)

	ids, err := kv.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, []string{}, ids)
}

func TestClient_GetInitializedTeamIDs_WithData(t *testing.T) {
	api := &plugintest.API{}
	expected := []string{"t1", "t2", "t3"}
	api.On("KVGet", "test-plugin-initialized-teams").Return(marshalJSON(t, expected), nil)
	kv := newTestClient(api)

	ids, err := kv.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, expected, ids)
}

func TestClient_GetInitializedTeamIDs_Error(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-initialized-teams").Return(nil, &model.AppError{Message: "fail"})
	kv := newTestClient(api)

	_, err := kv.GetInitializedTeamIDs()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get initialized team IDs")
}

func TestClient_AddInitializedTeamID_New(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-initialized-teams").Return(nil, nil).Once()
	api.On("KVSetWithOptions", "test-plugin-initialized-teams",
		marshalJSON(t, []string{"team1"}), kvCASNilOpts()).Return(true, nil).Once()

	kv := newTestClient(api)
	err := kv.AddInitializedTeamID("team1")
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_AddInitializedTeamID_Duplicate(t *testing.T) {
	api := &plugintest.API{}
	existing := []string{"team1", "team2"}
	api.On("KVGet", "test-plugin-initialized-teams").Return(marshalJSON(t, existing), nil).Once()

	kv := newTestClient(api)
	err := kv.AddInitializedTeamID("team1")
	require.NoError(t, err)
	api.AssertNotCalled(t, "KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything)
}

func TestClient_AddInitializedTeamID_CASConflict(t *testing.T) {
	api := &plugintest.API{}
	// First: empty, CAS fails
	api.On("KVGet", "test-plugin-initialized-teams").Return(nil, nil).Once()
	api.On("KVSetWithOptions", "test-plugin-initialized-teams", mock.Anything, kvCASNilOpts()).
		Return(false, nil).Once()
	// Retry: concurrent writer added data, CAS succeeds
	existing := []string{"team2"}
	api.On("KVGet", "test-plugin-initialized-teams").Return(marshalJSON(t, existing), nil).Once()
	api.On("KVSetWithOptions", "test-plugin-initialized-teams",
		marshalJSON(t, []string{"team2", "team1"}),
		kvCASOpts(t, existing)).Return(true, nil).Once()

	kv := newTestClient(api)
	err := kv.AddInitializedTeamID("team1")
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_RemoveInitializedTeamID_Exists(t *testing.T) {
	api := &plugintest.API{}
	existing := []string{"team1", "team2"}
	api.On("KVGet", "test-plugin-initialized-teams").Return(marshalJSON(t, existing), nil).Once()
	api.On("KVSetWithOptions", "test-plugin-initialized-teams",
		marshalJSON(t, []string{"team2"}),
		kvCASOpts(t, existing)).Return(true, nil).Once()

	kv := newTestClient(api)
	err := kv.RemoveInitializedTeamID("team1")
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_RemoveInitializedTeamID_NotFound(t *testing.T) {
	api := &plugintest.API{}
	existing := []string{"team2"}
	api.On("KVGet", "test-plugin-initialized-teams").Return(marshalJSON(t, existing), nil).Once()

	kv := newTestClient(api)
	err := kv.RemoveInitializedTeamID("team1")
	require.NoError(t, err)
	api.AssertNotCalled(t, "KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything)
}

// ---------------------------------------------------------------------------
// Channel connections
// ---------------------------------------------------------------------------

func TestClient_GetChannelConnections_Empty(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-channelinit-ch1").Return(nil, nil)
	kv := newTestClient(api)

	conns, err := kv.GetChannelConnections("ch1")
	require.NoError(t, err)
	assert.Equal(t, []TeamConnection{}, conns)
}

func TestClient_GetChannelConnections_WithData(t *testing.T) {
	api := &plugintest.API{}
	expected := []TeamConnection{{Direction: "inbound", Connection: "low"}}
	api.On("KVGet", "test-plugin-channelinit-ch1").Return(marshalJSON(t, expected), nil)
	kv := newTestClient(api)

	conns, err := kv.GetChannelConnections("ch1")
	require.NoError(t, err)
	assert.Equal(t, expected, conns)
}

func TestClient_SetChannelConnections(t *testing.T) {
	api := &plugintest.API{}
	conns := []TeamConnection{{Direction: "outbound", Connection: "high"}}
	api.On("KVSetWithOptions", "test-plugin-channelinit-ch1", marshalJSON(t, conns), kvSetOpts()).Return(true, nil)
	kv := newTestClient(api)

	err := kv.SetChannelConnections("ch1", conns)
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_DeleteChannelConnections(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVSetWithOptions", "test-plugin-channelinit-ch1", []byte(nil), kvSetOpts()).Return(true, nil)
	kv := newTestClient(api)

	err := kv.DeleteChannelConnections("ch1")
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_AddChannelConnection_New(t *testing.T) {
	api := &plugintest.API{}
	conn := TeamConnection{Direction: "inbound", Connection: "low"}
	api.On("KVGet", "test-plugin-channelinit-ch1").Return(nil, nil).Once()
	api.On("KVSetWithOptions", "test-plugin-channelinit-ch1",
		marshalJSON(t, []TeamConnection{conn}), kvCASNilOpts()).Return(true, nil).Once()

	kv := newTestClient(api)
	err := kv.AddChannelConnection("ch1", conn)
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_AddChannelConnection_AlreadyExists(t *testing.T) {
	api := &plugintest.API{}
	conn := TeamConnection{Direction: "inbound", Connection: "low"}
	api.On("KVGet", "test-plugin-channelinit-ch1").Return(marshalJSON(t, []TeamConnection{conn}), nil).Once()

	kv := newTestClient(api)
	err := kv.AddChannelConnection("ch1", conn)
	require.NoError(t, err)
	api.AssertNotCalled(t, "KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything)
}

func TestClient_RemoveChannelConnection_Exists(t *testing.T) {
	api := &plugintest.API{}
	conn := TeamConnection{Direction: "inbound", Connection: "low"}
	existing := []TeamConnection{conn, {Direction: "outbound", Connection: "high"}}
	api.On("KVGet", "test-plugin-channelinit-ch1").Return(marshalJSON(t, existing), nil).Once()
	api.On("KVSetWithOptions", "test-plugin-channelinit-ch1",
		marshalJSON(t, []TeamConnection{{Direction: "outbound", Connection: "high"}}),
		kvCASOpts(t, existing)).Return(true, nil).Once()

	kv := newTestClient(api)
	err := kv.RemoveChannelConnection("ch1", conn)
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_RemoveChannelConnection_NotFound(t *testing.T) {
	api := &plugintest.API{}
	existing := []TeamConnection{{Direction: "outbound", Connection: "high"}}
	api.On("KVGet", "test-plugin-channelinit-ch1").Return(marshalJSON(t, existing), nil).Once()

	kv := newTestClient(api)
	err := kv.RemoveChannelConnection("ch1", TeamConnection{Direction: "inbound", Connection: "low"})
	require.NoError(t, err)
	api.AssertNotCalled(t, "KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything)
}

func TestClient_IsChannelInitialized(t *testing.T) {
	t.Run("true", func(t *testing.T) {
		api := &plugintest.API{}
		api.On("KVGet", "test-plugin-channelinit-ch1").
			Return(marshalJSON(t, []TeamConnection{{Direction: "inbound", Connection: "x"}}), nil)
		kv := newTestClient(api)
		ok, err := kv.IsChannelInitialized("ch1")
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("false", func(t *testing.T) {
		api := &plugintest.API{}
		api.On("KVGet", "test-plugin-channelinit-ch1").Return(nil, nil)
		kv := newTestClient(api)
		ok, err := kv.IsChannelInitialized("ch1")
		require.NoError(t, err)
		assert.False(t, ok)
	})
}

// ---------------------------------------------------------------------------
// Post mapping
// ---------------------------------------------------------------------------

func TestClient_SetPostMapping(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVSetWithOptions", "pm-high-remote123", marshalJSON(t, "local456"), kvSetOpts()).Return(true, nil)
	kv := newTestClient(api)

	err := kv.SetPostMapping("high", "remote123", "local456")
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_GetPostMapping_Exists(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "pm-high-remote123").Return(marshalJSON(t, "local456"), nil)
	kv := newTestClient(api)

	localID, err := kv.GetPostMapping("high", "remote123")
	require.NoError(t, err)
	assert.Equal(t, "local456", localID)
}

func TestClient_GetPostMapping_Empty(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "pm-high-remote123").Return(nil, nil)
	kv := newTestClient(api)

	localID, err := kv.GetPostMapping("high", "remote123")
	require.NoError(t, err)
	assert.Equal(t, "", localID)
}

func TestClient_DeletePostMapping(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVSetWithOptions", "pm-high-remote123", []byte(nil), kvSetOpts()).Return(true, nil)
	kv := newTestClient(api)

	err := kv.DeletePostMapping("high", "remote123")
	require.NoError(t, err)
	api.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Deleting flag
// ---------------------------------------------------------------------------

func TestClient_SetDeletingFlag(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVSetWithOptions", "crossguard-deleting-post1", marshalJSON(t, true), kvSetOpts()).Return(true, nil)
	kv := newTestClient(api)

	err := kv.SetDeletingFlag("post1")
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_IsDeletingFlagSet_True(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "crossguard-deleting-post1").Return(marshalJSON(t, true), nil)
	kv := newTestClient(api)

	set, err := kv.IsDeletingFlagSet("post1")
	require.NoError(t, err)
	assert.True(t, set)
}

func TestClient_IsDeletingFlagSet_False(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "crossguard-deleting-post1").Return(nil, nil)
	kv := newTestClient(api)

	set, err := kv.IsDeletingFlagSet("post1")
	require.NoError(t, err)
	assert.False(t, set)
}

func TestClient_ClearDeletingFlag(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVSetWithOptions", "crossguard-deleting-post1", []byte(nil), kvSetOpts()).Return(true, nil)
	kv := newTestClient(api)

	err := kv.ClearDeletingFlag("post1")
	require.NoError(t, err)
	api.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Connection prompts (team level)
// ---------------------------------------------------------------------------

func TestClient_GetConnectionPrompt_Exists(t *testing.T) {
	api := &plugintest.API{}
	prompt := &ConnectionPrompt{State: PromptStatePending, PostID: "p1"}
	api.On("KVGet", "test-plugin-connprompt-team1-high").Return(marshalJSON(t, prompt), nil)
	kv := newTestClient(api)

	result, err := kv.GetConnectionPrompt("team1", "high")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, PromptStatePending, result.State)
	assert.Equal(t, "p1", result.PostID)
}

func TestClient_GetConnectionPrompt_EmptyState(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-connprompt-team1-high").Return(nil, nil)
	kv := newTestClient(api)

	result, err := kv.GetConnectionPrompt("team1", "high")
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestClient_GetConnectionPrompt_Error(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-connprompt-team1-high").Return(nil, &model.AppError{Message: "fail"})
	kv := newTestClient(api)

	_, err := kv.GetConnectionPrompt("team1", "high")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get connection prompt")
}

func TestClient_SetConnectionPrompt(t *testing.T) {
	api := &plugintest.API{}
	prompt := &ConnectionPrompt{State: PromptStateBlocked, PostID: "p2"}
	api.On("KVSetWithOptions", "test-plugin-connprompt-team1-high", marshalJSON(t, prompt), kvSetOpts()).Return(true, nil)
	kv := newTestClient(api)

	err := kv.SetConnectionPrompt("team1", "high", prompt)
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_DeleteConnectionPrompt(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVSetWithOptions", "test-plugin-connprompt-team1-high", []byte(nil), kvSetOpts()).Return(true, nil)
	kv := newTestClient(api)

	err := kv.DeleteConnectionPrompt("team1", "high")
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_CreateConnectionPrompt_Success(t *testing.T) {
	api := &plugintest.API{}
	prompt := &ConnectionPrompt{State: PromptStatePending, PostID: "p1"}
	api.On("KVSetWithOptions", "test-plugin-connprompt-team1-high",
		marshalJSON(t, prompt), kvCASNilOpts()).Return(true, nil)
	kv := newTestClient(api)

	created, err := kv.CreateConnectionPrompt("team1", "high", prompt)
	require.NoError(t, err)
	assert.True(t, created)
}

func TestClient_CreateConnectionPrompt_AlreadyExists(t *testing.T) {
	api := &plugintest.API{}
	prompt := &ConnectionPrompt{State: PromptStatePending, PostID: "p1"}
	api.On("KVSetWithOptions", "test-plugin-connprompt-team1-high",
		marshalJSON(t, prompt), kvCASNilOpts()).Return(false, nil)
	kv := newTestClient(api)

	created, err := kv.CreateConnectionPrompt("team1", "high", prompt)
	require.NoError(t, err)
	assert.False(t, created)
}

// ---------------------------------------------------------------------------
// Channel connection prompts
// ---------------------------------------------------------------------------

func TestClient_GetChannelConnectionPrompt_Exists(t *testing.T) {
	api := &plugintest.API{}
	prompt := &ConnectionPrompt{State: PromptStatePending, PostID: "cp1"}
	api.On("KVGet", "test-plugin-chanprompt-ch1-high").Return(marshalJSON(t, prompt), nil)
	kv := newTestClient(api)

	result, err := kv.GetChannelConnectionPrompt("ch1", "high")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, PromptStatePending, result.State)
}

func TestClient_GetChannelConnectionPrompt_EmptyState(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-chanprompt-ch1-high").Return(nil, nil)
	kv := newTestClient(api)

	result, err := kv.GetChannelConnectionPrompt("ch1", "high")
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestClient_SetChannelConnectionPrompt(t *testing.T) {
	api := &plugintest.API{}
	prompt := &ConnectionPrompt{State: PromptStateBlocked, PostID: "cp2"}
	api.On("KVSetWithOptions", "test-plugin-chanprompt-ch1-high", marshalJSON(t, prompt), kvSetOpts()).Return(true, nil)
	kv := newTestClient(api)

	err := kv.SetChannelConnectionPrompt("ch1", "high", prompt)
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_DeleteChannelConnectionPrompt(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVSetWithOptions", "test-plugin-chanprompt-ch1-high", []byte(nil), kvSetOpts()).Return(true, nil)
	kv := newTestClient(api)

	err := kv.DeleteChannelConnectionPrompt("ch1", "high")
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_CreateChannelConnectionPrompt_Success(t *testing.T) {
	api := &plugintest.API{}
	prompt := &ConnectionPrompt{State: PromptStatePending, PostID: "cp1"}
	api.On("KVSetWithOptions", "test-plugin-chanprompt-ch1-high",
		marshalJSON(t, prompt), kvCASNilOpts()).Return(true, nil)
	kv := newTestClient(api)

	created, err := kv.CreateChannelConnectionPrompt("ch1", "high", prompt)
	require.NoError(t, err)
	assert.True(t, created)
}

func TestClient_CreateChannelConnectionPrompt_AlreadyExists(t *testing.T) {
	api := &plugintest.API{}
	prompt := &ConnectionPrompt{State: PromptStatePending, PostID: "cp1"}
	api.On("KVSetWithOptions", "test-plugin-chanprompt-ch1-high",
		marshalJSON(t, prompt), kvCASNilOpts()).Return(false, nil)
	kv := newTestClient(api)

	created, err := kv.CreateChannelConnectionPrompt("ch1", "high", prompt)
	require.NoError(t, err)
	assert.False(t, created)
}

// ---------------------------------------------------------------------------
// Rewrite index
// ---------------------------------------------------------------------------

func TestClient_GetTeamRewriteIndex_Exists(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-rwi-high::remote-team").Return(marshalJSON(t, "local-team-id"), nil)
	kv := newTestClient(api)

	localID, err := kv.GetTeamRewriteIndex("high", "remote-team")
	require.NoError(t, err)
	assert.Equal(t, "local-team-id", localID)
}

func TestClient_GetTeamRewriteIndex_Empty(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-rwi-high::remote-team").Return(nil, nil)
	kv := newTestClient(api)

	localID, err := kv.GetTeamRewriteIndex("high", "remote-team")
	require.NoError(t, err)
	assert.Equal(t, "", localID)
}

func TestClient_SetTeamRewriteIndex_New(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-rwi-high::remote-team").Return(nil, nil)
	api.On("KVSetWithOptions", "test-plugin-rwi-high::remote-team",
		marshalJSON(t, "local-team-id"), kvSetOpts()).Return(true, nil)
	kv := newTestClient(api)

	err := kv.SetTeamRewriteIndex("high", "remote-team", "local-team-id")
	require.NoError(t, err)
	api.AssertExpectations(t)
}

func TestClient_SetTeamRewriteIndex_SameTeam(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-rwi-high::remote-team").Return(marshalJSON(t, "local-team-id"), nil)
	api.On("KVSetWithOptions", "test-plugin-rwi-high::remote-team",
		marshalJSON(t, "local-team-id"), kvSetOpts()).Return(true, nil)
	kv := newTestClient(api)

	err := kv.SetTeamRewriteIndex("high", "remote-team", "local-team-id")
	require.NoError(t, err)
}

func TestClient_SetTeamRewriteIndex_ConflictDifferentTeam(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-rwi-high::remote-team").Return(marshalJSON(t, "other-team-id"), nil)
	kv := newTestClient(api)

	err := kv.SetTeamRewriteIndex("high", "remote-team", "local-team-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already mapped to team")
}

func TestClient_DeleteTeamRewriteIndex(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVSetWithOptions", "test-plugin-rwi-high::remote-team", []byte(nil), kvSetOpts()).Return(true, nil)
	kv := newTestClient(api)

	err := kv.DeleteTeamRewriteIndex("high", "remote-team")
	require.NoError(t, err)
	api.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// CAS edge cases
// ---------------------------------------------------------------------------

func TestClient_CASStringList_MaxRetriesExhausted(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-initialized-teams").Return(nil, nil)
	api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).Return(false, nil)
	kv := newTestClient(api)

	err := kv.AddInitializedTeamID("team1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to modify list after max retries")
}

func TestClient_CASStringList_SetError(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-initialized-teams").Return(nil, nil).Once()
	api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).
		Return(false, &model.AppError{Message: "write fail"}).Once()
	kv := newTestClient(api)

	err := kv.AddInitializedTeamID("team1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to CAS list")
}

func TestClient_CASConnectionList_SetError(t *testing.T) {
	api := &plugintest.API{}
	api.On("KVGet", "test-plugin-teaminit-team1").Return(nil, nil).Once()
	api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).
		Return(false, &model.AppError{Message: "write fail"}).Once()
	kv := newTestClient(api)

	err := kv.AddTeamConnection("team1", TeamConnection{Direction: "outbound", Connection: "high"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to CAS connection list")
}
