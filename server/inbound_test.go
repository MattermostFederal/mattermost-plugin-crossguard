package main

import (
	"context"
	"errors"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

type testKVStore struct {
	store.KVStore
	postMappings  map[string]string
	deletingFlags map[string]bool
}

func newTestKVStore() *testKVStore {
	return &testKVStore{
		postMappings:  make(map[string]string),
		deletingFlags: make(map[string]bool),
	}
}

func (s *testKVStore) GetTeamConnections(string) ([]store.TeamConnection, error) {
	return []store.TeamConnection{
		{Direction: "inbound", Connection: "high"},
		{Direction: "outbound", Connection: "high"},
	}, nil
}
func (s *testKVStore) SetTeamConnections(string, []store.TeamConnection) error { return nil }
func (s *testKVStore) DeleteTeamConnections(string) error                      { return nil }
func (s *testKVStore) IsTeamInitialized(string) (bool, error)                  { return true, nil }
func (s *testKVStore) AddTeamConnection(string, store.TeamConnection) error    { return nil }
func (s *testKVStore) RemoveTeamConnection(string, store.TeamConnection) error { return nil }
func (s *testKVStore) GetInitializedTeamIDs() ([]string, error)                { return nil, nil }
func (s *testKVStore) AddInitializedTeamID(string) error                       { return nil }
func (s *testKVStore) RemoveInitializedTeamID(string) error                    { return nil }
func (s *testKVStore) GetChannelConnections(string) ([]store.TeamConnection, error) {
	return []store.TeamConnection{
		{Direction: "inbound", Connection: "high"},
		{Direction: "outbound", Connection: "high"},
	}, nil
}
func (s *testKVStore) SetChannelConnections(string, []store.TeamConnection) error { return nil }
func (s *testKVStore) DeleteChannelConnections(string) error                      { return nil }
func (s *testKVStore) IsChannelInitialized(string) (bool, error)                  { return true, nil }
func (s *testKVStore) AddChannelConnection(string, store.TeamConnection) error    { return nil }
func (s *testKVStore) RemoveChannelConnection(string, store.TeamConnection) error { return nil }

func (s *testKVStore) SetPostMapping(connName, remotePostID, localPostID string) error {
	s.postMappings[connName+"-"+remotePostID] = localPostID
	return nil
}

func (s *testKVStore) GetPostMapping(connName, remotePostID string) (string, error) {
	return s.postMappings[connName+"-"+remotePostID], nil
}

func (s *testKVStore) DeletePostMapping(connName, remotePostID string) error {
	delete(s.postMappings, connName+"-"+remotePostID)
	return nil
}

func (s *testKVStore) SetDeletingFlag(postID string) error {
	s.deletingFlags[postID] = true
	return nil
}

func (s *testKVStore) IsDeletingFlagSet(postID string) (bool, error) {
	return s.deletingFlags[postID], nil
}

func (s *testKVStore) ClearDeletingFlag(postID string) error {
	delete(s.deletingFlags, postID)
	return nil
}

func (s *testKVStore) GetConnectionPrompt(string, string) (*store.ConnectionPrompt, error) {
	return nil, nil
}

func (s *testKVStore) SetConnectionPrompt(string, string, *store.ConnectionPrompt) error {
	return nil
}

func (s *testKVStore) DeleteConnectionPrompt(string, string) error {
	return nil
}

func (s *testKVStore) CreateConnectionPrompt(string, string, *store.ConnectionPrompt) (bool, error) {
	return true, nil
}

func (s *testKVStore) GetChannelConnectionPrompt(string, string) (*store.ConnectionPrompt, error) {
	return nil, nil
}

func (s *testKVStore) SetChannelConnectionPrompt(string, string, *store.ConnectionPrompt) error {
	return nil
}

func (s *testKVStore) DeleteChannelConnectionPrompt(string, string) error {
	return nil
}

func (s *testKVStore) CreateChannelConnectionPrompt(string, string, *store.ConnectionPrompt) (bool, error) {
	return true, nil
}

func (s *testKVStore) GetTeamRewriteIndex(string, string) (string, error) {
	return "", nil
}

func (s *testKVStore) SetTeamRewriteIndex(string, string, string) error {
	return nil
}

func (s *testKVStore) DeleteTeamRewriteIndex(string, string) error {
	return nil
}

func setupTestPlugin(api *plugintest.API) (*Plugin, *testKVStore) {
	p := &Plugin{}
	p.SetAPI(api)
	p.botUserID = "bot-user-id"
	kvs := newTestKVStore()
	p.kvstore = kvs
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel
	p.relaySem = make(chan struct{}, 50)
	return p, kvs
}

func TestResolveTeamAndChannel(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)

		team := &mmModel.Team{Id: "team-id", Name: "test-a"}
		channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}

		api.On("GetTeamByName", "test-a").Return(team, nil)
		api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)

		gotTeam, gotChannel, err := p.resolveTeamAndChannel("high", "test-a", "town-square")
		require.NoError(t, err)
		assert.Equal(t, "team-id", gotTeam.Id)
		assert.Equal(t, "chan-id", gotChannel.Id)
	})

	t.Run("team not found", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)

		api.On("GetTeamByName", "unknown").Return(nil, &mmModel.AppError{Message: "not found"})

		_, _, err := p.resolveTeamAndChannel("high", "unknown", "town-square")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("channel not found", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)

		team := &mmModel.Team{Id: "team-id", Name: "test-a"}
		api.On("GetTeamByName", "test-a").Return(team, nil)
		api.On("GetChannelByName", "team-id", "unknown-chan", false).Return(nil, &mmModel.AppError{Message: "not found"})

		_, _, err := p.resolveTeamAndChannel("high", "test-a", "unknown-chan")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("connection not linked to team", func(t *testing.T) {
		api := &plugintest.API{}
		p := &Plugin{}
		p.SetAPI(api)
		p.botUserID = "bot-user-id"
		kvs := newTestKVStore()
		kvs.KVStore = nil
		p.kvstore = &unlinkTestKVStore{testKVStore: kvs, conns: []store.TeamConnection{
			{Direction: "inbound", Connection: "other"},
		}}
		ctx, cancel := context.WithCancel(context.Background())
		p.ctx = ctx
		p.cancel = cancel
		p.relaySem = make(chan struct{}, 50)

		team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
		channel := &mmModel.Channel{Id: "ts-id", Name: "town-square", TeamId: "team-id"}
		api.On("GetTeamByName", "test-a").Return(team, nil)
		api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(&mmModel.Post{Id: "prompt-post-id"}, nil)
		api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

		_, _, err := p.resolveTeamAndChannel("high", "test-a", "town-square")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not linked")
	})

	t.Run("rewrite when team exists but connection not linked", func(t *testing.T) {
		api := &plugintest.API{}
		p := &Plugin{}
		p.SetAPI(api)
		p.botUserID = "bot-user-id"
		ctx, cancel := context.WithCancel(context.Background())
		p.ctx = ctx
		p.cancel = cancel
		p.relaySem = make(chan struct{}, 50)

		destTeam := &mmModel.Team{Id: "dest-team-id", Name: "test"}
		channel := &mmModel.Channel{Id: "dest-chan-id", Name: "local-loopback", TeamId: "dest-team-id"}

		api.On("GetTeam", "dest-team-id").Return(destTeam, nil)
		api.On("GetChannelByName", "dest-team-id", "local-loopback", false).Return(channel, nil)

		kvs := &rewriteTestKVStore{
			testKVStore: newTestKVStore(),
			teamConns: map[string][]store.TeamConnection{
				"src-team-id":  {{Direction: "outbound", Connection: "loopback"}},
				"dest-team-id": {{Direction: "inbound", Connection: "loopback"}},
			},
			chanConns: map[string][]store.TeamConnection{
				"dest-chan-id": {{Direction: "inbound", Connection: "loopback"}},
			},
			rewriteIndex: map[string]string{
				"loopback::loop": "dest-team-id",
			},
		}
		p.kvstore = kvs

		gotTeam, gotChannel, err := p.resolveTeamAndChannel("loopback", "loop", "local-loopback")
		require.NoError(t, err)
		assert.Equal(t, "dest-team-id", gotTeam.Id)
		assert.Equal(t, "dest-chan-id", gotChannel.Id)
	})

	t.Run("rewrite index error", func(t *testing.T) {
		api := &plugintest.API{}
		p := &Plugin{}
		p.SetAPI(api)
		p.botUserID = "bot-user-id"
		ctx, cancel := context.WithCancel(context.Background())
		p.ctx = ctx
		p.cancel = cancel
		p.relaySem = make(chan struct{}, 50)

		kvs := &rewriteTestKVStore{
			testKVStore: newTestKVStore(),
			indexErr:    errors.New("store unavailable"),
		}
		p.kvstore = kvs

		_, _, err := p.resolveTeamAndChannel("loopback", "loop", "local-loopback")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rewrite index lookup")
		assert.Contains(t, err.Error(), "store unavailable")
	})

	t.Run("rewrite target team not found", func(t *testing.T) {
		api := &plugintest.API{}
		p := &Plugin{}
		p.SetAPI(api)
		p.botUserID = "bot-user-id"
		ctx, cancel := context.WithCancel(context.Background())
		p.ctx = ctx
		p.cancel = cancel
		p.relaySem = make(chan struct{}, 50)

		api.On("GetTeam", "deleted-team-id").Return(nil, &mmModel.AppError{Message: "team not found"})

		kvs := &rewriteTestKVStore{
			testKVStore: newTestKVStore(),
			rewriteIndex: map[string]string{
				"loopback::loop": "deleted-team-id",
			},
		}
		p.kvstore = kvs

		_, _, err := p.resolveTeamAndChannel("loopback", "loop", "local-loopback")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rewrite target team")
	})
}

type rewriteTestKVStore struct {
	*testKVStore
	teamConns    map[string][]store.TeamConnection
	chanConns    map[string][]store.TeamConnection
	rewriteIndex map[string]string
	indexErr     error
}

func (s *rewriteTestKVStore) GetTeamConnections(teamID string) ([]store.TeamConnection, error) {
	if conns, ok := s.teamConns[teamID]; ok {
		return conns, nil
	}
	return nil, nil
}

func (s *rewriteTestKVStore) GetChannelConnections(chanID string) ([]store.TeamConnection, error) {
	if conns, ok := s.chanConns[chanID]; ok {
		return conns, nil
	}
	return nil, nil
}

func (s *rewriteTestKVStore) GetTeamRewriteIndex(connName, remoteTeamName string) (string, error) {
	if s.indexErr != nil {
		return "", s.indexErr
	}
	return s.rewriteIndex[connName+"::"+remoteTeamName], nil
}

type unlinkTestKVStore struct {
	*testKVStore
	conns []store.TeamConnection
}

func (s *unlinkTestKVStore) GetTeamConnections(string) ([]store.TeamConnection, error) {
	return s.conns, nil
}

func TestHandleInboundPost(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-uid", Username: "alice.high", Position: syncUserPosition}
	notFoundErr := &mmModel.AppError{Message: "not found"}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, notFoundErr)
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user",
		"username", "alice", "conn", "high").Return()
	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil)
	api.On("CreateTeamMember", "team-id", "sync-uid").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan-id", "sync-uid").Return(&mmModel.ChannelMember{}, nil)
	api.On("CreatePost", mock.MatchedBy(func(post *mmModel.Post) bool {
		return post.UserId == "sync-uid" &&
			post.ChannelId == "chan-id" &&
			post.Message == "hello from remote" &&
			post.GetProp("crossguard_relayed") == true
	})).Return(&mmModel.Post{Id: "local-post-id"}, nil)

	postMsg := model.PostMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
		Username:    "alice",
		Message:     "hello from remote",
	}
	envelope, err := model.NewMessage(model.MessageTypePost, postMsg)
	require.NoError(t, err)

	p.handleInboundPost("high", envelope)

	assert.Equal(t, "local-post-id", kvs.postMappings["high-remote-post-id"])
	api.AssertExpectations(t)
}

func TestHandleInboundUpdate(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["high-remote-post-id"] = "local-post-id"

	existingPost := &mmModel.Post{
		Id:      "local-post-id",
		Message: "original",
	}
	existingPost.AddProp("crossguard_relayed", true)

	api.On("GetPost", "local-post-id").Return(existingPost, nil)
	api.On("UpdatePost", mock.MatchedBy(func(post *mmModel.Post) bool {
		return post.Id == "local-post-id" && post.Message == "edited message"
	})).Return(&mmModel.Post{Id: "local-post-id"}, nil)

	postMsg := model.PostMessage{
		PostID:  "remote-post-id",
		Message: "edited message",
	}
	envelope, err := model.NewMessage(model.MessageTypeUpdate, postMsg)
	require.NoError(t, err)

	p.handleInboundUpdate("high", envelope)
	api.AssertExpectations(t)
}

func TestHandleInboundUpdate_NoMapping(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	api.On("LogWarn", "Inbound update: no post mapping found", "conn", "high", "remote_id", "unknown-id").Return()

	postMsg := model.PostMessage{PostID: "unknown-id", Message: "edited"}
	envelope, err := model.NewMessage(model.MessageTypeUpdate, postMsg)
	require.NoError(t, err)

	p.handleInboundUpdate("high", envelope)
}

func TestHandleInboundDelete(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["high-remote-post-id"] = "local-post-id"

	api.On("DeletePost", "local-post-id").Return(nil)

	deleteMsg := model.DeleteMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
	}
	envelope, err := model.NewMessage(model.MessageTypeDelete, deleteMsg)
	require.NoError(t, err)

	p.handleInboundDelete("high", envelope)

	_, exists := kvs.postMappings["high-remote-post-id"]
	assert.False(t, exists, "post mapping should be deleted")

	assert.False(t, kvs.deletingFlags["local-post-id"], "delete flag should be cleaned up")

	api.AssertExpectations(t)
}

func TestHandleInboundReaction_Add(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["high-remote-post-id"] = "local-post-id"

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-uid", Username: "alice.high", Position: syncUserPosition}
	notFoundErr := &mmModel.AppError{Message: "not found"}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, notFoundErr)
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user",
		"username", "alice", "conn", "high").Return()
	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil)
	api.On("CreateTeamMember", "team-id", "sync-uid").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan-id", "sync-uid").Return(&mmModel.ChannelMember{}, nil)
	api.On("AddReaction", mock.MatchedBy(func(r *mmModel.Reaction) bool {
		return r.UserId == "sync-uid" && r.PostId == "local-post-id" && r.EmojiName == "thumbsup"
	})).Return(&mmModel.Reaction{}, nil)

	reactionMsg := model.ReactionMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
		Username:    "alice",
		EmojiName:   "thumbsup",
	}
	envelope, err := model.NewMessage(model.MessageTypeReactionAdd, reactionMsg)
	require.NoError(t, err)

	p.handleInboundReaction("high", envelope, true)
	api.AssertExpectations(t)
}

func TestHandleInboundReaction_Remove(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["high-remote-post-id"] = "local-post-id"

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-uid", Username: "alice.high", Position: syncUserPosition}
	notFoundErr := &mmModel.AppError{Message: "not found"}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, notFoundErr)
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user",
		"username", "alice", "conn", "high").Return()
	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil)
	api.On("CreateTeamMember", "team-id", "sync-uid").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan-id", "sync-uid").Return(&mmModel.ChannelMember{}, nil)
	api.On("RemoveReaction", mock.MatchedBy(func(r *mmModel.Reaction) bool {
		return r.UserId == "sync-uid" && r.PostId == "local-post-id" && r.EmojiName == "thumbsup"
	})).Return(nil)

	reactionMsg := model.ReactionMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
		Username:    "alice",
		EmojiName:   "thumbsup",
	}
	envelope, err := model.NewMessage(model.MessageTypeReactionRemove, reactionMsg)
	require.NoError(t, err)

	p.handleInboundReaction("high", envelope, false)
	api.AssertExpectations(t)
}

func TestHandleInboundPost_WithThread(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["high-remote-root-id"] = "local-root-id"

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-uid", Username: "alice.high", Position: syncUserPosition}
	notFoundErr := &mmModel.AppError{Message: "not found"}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, notFoundErr)
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user",
		"username", "alice", "conn", "high").Return()
	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil)
	api.On("CreateTeamMember", "team-id", "sync-uid").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan-id", "sync-uid").Return(&mmModel.ChannelMember{}, nil)
	api.On("CreatePost", mock.MatchedBy(func(post *mmModel.Post) bool {
		return post.RootId == "local-root-id" && post.Message == "reply"
	})).Return(&mmModel.Post{Id: "local-reply-id"}, nil)

	postMsg := model.PostMessage{
		PostID:      "remote-reply-id",
		RootID:      "remote-root-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
		Username:    "alice",
		Message:     "reply",
	}
	envelope, err := model.NewMessage(model.MessageTypePost, postMsg)
	require.NoError(t, err)

	p.handleInboundPost("high", envelope)

	assert.Equal(t, "local-reply-id", kvs.postMappings["high-remote-reply-id"])
	api.AssertExpectations(t)
}
