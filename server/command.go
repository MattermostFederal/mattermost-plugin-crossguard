package main

import (
	"fmt"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

const commandTrigger = "crossguard"

func (p *Plugin) registerCommand() error {
	return p.API.RegisterCommand(&model.Command{
		Trigger:          commandTrigger,
		AutoComplete:     true,
		AutoCompleteDesc: "Cross Guard commands",
		AutoCompleteHint: "[init-team|init-channel|teardown-team|teardown-channel|status]",
		AutocompleteData: getAutocompleteData(),
	})
}

func getAutocompleteData() *model.AutocompleteData {
	cmd := model.NewAutocompleteData(commandTrigger, "[command]", "Cross Guard commands")

	initTeam := model.NewAutocompleteData("init-team", "", "Initialize Cross Guard for this team (requires team admin or system admin)")
	cmd.AddCommand(initTeam)

	initChannel := model.NewAutocompleteData("init-channel", "", "Enable Cross Guard relay for this channel (requires channel admin or higher)")
	cmd.AddCommand(initChannel)

	teardownTeam := model.NewAutocompleteData("teardown-team", "", "Disable Cross Guard for this team (requires team admin or system admin)")
	cmd.AddCommand(teardownTeam)

	teardownChannel := model.NewAutocompleteData("teardown-channel", "", "Disable Cross Guard relay for this channel (requires channel admin or higher)")
	cmd.AddCommand(teardownChannel)

	status := model.NewAutocompleteData("status", "", "Check if Cross Guard has been initialized for this team")
	cmd.AddCommand(status)

	return cmd
}

func (p *Plugin) ExecuteCommand(_ *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	parts := strings.Fields(args.Command)
	if len(parts) < 2 {
		return respondEphemeral("Usage: /%s [init-team|init-channel|teardown-team|teardown-channel|status]", commandTrigger), nil
	}

	subcommand := parts[1]
	switch subcommand {
	case "init-team":
		return p.executeInitTeam(args), nil
	case "init-channel":
		return p.executeInitChannel(args), nil
	case "teardown-team":
		return p.executeTeardownTeam(args), nil
	case "teardown-channel":
		return p.executeTeardownChannel(args), nil
	case "status":
		return p.executeStatus(args), nil
	default:
		return respondEphemeral("Unknown subcommand: %s. Usage: /%s [init-team|init-channel|teardown-team|teardown-channel|status]", subcommand, commandTrigger), nil
	}
}

func (p *Plugin) executeInitTeam(args *model.CommandArgs) *model.CommandResponse {
	if !p.isTeamAdminOrSystemAdmin(args.UserId, args.TeamId) {
		return respondEphemeral("You don't have permissions to run this command. You must be a team admin or system admin.")
	}

	user, appErr := p.API.GetUser(args.UserId)
	if appErr != nil {
		return respondEphemeral("Failed to look up user.")
	}

	team, alreadyInit, svcErr := p.initTeamForCrossGuard(user, args.TeamId)
	if svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
	}

	if alreadyInit {
		return respondEphemeral("Cross Guard is already initialized for this team. (team ID: %s, team name: %s)", team.Id, team.Name)
	}

	return &model.CommandResponse{}
}

func (p *Plugin) executeStatus(args *model.CommandArgs) *model.CommandResponse {
	user, appErr := p.API.GetUser(args.UserId)
	if appErr != nil {
		return respondEphemeral("Failed to look up user: %s", appErr.Error())
	}

	if user.IsSystemAdmin() {
		return p.executeStatusSystemAdmin()
	}

	return p.executeStatusTeam(args.TeamId)
}

func (p *Plugin) executeStatusTeam(teamID string) *model.CommandResponse {
	resp, svcErr := p.getTeamStatus(teamID)
	if svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
	}

	if resp.Initialized {
		return respondEphemeral("Cross Guard is initialized for this team.")
	}

	return respondEphemeral("Cross Guard has not been initialized for this team. Run `/%s init-team` to initialize.", commandTrigger)
}

func (p *Plugin) executeStatusSystemAdmin() *model.CommandResponse {
	resp, svcErr := p.getGlobalStatus()
	if svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
	}

	if len(resp.Teams) == 0 {
		return respondEphemeral("No teams have been initialized. Run `/%s init-team` in a team to get started.", commandTrigger)
	}

	var sb strings.Builder
	sb.WriteString("#### Cross Guard Status\n\n")
	sb.WriteString("**Initialized Teams:**\n\n")
	sb.WriteString("| Team | Team ID | Team Name |\n")
	sb.WriteString("|:-----|:--------|:---------|\n")

	for _, team := range resp.Teams {
		fmt.Fprintf(&sb, "| %s | %s | %s |\n", team.DisplayName, team.TeamID, team.TeamName)
	}

	if len(resp.Warnings) > 0 {
		sb.WriteString("\n**Warning:** Failed to parse some connection configuration. Check System Console settings.\n")
	}

	if len(resp.Connections) > 0 {
		sb.WriteString("\n**NATS Connections:**\n\n")
		sb.WriteString("| Name | Direction | Address | Auth Type | Subject |\n")
		sb.WriteString("|:-----|:----------|:--------|:----------|:--------|\n")
		for _, conn := range resp.Connections {
			fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s |\n", conn.Name, conn.Direction, conn.Address, conn.AuthType, conn.Subject)
		}
	}

	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         sb.String(),
	}
}

func (p *Plugin) isTeamAdminOrSystemAdmin(userID, teamID string) bool {
	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		return false
	}

	if user.IsSystemAdmin() {
		return true
	}

	member, appErr := p.API.GetTeamMember(teamID, userID)
	if appErr != nil {
		return false
	}

	return member.SchemeAdmin
}

func (p *Plugin) executeInitChannel(args *model.CommandArgs) *model.CommandResponse {
	if !p.isChannelAdminOrHigher(args.UserId, args.ChannelId, args.TeamId) {
		return respondEphemeral("You must be a member of this channel and a channel admin, team admin, or system admin.")
	}

	user, appErr := p.API.GetUser(args.UserId)
	if appErr != nil {
		return respondEphemeral("Failed to look up user.")
	}

	ch, alreadyInit, svcErr := p.initChannelForCrossGuard(user, args.ChannelId)
	if svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
	}

	if alreadyInit {
		return respondEphemeral("Cross Guard relay is already enabled for this channel. (channel ID: %s, channel name: %s)", ch.Id, ch.Name)
	}

	return &model.CommandResponse{}
}

func (p *Plugin) executeTeardownChannel(args *model.CommandArgs) *model.CommandResponse {
	if !p.isChannelAdminOrHigher(args.UserId, args.ChannelId, args.TeamId) {
		return respondEphemeral("You must be a member of this channel and a channel admin, team admin, or system admin.")
	}

	user, appErr := p.API.GetUser(args.UserId)
	if appErr != nil {
		return respondEphemeral("Failed to look up user.")
	}

	if _, svcErr := p.teardownChannelForCrossGuard(user, args.ChannelId); svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
	}

	return &model.CommandResponse{}
}

func (p *Plugin) executeTeardownTeam(args *model.CommandArgs) *model.CommandResponse {
	if !p.isTeamAdminOrSystemAdmin(args.UserId, args.TeamId) {
		return respondEphemeral("You don't have permissions to run this command. You must be a team admin or system admin.")
	}

	user, appErr := p.API.GetUser(args.UserId)
	if appErr != nil {
		return respondEphemeral("Failed to look up user.")
	}

	if _, svcErr := p.teardownTeamForCrossGuard(user, args.TeamId); svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
	}

	return &model.CommandResponse{}
}

func (p *Plugin) isChannelAdminOrHigher(userID, channelID, teamID string) bool {
	member, appErr := p.API.GetChannelMember(channelID, userID)
	if appErr != nil || member == nil {
		return false
	}

	if member.SchemeAdmin {
		return true
	}

	return p.isTeamAdminOrSystemAdmin(userID, teamID)
}

func respondEphemeral(format string, a ...any) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         fmt.Sprintf(format, a...),
	}
}
