package main

import (
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestEnsureSyncUser_ExistingUser(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)

	syncUser := &mmModel.User{
		Id:       "sync-user-id",
		Username: "alice.high",
		Position: syncUserPosition,
	}

	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil)
	api.On("CreateTeamMember", "team1", "sync-user-id").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan1", "sync-user-id").Return(&mmModel.ChannelMember{}, nil)

	userID, err := p.ensureSyncUser("alice", "high", "team1", "chan1")
	require.NoError(t, err)
	assert.Equal(t, "sync-user-id", userID)
	api.AssertExpectations(t)
}

func TestEnsureSyncUser_ExistingNonSyncUser(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)

	realUser := &mmModel.User{
		Id:       "real-user-id",
		Username: "alice.high",
		Position: "engineer",
	}

	api.On("GetUserByUsername", "alice.high").Return(realUser, nil)

	_, err := p.ensureSyncUser("alice", "high", "team1", "chan1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a sync user")
}

func TestEnsureSyncUser_CreateNewUser(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)

	notFoundErr := &mmModel.AppError{Message: "user not found"}
	api.On("GetUserByUsername", "bob.low").Return(nil, notFoundErr)
	api.On("CreateUser", mock.MatchedBy(func(u *mmModel.User) bool {
		return u.Username == "bob.low" &&
			u.Position == syncUserPosition &&
			u.Nickname == "bob" &&
			u.FirstName == "bob" &&
			u.LastName == "(via low)" &&
			u.RemoteId == nil &&
			u.Props["CrossguardRemoteUsername"] == "bob"
	})).Return(&mmModel.User{Id: "new-user-id", Username: "bob.low"}, nil)
	api.On("CreateTeamMember", "team1", "new-user-id").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan1", "new-user-id").Return(&mmModel.ChannelMember{}, nil)

	userID, err := p.ensureSyncUser("bob", "low", "team1", "chan1")
	require.NoError(t, err)
	assert.Equal(t, "new-user-id", userID)
	api.AssertExpectations(t)
}

func TestEnsureSyncUser_UsernameTruncation(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)

	longUsername := "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefgh"
	connName := "high"
	expectedMunged := longUsername[:59] + ".high"

	notFoundErr := &mmModel.AppError{Message: "user not found"}
	api.On("GetUserByUsername", expectedMunged).Return(nil, notFoundErr)
	api.On("CreateUser", mock.MatchedBy(func(u *mmModel.User) bool {
		return u.Username == expectedMunged && len(u.Username) <= maxUsernameLength
	})).Return(&mmModel.User{Id: "new-id", Username: expectedMunged}, nil)
	api.On("CreateTeamMember", "team1", "new-id").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan1", "new-id").Return(&mmModel.ChannelMember{}, nil)
	api.On("LogWarn", "Truncated sync username to fit limit", "error_code", mock.Anything, "original", longUsername, "munged", expectedMunged).Return()

	userID, err := p.ensureSyncUser(longUsername, connName, "team1", "chan1")
	require.NoError(t, err)
	assert.Equal(t, "new-id", userID)
}

func TestResolveInboundUser_LookupFindsRealUser(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)

	enabled := true
	p.configuration = &configuration{UsernameLookup: &enabled}

	realUser := &mmModel.User{
		Id:       "real-user-id",
		Username: "alice",
		Position: "engineer",
	}

	api.On("GetUserByUsername", "alice").Return(realUser, nil)
	api.On("CreateTeamMember", "team1", "real-user-id").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan1", "real-user-id").Return(&mmModel.ChannelMember{}, nil)

	userID, err := p.resolveInboundUser("alice", "high", "team1", "chan1")
	require.NoError(t, err)
	assert.Equal(t, "real-user-id", userID)
	api.AssertExpectations(t)
}

func TestResolveInboundUser_LookupFindsSyncUser(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)

	enabled := true
	p.configuration = &configuration{UsernameLookup: &enabled}

	syncUser := &mmModel.User{
		Id:       "sync-user-id",
		Username: "alice",
		Position: syncUserPosition,
	}

	api.On("GetUserByUsername", "alice").Return(syncUser, nil)

	notFoundErr := &mmModel.AppError{Message: "user not found"}
	api.On("GetUserByUsername", "alice.high").Return(nil, notFoundErr)
	api.On("CreateUser", mock.MatchedBy(func(u *mmModel.User) bool {
		return u.Username == "alice.high" && u.Position == syncUserPosition
	})).Return(&mmModel.User{Id: "new-sync-id", Username: "alice.high"}, nil)
	api.On("CreateTeamMember", "team1", "new-sync-id").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan1", "new-sync-id").Return(&mmModel.ChannelMember{}, nil)

	userID, err := p.resolveInboundUser("alice", "high", "team1", "chan1")
	require.NoError(t, err)
	assert.Equal(t, "new-sync-id", userID)
	api.AssertExpectations(t)
}

func TestResolveInboundUser_LookupNotFound(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)

	enabled := true
	p.configuration = &configuration{UsernameLookup: &enabled}

	notFoundErr := &mmModel.AppError{Message: "user not found"}
	api.On("GetUserByUsername", "bob").Return(nil, notFoundErr)
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user", "error_code", mock.Anything,
		"username", "bob", "conn", "low").Return()

	api.On("GetUserByUsername", "bob.low").Return(nil, notFoundErr)
	api.On("CreateUser", mock.MatchedBy(func(u *mmModel.User) bool {
		return u.Username == "bob.low" && u.Position == syncUserPosition
	})).Return(&mmModel.User{Id: "new-sync-id", Username: "bob.low"}, nil)
	api.On("CreateTeamMember", "team1", "new-sync-id").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan1", "new-sync-id").Return(&mmModel.ChannelMember{}, nil)

	userID, err := p.resolveInboundUser("bob", "low", "team1", "chan1")
	require.NoError(t, err)
	assert.Equal(t, "new-sync-id", userID)
	api.AssertExpectations(t)
}

func TestResolveInboundUser_LookupDisabled(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)

	disabled := false
	p.configuration = &configuration{UsernameLookup: &disabled}

	syncUser := &mmModel.User{
		Id:       "sync-user-id",
		Username: "alice.high",
		Position: syncUserPosition,
	}

	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil)
	api.On("CreateTeamMember", "team1", "sync-user-id").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan1", "sync-user-id").Return(&mmModel.ChannelMember{}, nil)

	userID, err := p.resolveInboundUser("alice", "high", "team1", "chan1")
	require.NoError(t, err)
	assert.Equal(t, "sync-user-id", userID)
	api.AssertExpectations(t)
	api.AssertNotCalled(t, "GetUserByUsername", "alice")
}

func TestResolveInboundUser_NilDefaultEnabled(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)

	p.configuration = &configuration{}

	realUser := &mmModel.User{
		Id:       "real-user-id",
		Username: "alice",
		Position: "engineer",
	}

	api.On("GetUserByUsername", "alice").Return(realUser, nil)
	api.On("CreateTeamMember", "team1", "real-user-id").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan1", "real-user-id").Return(&mmModel.ChannelMember{}, nil)

	userID, err := p.resolveInboundUser("alice", "high", "team1", "chan1")
	require.NoError(t, err)
	assert.Equal(t, "real-user-id", userID)
	api.AssertExpectations(t)
}

func TestEnsureSyncUser_RaceConditionRetry(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)

	notFoundErr := &mmModel.AppError{Message: "user not found"}
	alreadyExistsErr := &mmModel.AppError{Message: "username already taken"}

	api.On("GetUserByUsername", "alice.high").Return(nil, notFoundErr).Once()
	api.On("CreateUser", mock.Anything).Return(nil, alreadyExistsErr)

	syncUser := &mmModel.User{
		Id:       "raced-user-id",
		Username: "alice.high",
		Position: syncUserPosition,
	}
	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil).Once()
	api.On("CreateTeamMember", "team1", "raced-user-id").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan1", "raced-user-id").Return(&mmModel.ChannelMember{}, nil)

	userID, err := p.ensureSyncUser("alice", "high", "team1", "chan1")
	require.NoError(t, err)
	assert.Equal(t, "raced-user-id", userID)
}

func TestEnsureSyncUser_ConnectionNameTooLong(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p := &Plugin{}
	p.SetAPI(api)

	// Connection name is 64 chars, leaving maxUser < 1
	longConn := "a234567890123456789012345678901234567890123456789012345678901234"
	_, err := p.ensureSyncUser("alice", longConn, "team1", "chan1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection name too long")
}

func TestEnsureSyncUser_CreateUserNonAlreadyError(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p := &Plugin{}
	p.SetAPI(api)

	api.On("GetUserByUsername", "alice.high").Return(nil, &mmModel.AppError{Message: "not found"})
	api.On("CreateUser", mock.Anything).Return(nil, &mmModel.AppError{Message: "internal error"})

	_, err := p.ensureSyncUser("alice", "high", "team1", "chan1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create sync user")
}

func TestEnsureMembership_AlreadyMember(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p := &Plugin{}
	p.SetAPI(api)

	api.On("CreateTeamMember", "team1", "user1").
		Return(nil, &mmModel.AppError{Message: "already a member"})
	api.On("AddChannelMember", "chan1", "user1").
		Return(nil, &mmModel.AppError{Message: "already a member"})

	// Should not log warnings for "already" errors
	p.ensureMembership("user1", "team1", "chan1")
	api.AssertNotCalled(t, "LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestEnsureMembership_RealError(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p := &Plugin{}
	p.SetAPI(api)

	api.On("CreateTeamMember", "team1", "user1").
		Return(nil, &mmModel.AppError{Message: "forbidden"})
	api.On("AddChannelMember", "chan1", "user1").
		Return(&mmModel.ChannelMember{}, nil)

	p.ensureMembership("user1", "team1", "chan1")
	// LogWarn should have been called for the team member error
	api.AssertCalled(t, "LogWarn", "Failed to add sync user to team", "error_code", mock.Anything,
		"user_id", "user1", "team_id", "team1", "error", "forbidden")
}
