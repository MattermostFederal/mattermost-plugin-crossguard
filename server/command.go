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
		AutoCompleteHint: "[init-team|status]",
		AutocompleteData: getAutocompleteData(),
	})
}

func getAutocompleteData() *model.AutocompleteData {
	cmd := model.NewAutocompleteData(commandTrigger, "[command]", "Cross Guard commands")

	initTeam := model.NewAutocompleteData("init-team", "", "Initialize Cross Guard for this team (requires team admin or system admin)")
	cmd.AddCommand(initTeam)

	status := model.NewAutocompleteData("status", "", "Check if Cross Guard has been initialized for this team")
	cmd.AddCommand(status)

	return cmd
}

func (p *Plugin) ExecuteCommand(_ *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	parts := strings.Fields(args.Command)
	if len(parts) < 2 {
		return respondEphemeral("Usage: /%s [init-team|status]", commandTrigger), nil
	}

	subcommand := parts[1]
	switch subcommand {
	case "init-team":
		return p.executeInitTeam(args), nil
	case "status":
		return p.executeStatus(args), nil
	default:
		return respondEphemeral("Unknown subcommand: %s. Usage: /%s [init-team|status]", subcommand, commandTrigger), nil
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

	if _, svcErr := p.initTeamForCrossGuard(user, args.TeamId); svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
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

func respondEphemeral(format string, a ...any) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         fmt.Sprintf(format, a...),
	}
}
