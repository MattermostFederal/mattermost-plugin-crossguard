package main

import (
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
