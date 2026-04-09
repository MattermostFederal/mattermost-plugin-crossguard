package main

import (
	"fmt"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

func TestIsTeamAdminOrSystemAdmin(t *testing.T) {
	t.Run("system admin always allowed", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{RestrictToSystemAdmins: new(true)})

		api.On("GetUser", "sysadmin").Return(&mmModel.User{
			Roles: mmModel.SystemAdminRoleId,
		}, nil)

		assert.True(t, p.isTeamAdminOrSystemAdmin("sysadmin", "team-id"))
	})

	t.Run("team admin allowed when unrestricted", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{RestrictToSystemAdmins: new(false)})

		api.On("GetUser", "teamadmin").Return(&mmModel.User{
			Roles: mmModel.TeamAdminRoleId,
		}, nil)
		api.On("GetTeamMember", "team-id", "teamadmin").Return(&mmModel.TeamMember{
			SchemeAdmin: true,
		}, nil)

		assert.True(t, p.isTeamAdminOrSystemAdmin("teamadmin", "team-id"))
	})

	t.Run("team admin blocked when restricted", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{RestrictToSystemAdmins: new(true)})

		api.On("GetUser", "teamadmin").Return(&mmModel.User{
			Roles: mmModel.TeamAdminRoleId,
		}, nil)

		assert.False(t, p.isTeamAdminOrSystemAdmin("teamadmin", "team-id"))
	})

	t.Run("regular user blocked when unrestricted", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{})

		api.On("GetUser", "user").Return(&mmModel.User{
			Roles: mmModel.SystemUserRoleId,
		}, nil)
		api.On("GetTeamMember", "team-id", "user").Return(&mmModel.TeamMember{
			SchemeAdmin: false,
		}, nil)

		assert.False(t, p.isTeamAdminOrSystemAdmin("user", "team-id"))
	})

	t.Run("nil config defaults to unrestricted", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)

		api.On("GetUser", "teamadmin").Return(&mmModel.User{
			Roles: mmModel.TeamAdminRoleId,
		}, nil)
		api.On("GetTeamMember", "team-id", "teamadmin").Return(&mmModel.TeamMember{
			SchemeAdmin: true,
		}, nil)

		assert.True(t, p.isTeamAdminOrSystemAdmin("teamadmin", "team-id"))
	})
}

func TestIsChannelAdminOrHigher(t *testing.T) {
	t.Run("channel admin allowed when unrestricted", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{RestrictToSystemAdmins: new(false)})

		api.On("GetChannelMember", "chan-id", "chanadmin").Return(&mmModel.ChannelMember{
			SchemeAdmin: true,
		}, nil)

		assert.True(t, p.isChannelAdminOrHigher("chanadmin", "chan-id", "team-id"))
	})

	t.Run("channel admin blocked when restricted", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{RestrictToSystemAdmins: new(true)})

		api.On("GetUser", "chanadmin").Return(&mmModel.User{
			Roles: mmModel.SystemUserRoleId,
		}, nil)

		assert.False(t, p.isChannelAdminOrHigher("chanadmin", "chan-id", "team-id"))
	})

	t.Run("system admin passes when restricted via channel path", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{RestrictToSystemAdmins: new(true)})

		api.On("GetUser", "sysadmin").Return(&mmModel.User{
			Roles: mmModel.SystemAdminRoleId,
		}, nil)

		assert.True(t, p.isChannelAdminOrHigher("sysadmin", "chan-id", "team-id"))
	})

	t.Run("team admin passes channel check when unrestricted", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{RestrictToSystemAdmins: new(false)})

		api.On("GetChannelMember", "chan-id", "teamadmin").Return(&mmModel.ChannelMember{
			SchemeAdmin: false,
		}, nil)
		api.On("GetUser", "teamadmin").Return(&mmModel.User{
			Roles: mmModel.TeamAdminRoleId,
		}, nil)
		api.On("GetTeamMember", "team-id", "teamadmin").Return(&mmModel.TeamMember{
			SchemeAdmin: true,
		}, nil)

		assert.True(t, p.isChannelAdminOrHigher("teamadmin", "chan-id", "team-id"))
	})

	t.Run("team admin blocked via channel path when restricted", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)
		p.setConfiguration(&configuration{RestrictToSystemAdmins: new(true)})

		api.On("GetUser", "teamadmin").Return(&mmModel.User{
			Roles: mmModel.TeamAdminRoleId,
		}, nil)
		// Should not even reach GetChannelMember when restricted
		api.On("GetChannelMember", mock.Anything, mock.Anything).Unset()

		assert.False(t, p.isChannelAdminOrHigher("teamadmin", "chan-id", "team-id"))
	})
}

// addCmdLogMocks registers permissive log mocks on the given API so that
// LogInfo, LogWarn, LogError, and LogDebug calls never panic.
func addCmdLogMocks(api *plugintest.API) {
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogDebug", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
}

// singleOutboundConfig returns a configuration with one outbound connection named "high".
func singleOutboundConfig() *configuration {
	return &configuration{
		OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.high"}}]`,
	}
}

// multiConnectionConfig returns a configuration with one outbound and one inbound connection.
func multiConnectionConfig() *configuration {
	return &configuration{
		OutboundConnections: `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.out"}}]`,
		InboundConnections:  `[{"name":"high","nats":{"address":"nats://localhost:4222","subject":"crossguard.in"}}]`,
	}
}

func TestExecuteCommand(t *testing.T) {
	t.Run("no subcommand returns help hint", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		args := &mmModel.CommandArgs{Command: "/crossguard"}
		resp, appErr := p.ExecuteCommand(nil, args)
		assert.Nil(t, appErr)
		assert.Contains(t, resp.Text, "help")
	})

	t.Run("unknown subcommand returns error", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		args := &mmModel.CommandArgs{Command: "/crossguard foobar"}
		resp, appErr := p.ExecuteCommand(nil, args)
		assert.Nil(t, appErr)
		assert.Contains(t, resp.Text, "Unknown subcommand")
		assert.Contains(t, resp.Text, "foobar")
	})

	t.Run("dispatches help", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		args := &mmModel.CommandArgs{Command: "/crossguard help"}
		resp, appErr := p.ExecuteCommand(nil, args)
		assert.Nil(t, appErr)
		assert.Contains(t, resp.Text, "Cross Guard Help")
	})

	t.Run("dispatches init-team", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-team",
			UserId:  "user-id",
			TeamId:  "team-id",
		}
		resp, appErr := p.ExecuteCommand(nil, args)
		assert.Nil(t, appErr)
		assert.Contains(t, resp.Text, "permissions")
	})
}

func TestExecuteInitTeam(t *testing.T) {
	t.Run("permission denied for non-admin", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-team",
			UserId:  "user-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeam(args)
		assert.Contains(t, resp.Text, "don't have permissions")
		assert.Equal(t, mmModel.CommandResponseTypeEphemeral, resp.ResponseType)
	})

	t.Run("no connections configured", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeam(args)
		assert.Contains(t, resp.Text, "No connections configured")
	})

	t.Run("single connection auto-selects happy path", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		team := &mmModel.Team{Id: "team-id", Name: "test-team"}
		api.On("GetTeam", "team-id").Return(team, nil)

		tsChannel := &mmModel.Channel{Id: "ts-id"}
		api.On("GetChannelByName", "team-id", "town-square", false).Return(tsChannel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

		// Start with no team connections so the init succeeds as new
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, nil
		}
		kvs.addTeamConnectionFn = func(teamID string, conn store.TeamConnection) error {
			return nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeam(args)
		assert.Empty(t, resp.Text)
	})

	t.Run("explicit connection name happy path", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		team := &mmModel.Team{Id: "team-id", Name: "test-team"}
		api.On("GetTeam", "team-id").Return(team, nil)

		tsChannel := &mmModel.Channel{Id: "ts-id"}
		api.On("GetChannelByName", "team-id", "town-square", false).Return(tsChannel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, nil
		}
		kvs.addTeamConnectionFn = func(teamID string, conn store.TeamConnection) error {
			return nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-team outbound:high",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeam(args)
		assert.Empty(t, resp.Text)
	})

	t.Run("multiple connections without name opens dialog", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("OpenInteractiveDialog", mock.Anything).Return(nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-team",
			UserId:    "admin-id",
			TeamId:    "team-id",
			TriggerId: "trigger-id",
		}
		resp := p.executeInitTeam(args)
		assert.Empty(t, resp.Text)
		api.AssertCalled(t, "OpenInteractiveDialog", mock.Anything)
	})

	t.Run("invalid connection name", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-team outbound:nonexistent",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeam(args)
		assert.Contains(t, resp.Text, "connection not found")
		assert.Contains(t, resp.Text, "Available connections")
	})

	t.Run("already linked returns message", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		team := &mmModel.Team{Id: "team-id", Name: "test-team"}
		api.On("GetTeam", "team-id").Return(team, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard init-team outbound:high",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeInitTeam(args)
		assert.Contains(t, resp.Text, "already linked")
	})
}

func TestExecuteInitChannel(t *testing.T) {
	t.Run("permission denied", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetChannelMember", "chan-id", "user-id").Return(&mmModel.ChannelMember{SchemeAdmin: false}, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel",
			UserId:    "user-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Contains(t, resp.Text, "channel admin")
	})

	t.Run("team not initialized", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		// Override to return empty team connections (team not initialized)
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel outbound:high",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Contains(t, resp.Text, "must be initialized first")
	})

	t.Run("connection not linked to team", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "inbound", Connection: "high"}}, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel outbound:high",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Contains(t, resp.Text, "not linked to this team")
	})

	t.Run("happy path", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}
		kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		channel := &mmModel.Channel{Id: "chan-id", Name: "test-channel", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("UpdateChannel", mock.Anything).Return(&mmModel.Channel{}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel outbound:high",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Empty(t, resp.Text)
	})

	t.Run("already linked", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}
		kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}

		channel := &mmModel.Channel{Id: "chan-id", Name: "test-channel", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel outbound:high",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Contains(t, resp.Text, "already linked")
	})

	t.Run("no connections configured", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard init-channel",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeInitChannel(args)
		assert.Contains(t, resp.Text, "No connections configured")
	})
}

func TestExecuteTeardownTeam(t *testing.T) {
	t.Run("permission denied", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard teardown-team",
			UserId:  "user-id",
			TeamId:  "team-id",
		}
		resp := p.executeTeardownTeam(args)
		assert.Contains(t, resp.Text, "don't have permissions")
	})

	t.Run("no connections linked", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard teardown-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeTeardownTeam(args)
		assert.Contains(t, resp.Text, "No connections are linked")
	})

	t.Run("happy path single connection", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		team := &mmModel.Team{Id: "team-id", Name: "test-team"}
		api.On("GetTeam", "team-id").Return(team, nil)

		tsChannel := &mmModel.Channel{Id: "ts-id"}
		api.On("GetChannelByName", "team-id", "town-square", false).Return(tsChannel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

		conn := store.TeamConnection{Direction: "outbound", Connection: "high"}
		removed := false
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			if removed {
				return nil, nil
			}
			return []store.TeamConnection{conn}, nil
		}
		kvs.removeTeamConnectionFn = func(teamID string, c store.TeamConnection) error {
			removed = true
			return nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard teardown-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeTeardownTeam(args)
		assert.Empty(t, resp.Text)
		assert.True(t, removed)
	})

	t.Run("multiple connections without name opens dialog", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("OpenInteractiveDialog", mock.Anything).Return(nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "outbound", Connection: "high"},
				{Direction: "inbound", Connection: "high"},
			}, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard teardown-team",
			UserId:    "admin-id",
			TeamId:    "team-id",
			TriggerId: "trigger-id",
		}
		resp := p.executeTeardownTeam(args)
		assert.Empty(t, resp.Text)
		api.AssertCalled(t, "OpenInteractiveDialog", mock.Anything)
	})
}

func TestExecuteTeardownChannel(t *testing.T) {
	t.Run("permission denied", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetChannelMember", "chan-id", "user-id").Return(&mmModel.ChannelMember{SchemeAdmin: false}, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard teardown-channel",
			UserId:    "user-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeTeardownChannel(args)
		assert.Contains(t, resp.Text, "channel admin")
	})

	t.Run("no connections linked", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
			return nil, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard teardown-channel",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeTeardownChannel(args)
		assert.Contains(t, resp.Text, "No connections are linked")
	})

	t.Run("happy path single connection", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)
		api.On("GetChannelMember", "chan-id", "admin-id").Return(&mmModel.ChannelMember{SchemeAdmin: true}, nil)

		channel := &mmModel.Channel{Id: "chan-id", Name: "test-channel", TeamId: "team-id"}
		api.On("GetChannel", "chan-id").Return(channel, nil)
		api.On("UpdateChannel", mock.Anything).Return(&mmModel.Channel{}, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)
		api.On("PublishWebSocketEvent", mock.Anything, mock.Anything, mock.Anything).Maybe()

		conn := store.TeamConnection{Direction: "outbound", Connection: "high"}
		removed := false
		kvs.getChannelConnectionsFn = func(channelID string) ([]store.TeamConnection, error) {
			if removed {
				return nil, nil
			}
			return []store.TeamConnection{conn}, nil
		}
		kvs.removeChannelConnectionFn = func(channelID string, c store.TeamConnection) error {
			removed = true
			return nil
		}
		kvs.deleteChannelConnectionsFn = func(channelID string) error {
			return nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard teardown-channel",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeTeardownChannel(args)
		assert.Empty(t, resp.Text)
		assert.True(t, removed)
	})
}

func TestExecuteResetPrompt(t *testing.T) {
	t.Run("permission denied", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard reset-prompt high",
			UserId:  "user-id",
			TeamId:  "team-id",
		}
		resp := p.executeResetPrompt(args)
		assert.Contains(t, resp.Text, "team admin or system admin")
	})

	t.Run("missing connection name", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard reset-prompt",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeResetPrompt(args)
		assert.Contains(t, resp.Text, "Usage")
	})

	t.Run("no prompt found", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return nil, nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard reset-prompt high",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeResetPrompt(args)
		assert.Contains(t, resp.Text, "No prompt found")
	})

	t.Run("happy path clears prompt", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getConnectionPromptFn = func(teamID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: "pending", PostID: "post-1"}, nil
		}
		deleted := false
		kvs.deleteConnectionPromptFn = func(teamID, connName string) error {
			deleted = true
			return nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard reset-prompt high",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeResetPrompt(args)
		assert.Contains(t, resp.Text, "cleared")
		assert.True(t, deleted)
	})
}

func TestExecuteResetChannelPrompt(t *testing.T) {
	t.Run("permission denied", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard reset-channel-prompt high",
			UserId:    "user-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeResetChannelPrompt(args)
		assert.Contains(t, resp.Text, "team admin or system admin")
	})

	t.Run("missing connection name", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard reset-channel-prompt",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeResetChannelPrompt(args)
		assert.Contains(t, resp.Text, "Usage")
	})

	t.Run("no prompt found", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return nil, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard reset-channel-prompt high",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeResetChannelPrompt(args)
		assert.Contains(t, resp.Text, "No channel prompt found")
	})

	t.Run("happy path clears prompt", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getChannelConnectionPromptFn = func(channelID, connName string) (*store.ConnectionPrompt, error) {
			return &store.ConnectionPrompt{State: "blocked", PostID: "post-2"}, nil
		}
		deleted := false
		kvs.deleteChannelConnectionPromptFn = func(channelID, connName string) error {
			deleted = true
			return nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard reset-channel-prompt high",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeResetChannelPrompt(args)
		assert.Contains(t, resp.Text, "cleared")
		assert.True(t, deleted)
	})
}

func TestExecuteRewriteTeam(t *testing.T) {
	t.Run("permission denied", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)
		api.On("GetTeamMember", "team-id", "user-id").Return(&mmModel.TeamMember{SchemeAdmin: false}, nil)

		args := &mmModel.CommandArgs{
			Command: "/crossguard rewrite-team high remote-team",
			UserId:  "user-id",
			TeamId:  "team-id",
		}
		resp := p.executeRewriteTeam(args)
		assert.Contains(t, resp.Text, "team admin or system admin")
	})

	t.Run("no inbound connections", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard rewrite-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeRewriteTeam(args)
		assert.Contains(t, resp.Text, "No inbound connections")
	})

	t.Run("single inbound auto-selects and sets rewrite", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		tsChannel := &mmModel.Channel{Id: "ts-id"}
		api.On("GetChannelByName", "team-id", "town-square", false).Return(tsChannel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "inbound", Connection: "high"}}, nil
		}
		rewriteSet := false
		kvs.setTeamRewriteIndexFn = func(connName, remoteTeamName, localTeamID string) error {
			rewriteSet = true
			assert.Equal(t, "high", connName)
			assert.Equal(t, "remote-team", remoteTeamName)
			assert.Equal(t, "team-id", localTeamID)
			return nil
		}
		kvs.setTeamConnectionsFn = func(teamID string, conns []store.TeamConnection) error {
			return nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard rewrite-team high remote-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeRewriteTeam(args)
		assert.Contains(t, resp.Text, "Rewrite set")
		assert.Contains(t, resp.Text, "remote-team")
		assert.True(t, rewriteSet)
	})

	t.Run("clear rewrite happy path", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		tsChannel := &mmModel.Channel{Id: "ts-id"}
		api.On("GetChannelByName", "team-id", "town-square", false).Return(tsChannel, nil)
		api.On("CreatePost", mock.Anything).Return(&mmModel.Post{}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "inbound", Connection: "high", RemoteTeamName: "old-remote"}}, nil
		}
		kvs.setTeamConnectionsFn = func(teamID string, conns []store.TeamConnection) error {
			return nil
		}
		indexDeleted := false
		kvs.deleteTeamRewriteIndexFn = func(connName, remoteTeamName string) error {
			indexDeleted = true
			assert.Equal(t, "old-remote", remoteTeamName)
			return nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard rewrite-team high",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeRewriteTeam(args)
		assert.Contains(t, resp.Text, "Rewrite cleared")
		assert.True(t, indexDeleted)
	})

	t.Run("multiple inbound connections require name", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{
				{Direction: "inbound", Connection: "alpha"},
				{Direction: "inbound", Connection: "beta"},
			}, nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard rewrite-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeRewriteTeam(args)
		assert.Contains(t, resp.Text, "Multiple inbound connections")
	})

	t.Run("inbound connection name not found", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "inbound", Connection: "high"}}, nil
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard rewrite-team nonexistent remote-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeRewriteTeam(args)
		assert.Contains(t, resp.Text, "not linked to this team")
	})

	t.Run("set rewrite index error", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(multiConnectionConfig())

		adminUser := &mmModel.User{Id: "admin-id", Username: "admin", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "inbound", Connection: "high"}}, nil
		}
		kvs.setTeamRewriteIndexFn = func(connName, remoteTeamName, localTeamID string) error {
			return fmt.Errorf("conflict: rewrite already exists")
		}

		args := &mmModel.CommandArgs{
			Command: "/crossguard rewrite-team high remote-team",
			UserId:  "admin-id",
			TeamId:  "team-id",
		}
		resp := p.executeRewriteTeam(args)
		assert.Contains(t, resp.Text, "conflict")
	})
}

func TestExecuteStatus(t *testing.T) {
	t.Run("system admin gets global status", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		adminUser := &mmModel.User{Id: "admin-id", Roles: mmModel.SystemAdminRoleId}
		api.On("GetUser", "admin-id").Return(adminUser, nil)

		kvs.getInitializedTeamIDsFn = func() ([]string, error) {
			return []string{"team-id"}, nil
		}
		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}

		team := &mmModel.Team{Id: "team-id", Name: "test-team", DisplayName: "Test Team"}
		api.On("GetTeam", "team-id").Return(team, nil)
		api.On("GetChannel", "chan-id").Return(&mmModel.Channel{Id: "chan-id", Name: "test-chan", DisplayName: "Test Chan"}, nil)

		args := &mmModel.CommandArgs{
			Command:   "/crossguard status",
			UserId:    "admin-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeStatus(args)
		assert.Contains(t, resp.Text, "Cross Guard Status")
		assert.Contains(t, resp.Text, "Initialized Teams")
	})

	t.Run("regular user gets team status", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, kvs := setupTestPluginWithRouter(api)
		p.setConfiguration(singleOutboundConfig())

		regularUser := &mmModel.User{Id: "user-id", Roles: mmModel.SystemUserRoleId}
		api.On("GetUser", "user-id").Return(regularUser, nil)

		team := &mmModel.Team{Id: "team-id", Name: "test-team", DisplayName: "Test Team"}
		api.On("GetTeam", "team-id").Return(team, nil)
		api.On("GetChannel", "chan-id").Return(&mmModel.Channel{Id: "chan-id", Name: "test-chan", DisplayName: "Test Chan"}, nil)

		kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
			return []store.TeamConnection{{Direction: "outbound", Connection: "high"}}, nil
		}

		args := &mmModel.CommandArgs{
			Command:   "/crossguard status",
			UserId:    "user-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeStatus(args)
		assert.Contains(t, resp.Text, "Cross Guard Status")
		assert.NotContains(t, resp.Text, "Initialized Teams")
	})

	t.Run("GetUser failure", func(t *testing.T) {
		api := &plugintest.API{}
		addCmdLogMocks(api)
		p, _ := setupTestPluginWithRouter(api)
		p.setConfiguration(&configuration{})

		api.On("GetUser", "bad-id").Return(nil, &mmModel.AppError{Message: "not found"})

		args := &mmModel.CommandArgs{
			Command:   "/crossguard status",
			UserId:    "bad-id",
			TeamId:    "team-id",
			ChannelId: "chan-id",
		}
		resp := p.executeStatus(args)
		assert.Contains(t, resp.Text, "Failed to look up user")
	})
}

func TestExecuteHelp(t *testing.T) {
	t.Run("returns non-empty help text", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPluginWithRouter(api)
		resp := p.executeHelp()
		assert.Contains(t, resp.Text, "Cross Guard Help")
		assert.Equal(t, mmModel.CommandResponseTypeEphemeral, resp.ResponseType)
	})

	t.Run("help contains all commands", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPluginWithRouter(api)
		resp := p.executeHelp()
		assert.Contains(t, resp.Text, "init-team")
		assert.Contains(t, resp.Text, "init-channel")
		assert.Contains(t, resp.Text, "teardown-team")
		assert.Contains(t, resp.Text, "teardown-channel")
		assert.Contains(t, resp.Text, "reset-prompt")
		assert.Contains(t, resp.Text, "reset-channel-prompt")
		assert.Contains(t, resp.Text, "rewrite-team")
		assert.Contains(t, resp.Text, "status")
	})
}
