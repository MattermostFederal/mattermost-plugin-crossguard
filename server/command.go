package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

const (
	commandTrigger        = "crossguard"
	actionInitTeam        = "init-team"
	actionTeardownTeam    = "teardown-team"
	actionInitChannel     = "init-channel"
	actionTeardownChannel = "teardown-channel"
)

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

	initTeam := model.NewAutocompleteData("init-team", "[connection-name]", "Link a NATS connection to this team (requires team admin or system admin)")
	cmd.AddCommand(initTeam)

	initChannel := model.NewAutocompleteData("init-channel", "[connection-name]", "Link a NATS connection to this channel (requires channel admin or higher)")
	cmd.AddCommand(initChannel)

	teardownTeam := model.NewAutocompleteData("teardown-team", "[connection-name]", "Unlink a NATS connection from this team (requires team admin or system admin)")
	cmd.AddCommand(teardownTeam)

	teardownChannel := model.NewAutocompleteData("teardown-channel", "[connection-name]", "Unlink a NATS connection from this channel (requires channel admin or higher)")
	cmd.AddCommand(teardownChannel)

	status := model.NewAutocompleteData("status", "", "Check Cross Guard status for this team")
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

	parts := strings.Fields(args.Command)
	inputName := ""
	if len(parts) >= 3 {
		inputName = parts[2]
	}

	connName, allConns, resolveErr := p.resolveConnectionName(inputName, p.getAllConnectionNames())
	if resolveErr != "" {
		if len(allConns) == 0 {
			return respondEphemeral("No NATS connections configured. Add connections in the System Console first.")
		}
		if inputName == "" && len(allConns) > 1 {
			p.sendConnectionPicker(args.UserId, args.ChannelId, args.TeamId, allConns, actionInitTeam)
			return &model.CommandResponse{}
		}
		return respondEphemeral("%s\n\nAvailable connections: %s", resolveErr, strings.Join(allConns, ", "))
	}

	team, alreadyLinked, svcErr := p.initTeamForCrossGuard(user, args.TeamId, connName)
	if svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
	}

	if alreadyLinked {
		return respondEphemeral("Connection `%s` is already linked to this team. (team ID: %s, team name: %s)", connName, team.Id, team.Name)
	}

	return &model.CommandResponse{}
}

func (p *Plugin) executeStatus(args *model.CommandArgs) *model.CommandResponse {
	user, appErr := p.API.GetUser(args.UserId)
	if appErr != nil {
		return respondEphemeral("Failed to look up user: %s", appErr.Error())
	}

	if user.IsSystemAdmin() {
		return p.executeStatusSystemAdmin(args.ChannelId)
	}

	return p.executeStatusTeam(args.TeamId, args.ChannelId)
}

func (p *Plugin) executeStatusTeam(teamID, channelID string) *model.CommandResponse {
	resp, svcErr := p.getTeamStatus(teamID)
	if svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
	}

	channelConns, _ := p.kvstore.GetChannelConnections(channelID)

	teamStatus := ":x: No"
	if resp.Initialized {
		teamStatus = ":white_check_mark: Yes"
	}

	channelStatus := ":x: No"
	if len(channelConns) > 0 {
		channelStatus = ":white_check_mark: Yes"
	}

	team, _ := p.API.GetTeam(teamID)
	channel, _ := p.API.GetChannel(channelID)

	teamName := teamID
	teamDisplayName := ""
	if team != nil {
		teamName = team.Name
		teamDisplayName = team.DisplayName
	}

	channelName := channelID
	channelDisplayName := ""
	if channel != nil {
		channelName = channel.Name
		channelDisplayName = channel.DisplayName
	}

	var sb strings.Builder
	sb.WriteString("#### Cross Guard Status\n\n")

	sb.WriteString("**Channel:**\n\n")
	sb.WriteString("| Channel | Name | ID | Relay Enabled |\n")
	sb.WriteString("|:--------|:-----|:---|:--------------|\n")
	fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", channelDisplayName, channelName, channelID, channelStatus)

	sb.WriteString("\n**Team:**\n\n")
	sb.WriteString("| Team | Name | ID | Initialized |\n")
	sb.WriteString("|:-----|:-----|:---|:------------|\n")
	fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", teamDisplayName, teamName, teamID, teamStatus)

	if len(resp.LinkedConnections) > 0 {
		sb.WriteString("\n**Team Connections:**\n\n")
		for _, conn := range resp.LinkedConnections {
			fmt.Fprintf(&sb, "- `%s`\n", conn)
		}
	}

	if len(channelConns) > 0 {
		sb.WriteString("\n**Channel Connections:**\n\n")
		for _, conn := range channelConns {
			fmt.Fprintf(&sb, "- `%s`\n", conn)
		}
	}

	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         sb.String(),
	}
}

func (p *Plugin) executeStatusSystemAdmin(channelID string) *model.CommandResponse {
	resp, svcErr := p.getGlobalStatus()
	if svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
	}

	if len(resp.Teams) == 0 {
		return respondEphemeral("No teams have been initialized. Run `/%s init-team` in a team to get started.", commandTrigger)
	}

	var sb strings.Builder
	sb.WriteString("#### Cross Guard Status\n\n")

	channelConns, _ := p.kvstore.GetChannelConnections(channelID)
	channelStatus := ":x: No"
	if len(channelConns) > 0 {
		channelStatus = ":white_check_mark: Yes"
	}
	channel, _ := p.API.GetChannel(channelID)
	channelName := channelID
	channelDisplayName := ""
	if channel != nil {
		channelName = channel.Name
		channelDisplayName = channel.DisplayName
	}

	sb.WriteString("**Channel:**\n\n")
	sb.WriteString("| Channel | Name | ID | Relay Enabled |\n")
	sb.WriteString("|:--------|:-----|:---|:--------------|\n")
	fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", channelDisplayName, channelName, channelID, channelStatus)

	if len(channelConns) > 0 {
		sb.WriteString("\n**Channel Connections:**\n\n")
		for _, conn := range channelConns {
			fmt.Fprintf(&sb, "- `%s`\n", conn)
		}
	}

	sb.WriteString("\n**Initialized Teams:**\n\n")
	sb.WriteString("| Team | Team ID | Team Name | Linked Connections |\n")
	sb.WriteString("|:-----|:--------|:----------|:-------------------|\n")

	for _, team := range resp.Teams {
		conns := "(none)"
		if len(team.LinkedConnections) > 0 {
			conns = strings.Join(team.LinkedConnections, ", ")
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", team.DisplayName, team.TeamID, team.TeamName, conns)
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

	teamConns, err := p.kvstore.GetTeamConnections(args.TeamId)
	if err != nil {
		return respondEphemeral("Failed to check team connections.")
	}
	if len(teamConns) == 0 {
		return respondEphemeral("Team must be initialized first. Run `/%s init-team` first.", commandTrigger)
	}

	parts := strings.Fields(args.Command)
	inputName := ""
	if len(parts) >= 3 {
		inputName = parts[2]
	}

	allConns := p.getAllConnectionNames()
	connName, _, resolveErr := p.resolveConnectionName(inputName, allConns)
	if resolveErr != "" {
		if len(allConns) == 0 {
			return respondEphemeral("No NATS connections configured. Check the System Console settings.")
		}
		if inputName == "" && len(allConns) > 1 {
			p.sendConnectionPicker(args.UserId, args.ChannelId, args.ChannelId, allConns, actionInitChannel)
			return &model.CommandResponse{}
		}
		return respondEphemeral("%s\n\nAvailable connections: %s", resolveErr, strings.Join(allConns, ", "))
	}

	if !slices.Contains(teamConns, connName) {
		return respondEphemeral("Connection `%s` is not linked to this team. Run `/%s init-team` first.", connName, commandTrigger)
	}

	ch, alreadyLinked, svcErr := p.initChannelForCrossGuard(user, args.ChannelId, connName)
	if svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
	}

	if alreadyLinked {
		return respondEphemeral("Connection `%s` is already linked to this channel. (channel ID: %s, channel name: %s)", connName, ch.Id, ch.Name)
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

	linked, err := p.kvstore.GetChannelConnections(args.ChannelId)
	if err != nil {
		return respondEphemeral("Failed to check channel connections.")
	}
	if len(linked) == 0 {
		return respondEphemeral("No connections are linked to this channel.")
	}

	parts := strings.Fields(args.Command)
	inputName := ""
	if len(parts) >= 3 {
		inputName = parts[2]
	}

	connName, _, resolveErr := p.resolveConnectionName(inputName, linked)
	if resolveErr != "" {
		if inputName == "" && len(linked) > 1 {
			p.sendConnectionPicker(args.UserId, args.ChannelId, args.ChannelId, linked, actionTeardownChannel)
			return &model.CommandResponse{}
		}
		return respondEphemeral("%s\n\nLinked connections: %s", resolveErr, strings.Join(linked, ", "))
	}

	if _, svcErr := p.teardownChannelForCrossGuard(user, args.ChannelId, connName); svcErr != nil {
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

	linked, err := p.kvstore.GetTeamConnections(args.TeamId)
	if err != nil {
		return respondEphemeral("Failed to check team connections.")
	}
	if len(linked) == 0 {
		return respondEphemeral("No connections are linked to this team.")
	}

	parts := strings.Fields(args.Command)
	inputName := ""
	if len(parts) >= 3 {
		inputName = parts[2]
	}

	connName, _, resolveErr := p.resolveConnectionName(inputName, linked)
	if resolveErr != "" {
		if inputName == "" && len(linked) > 1 {
			p.sendConnectionPicker(args.UserId, args.ChannelId, args.TeamId, linked, actionTeardownTeam)
			return &model.CommandResponse{}
		}
		return respondEphemeral("%s\n\nLinked connections: %s", resolveErr, strings.Join(linked, ", "))
	}

	if _, svcErr := p.teardownTeamForCrossGuard(user, args.TeamId, connName); svcErr != nil {
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

func (p *Plugin) sendConnectionPicker(userID, channelID, targetID string, connections []string, action string) {
	baseURL := fmt.Sprintf("/plugins/%s/api/v1/actions/select-connection", manifest.Id)

	var title string
	switch action {
	case actionInitTeam:
		title = "Select a connection to link to this team:"
	case actionTeardownTeam:
		title = "Select a connection to unlink from this team:"
	case actionInitChannel:
		title = "Select a connection to link to this channel:"
	case actionTeardownChannel:
		title = "Select a connection to unlink from this channel:"
	}

	ctx := map[string]any{
		"action":          action,
		"connection_name": "",
	}
	switch action {
	case actionInitTeam, actionTeardownTeam:
		ctx["team_id"] = targetID
	case actionInitChannel, actionTeardownChannel:
		ctx["channel_id"] = targetID
	}

	actions := make([]*model.PostAction, 0, len(connections))
	for _, conn := range connections {
		connCtx := make(map[string]any, len(ctx))
		maps.Copy(connCtx, ctx)
		connCtx["connection_name"] = conn
		actions = append(actions, &model.PostAction{
			Id:   conn,
			Name: conn,
			Type: model.PostActionTypeButton,
			Integration: &model.PostActionIntegration{
				URL:     baseURL,
				Context: connCtx,
			},
		})
	}

	actions = append(actions, &model.PostAction{
		Id:    "cancel",
		Name:  "Cancel",
		Type:  model.PostActionTypeButton,
		Style: "default",
		Integration: &model.PostActionIntegration{
			URL:     baseURL,
			Context: map[string]any{"action": "cancel"},
		},
	})

	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: channelID,
		Props: model.StringInterface{
			"attachments": []*model.SlackAttachment{
				{
					Title:   title,
					Actions: actions,
				},
			},
		},
	}

	p.API.SendEphemeralPost(userID, post)
}

func respondEphemeral(format string, a ...any) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         fmt.Sprintf(format, a...),
	}
}
