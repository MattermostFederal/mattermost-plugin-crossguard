package main

import (
	"context"
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

func (s *testKVStore) GetTeamConnections(string) ([]string, error) {
	return []string{"inbound:cgb", "outbound:cgb"}, nil
}
func (s *testKVStore) SetTeamConnections(string, []string) error { return nil }
func (s *testKVStore) DeleteTeamConnections(string) error        { return nil }
func (s *testKVStore) IsTeamInitialized(string) (bool, error)    { return true, nil }
func (s *testKVStore) AddTeamConnection(string, string) error    { return nil }
func (s *testKVStore) RemoveTeamConnection(string, string) error { return nil }
func (s *testKVStore) GetInitializedTeamIDs() ([]string, error)  { return nil, nil }
func (s *testKVStore) AddInitializedTeamID(string) error         { return nil }
func (s *testKVStore) RemoveInitializedTeamID(string) error      { return nil }
func (s *testKVStore) GetChannelConnections(string) ([]string, error) {
	return []string{"inbound:cgb", "outbound:cgb"}, nil
}
func (s *testKVStore) SetChannelConnections(string, []string) error { return nil }
func (s *testKVStore) DeleteChannelConnections(string) error        { return nil }
func (s *testKVStore) IsChannelInitialized(string) (bool, error)    { return true, nil }
func (s *testKVStore) AddChannelConnection(string, string) error    { return nil }
func (s *testKVStore) RemoveChannelConnection(string, string) error { return nil }

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

		gotTeam, gotChannel, err := p.resolveTeamAndChannel("cgb", "test-a", "town-square")
		require.NoError(t, err)
		assert.Equal(t, "team-id", gotTeam.Id)
		assert.Equal(t, "chan-id", gotChannel.Id)
	})

	t.Run("team not found", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)

		api.On("GetTeamByName", "unknown").Return(nil, &mmModel.AppError{Message: "not found"})

		_, _, err := p.resolveTeamAndChannel("cgb", "unknown", "town-square")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("channel not found", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)

		team := &mmModel.Team{Id: "team-id", Name: "test-a"}
		api.On("GetTeamByName", "test-a").Return(team, nil)
		api.On("GetChannelByName", "team-id", "unknown-chan", false).Return(nil, &mmModel.AppError{Message: "not found"})

		_, _, err := p.resolveTeamAndChannel("cgb", "test-a", "unknown-chan")
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
		p.kvstore = &unlinkTestKVStore{testKVStore: kvs, conns: []string{"inbound:other"}}
		ctx, cancel := context.WithCancel(context.Background())
		p.ctx = ctx
		p.cancel = cancel
		p.relaySem = make(chan struct{}, 50)

		team := &mmModel.Team{Id: "team-id", Name: "test-a"}
		api.On("GetTeamByName", "test-a").Return(team, nil)

		_, _, err := p.resolveTeamAndChannel("cgb", "test-a", "town-square")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not linked")
	})
}

type unlinkTestKVStore struct {
	*testKVStore
	conns []string
}

func (s *unlinkTestKVStore) GetTeamConnections(string) ([]string, error) {
	return s.conns, nil
}

func TestHandleInboundPost(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-uid", Username: "alice.cgb", Position: syncUserPosition}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, &mmModel.AppError{Message: "not found"})
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user",
		"username", "alice", "conn", "cgb").Return()
	api.On("GetUserByUsername", "alice.cgb").Return(syncUser, nil)
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

	p.handleInboundPost("cgb", envelope)

	assert.Equal(t, "local-post-id", kvs.postMappings["cgb-remote-post-id"])
	api.AssertExpectations(t)
}

func TestHandleInboundUpdate(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["cgb-remote-post-id"] = "local-post-id"

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

	p.handleInboundUpdate("cgb", envelope)
	api.AssertExpectations(t)
}

func TestHandleInboundUpdate_NoMapping(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	api.On("LogWarn", "Inbound update: no post mapping found", "conn", "cgb", "remote_id", "unknown-id").Return()

	postMsg := model.PostMessage{PostID: "unknown-id", Message: "edited"}
	envelope, err := model.NewMessage(model.MessageTypeUpdate, postMsg)
	require.NoError(t, err)

	p.handleInboundUpdate("cgb", envelope)
}

func TestHandleInboundDelete(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["cgb-remote-post-id"] = "local-post-id"

	api.On("DeletePost", "local-post-id").Return(nil)

	deleteMsg := model.DeleteMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
	}
	envelope, err := model.NewMessage(model.MessageTypeDelete, deleteMsg)
	require.NoError(t, err)

	p.handleInboundDelete("cgb", envelope)

	_, exists := kvs.postMappings["cgb-remote-post-id"]
	assert.False(t, exists, "post mapping should be deleted")

	assert.False(t, kvs.deletingFlags["local-post-id"], "delete flag should be cleaned up")

	api.AssertExpectations(t)
}

func TestHandleInboundReaction_Add(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["cgb-remote-post-id"] = "local-post-id"

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-uid", Username: "alice.cgb", Position: syncUserPosition}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, &mmModel.AppError{Message: "not found"})
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user",
		"username", "alice", "conn", "cgb").Return()
	api.On("GetUserByUsername", "alice.cgb").Return(syncUser, nil)
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

	p.handleInboundReaction("cgb", envelope, true)
	api.AssertExpectations(t)
}

func TestHandleInboundReaction_Remove(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["cgb-remote-post-id"] = "local-post-id"

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-uid", Username: "alice.cgb", Position: syncUserPosition}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, &mmModel.AppError{Message: "not found"})
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user",
		"username", "alice", "conn", "cgb").Return()
	api.On("GetUserByUsername", "alice.cgb").Return(syncUser, nil)
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

	p.handleInboundReaction("cgb", envelope, false)
	api.AssertExpectations(t)
}

func TestHandleInboundPost_WithThread(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["cgb-remote-root-id"] = "local-root-id"

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-uid", Username: "alice.cgb", Position: syncUserPosition}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, &mmModel.AppError{Message: "not found"})
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user",
		"username", "alice", "conn", "cgb").Return()
	api.On("GetUserByUsername", "alice.cgb").Return(syncUser, nil)
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

	p.handleInboundPost("cgb", envelope)

	assert.Equal(t, "local-reply-id", kvs.postMappings["cgb-remote-reply-id"])
	api.AssertExpectations(t)
}
