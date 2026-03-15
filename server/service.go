package main

import (
	"fmt"
	"slices"

	"github.com/mattermost/mattermost/server/public/model"
)

// apiError represents a structured error with an HTTP status code.
type apiError struct {
	Message string
	Status  int
}

func (e *apiError) Error() string {
	return e.Message
}

// TeamStatusResponse is the JSON response for a single team's initialization status.
type TeamStatusResponse struct {
	TeamID            string   `json:"team_id"`
	TeamName          string   `json:"team_name"`
	Initialized       bool     `json:"initialized"`
	LinkedConnections []string `json:"linked_connections"`
}

// TeamStatusEntry represents one initialized team in the global status response.
type TeamStatusEntry struct {
	TeamID            string   `json:"team_id"`
	TeamName          string   `json:"team_name"`
	DisplayName       string   `json:"display_name"`
	LinkedConnections []string `json:"linked_connections"`
}

// RedactedNATSConnection exposes only safe fields from a NATS connection config.
type RedactedNATSConnection struct {
	Name      string `json:"name"`
	Direction string `json:"direction"`
	Address   string `json:"address"`
	AuthType  string `json:"auth_type"`
	Subject   string `json:"subject"`
}

// GlobalStatusResponse is the JSON response for the system-wide status endpoint.
type GlobalStatusResponse struct {
	Teams       []TeamStatusEntry        `json:"teams"`
	Connections []RedactedNATSConnection `json:"connections"`
	Warnings    []string                 `json:"warnings,omitempty"`
}

// initTeamForCrossGuard links a connection to a team. If the team was not
// previously initialized, it also adds it to the initialized teams list and
// posts an announcement. Returns (team, alreadyLinked, error).
func (p *Plugin) initTeamForCrossGuard(user *model.User, teamID, connName string) (*model.Team, bool, *apiError) {
	team, appErr := p.API.GetTeam(teamID)
	if appErr != nil {
		return nil, false, &apiError{Message: "team not found", Status: 404}
	}

	existing, err := p.kvstore.GetTeamConnections(teamID)
	if err != nil {
		p.API.LogError("Failed to get team connections", "team_id", teamID, "error", err.Error())
		return nil, false, &apiError{Message: "failed to check team initialization state", Status: 500}
	}

	if slices.Contains(existing, connName) {
		return team, true, nil
	}

	if addErr := p.kvstore.AddTeamConnection(teamID, connName); addErr != nil {
		p.API.LogError("Failed to add team connection", "team_id", teamID, "conn", connName, "error", addErr.Error())
		return nil, false, &apiError{Message: "failed to save team initialization state", Status: 500}
	}

	updated, err := p.kvstore.GetTeamConnections(teamID)
	if err != nil {
		p.API.LogError("Failed to re-read team connections", "team_id", teamID, "error", err.Error())
		return nil, false, &apiError{Message: "failed to save team initialization state", Status: 500}
	}

	if len(updated) == 1 {
		if err := p.kvstore.AddInitializedTeamID(teamID); err != nil {
			p.API.LogError("Failed to add team to initialized list", "team_id", teamID, "error", err.Error())
			return nil, false, &apiError{Message: "failed to save team initialization state", Status: 500}
		}
	}

	channel, appErr := p.API.GetChannelByName(teamID, model.DefaultChannelName, false)
	if appErr == nil {
		post := &model.Post{
			UserId:    p.botUserID,
			ChannelId: channel.Id,
			Message:   fmt.Sprintf("Cross Guard connection `%s` linked to this team by @%s. (team ID: %s, team name: %s)", connName, user.Username, team.Id, team.Name),
		}
		if _, appErr := p.API.CreatePost(post); appErr != nil {
			p.API.LogWarn("Failed to post initialization message", "error", appErr.Error())
		}
	}

	return team, false, nil
}

// getTeamStatus returns the initialization status and linked connections for a team.
func (p *Plugin) getTeamStatus(teamID string) (*TeamStatusResponse, *apiError) {
	team, appErr := p.API.GetTeam(teamID)
	if appErr != nil {
		return nil, &apiError{Message: "team not found", Status: 404}
	}

	conns, err := p.kvstore.GetTeamConnections(teamID)
	if err != nil {
		p.API.LogError("Failed to check team status", "team_id", teamID, "error", err.Error())
		return nil, &apiError{Message: "failed to check team status", Status: 500}
	}

	return &TeamStatusResponse{
		TeamID:            team.Id,
		TeamName:          team.Name,
		Initialized:       len(conns) > 0,
		LinkedConnections: conns,
	}, nil
}

// getGlobalStatus returns the status of all initialized teams and redacted NATS connections.
func (p *Plugin) getGlobalStatus() (*GlobalStatusResponse, *apiError) {
	teamIDs, err := p.kvstore.GetInitializedTeamIDs()
	if err != nil {
		p.API.LogError("Failed to get initialized teams", "error", err.Error())
		return nil, &apiError{Message: "failed to get initialized teams", Status: 500}
	}

	teams := make([]TeamStatusEntry, 0, len(teamIDs))
	for _, teamID := range teamIDs {
		team, appErr := p.API.GetTeam(teamID)
		if appErr != nil {
			p.API.LogWarn("Failed to look up team for status", "team_id", teamID, "error", appErr.Error())
			teams = append(teams, TeamStatusEntry{TeamID: teamID, DisplayName: "(unknown)", TeamName: "(error)"})
			continue
		}

		conns, connErr := p.kvstore.GetTeamConnections(teamID)
		if connErr != nil {
			p.API.LogWarn("Failed to get team connections for status", "team_id", teamID, "error", connErr.Error())
		}

		teams = append(teams, TeamStatusEntry{
			TeamID:            team.Id,
			TeamName:          team.Name,
			DisplayName:       team.DisplayName,
			LinkedConnections: conns,
		})
	}

	cfg := p.getConfiguration()
	outbound, outErr := cfg.GetOutboundConnections()
	inbound, inErr := cfg.GetInboundConnections()

	connections := redactConnections(outbound, inbound)

	resp := &GlobalStatusResponse{
		Teams:       teams,
		Connections: connections,
	}

	if outErr != nil {
		p.API.LogWarn("Failed to parse outbound connection configuration", "error", outErr.Error())
		resp.Warnings = append(resp.Warnings, "Failed to parse outbound connection configuration")
	}
	if inErr != nil {
		p.API.LogWarn("Failed to parse inbound connection configuration", "error", inErr.Error())
		resp.Warnings = append(resp.Warnings, "Failed to parse inbound connection configuration")
	}

	return resp, nil
}

// initChannelForCrossGuard marks a channel for cross-domain relay.
func (p *Plugin) initChannelForCrossGuard(user *model.User, channelID string) (*model.Channel, bool, *apiError) {
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		return nil, false, &apiError{Message: "channel not found", Status: 404}
	}

	teamInit, err := p.kvstore.IsTeamInitialized(channel.TeamId)
	if err != nil {
		p.API.LogError("Failed to check team init status", "team_id", channel.TeamId, "error", err.Error())
		return nil, false, &apiError{Message: "failed to check team initialization status", Status: 500}
	}
	if !teamInit {
		return nil, false, &apiError{Message: "team must be initialized first (run /crossguard init-team)", Status: 400}
	}

	already, err := p.kvstore.GetChannelInitialized(channelID)
	if err == nil && already {
		return channel, true, nil
	}

	if err := p.kvstore.SetChannelInitialized(channelID); err != nil {
		p.API.LogError("Failed to store channel init state", "channel_id", channelID, "error", err.Error())
		return nil, false, &apiError{Message: "failed to save channel initialization state", Status: 500}
	}

	channel.Shared = model.NewPointer(true)
	if _, appErr := p.API.UpdateChannel(channel); appErr != nil {
		p.API.LogWarn("Failed to mark channel as shared", "error", appErr.Error())
	}

	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: channel.Id,
		Message:   fmt.Sprintf("Cross Guard relay enabled for this channel by @%s. (channel ID: %s, channel name: %s)", user.Username, channel.Id, channel.Name),
	}
	if _, appErr := p.API.CreatePost(post); appErr != nil {
		p.API.LogWarn("Failed to post channel init message", "error", appErr.Error())
	}

	return channel, false, nil
}

// teardownChannelForCrossGuard removes a channel from relay.
func (p *Plugin) teardownChannelForCrossGuard(user *model.User, channelID string) (*model.Channel, *apiError) {
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		return nil, &apiError{Message: "channel not found", Status: 404}
	}

	initialized, err := p.kvstore.GetChannelInitialized(channelID)
	if err == nil && !initialized {
		return channel, nil
	}

	if err := p.kvstore.DeleteChannelInitialized(channelID); err != nil {
		p.API.LogError("Failed to delete channel init state", "channel_id", channelID, "error", err.Error())
		return nil, &apiError{Message: "failed to remove channel initialization state", Status: 500}
	}

	channel.Shared = model.NewPointer(false)
	if _, appErr := p.API.UpdateChannel(channel); appErr != nil {
		p.API.LogWarn("Failed to unmark channel as shared", "error", appErr.Error())
	}

	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: channel.Id,
		Message:   fmt.Sprintf("Cross Guard relay disabled for this channel by @%s. (channel ID: %s, channel name: %s)", user.Username, channel.Id, channel.Name),
	}
	if _, appErr := p.API.CreatePost(post); appErr != nil {
		p.API.LogWarn("Failed to post channel teardown message", "error", appErr.Error())
	}

	return channel, nil
}

// teardownTeamForCrossGuard unlinks a connection from a team. If it was the
// last connection, the team is removed from the initialized list.
func (p *Plugin) teardownTeamForCrossGuard(user *model.User, teamID, connName string) (*model.Team, *apiError) {
	team, appErr := p.API.GetTeam(teamID)
	if appErr != nil {
		return nil, &apiError{Message: "team not found", Status: 404}
	}

	existing, err := p.kvstore.GetTeamConnections(teamID)
	if err != nil {
		p.API.LogError("Failed to get team connections", "team_id", teamID, "error", err.Error())
		return nil, &apiError{Message: "failed to check team initialization state", Status: 500}
	}

	if len(existing) == 0 {
		return team, nil
	}

	if !slices.Contains(existing, connName) {
		return nil, &apiError{Message: fmt.Sprintf("connection %q is not linked to this team", connName), Status: 400}
	}

	if removeErr := p.kvstore.RemoveTeamConnection(teamID, connName); removeErr != nil {
		p.API.LogError("Failed to remove team connection", "team_id", teamID, "conn", connName, "error", removeErr.Error())
		return nil, &apiError{Message: "failed to remove team connection", Status: 500}
	}

	updated, err := p.kvstore.GetTeamConnections(teamID)
	if err != nil {
		p.API.LogError("Failed to re-read team connections", "team_id", teamID, "error", err.Error())
		return nil, &apiError{Message: "failed to check team connection state", Status: 500}
	}

	if len(updated) == 0 {
		if err := p.kvstore.RemoveInitializedTeamID(teamID); err != nil {
			p.API.LogError("Failed to remove team from initialized list", "team_id", teamID, "error", err.Error())
			return nil, &apiError{Message: "failed to remove team from initialized list", Status: 500}
		}
	}

	channel, appErr := p.API.GetChannelByName(teamID, model.DefaultChannelName, false)
	if appErr == nil {
		msg := fmt.Sprintf("Cross Guard connection `%s` unlinked from this team by @%s.", connName, user.Username)
		if len(updated) == 0 {
			msg += " All channel relays in this team are now inactive."
		}
		post := &model.Post{
			UserId:    p.botUserID,
			ChannelId: channel.Id,
			Message:   msg,
		}
		if _, appErr := p.API.CreatePost(post); appErr != nil {
			p.API.LogWarn("Failed to post team teardown message", "error", appErr.Error())
		}
	}

	return team, nil
}

// getAllConnectionNames returns all direction-prefixed connection names from config.
func (p *Plugin) getAllConnectionNames() []string {
	cfg := p.getConfiguration()
	outbound, outErr := cfg.GetOutboundConnections()
	inbound, inErr := cfg.GetInboundConnections()

	var names []string
	if outErr != nil {
		p.API.LogWarn("Failed to parse outbound connections", "error", outErr.Error())
	} else {
		for _, conn := range outbound {
			names = append(names, "outbound-"+conn.Name)
		}
	}
	if inErr != nil {
		p.API.LogWarn("Failed to parse inbound connections", "error", inErr.Error())
	} else {
		for _, conn := range inbound {
			names = append(names, "inbound-"+conn.Name)
		}
	}
	return names
}

// resolveConnectionName resolves the connection name from the given name and available list.
// If connName is empty and there is exactly one connection, it auto-selects it.
// Returns (resolved name, available list, error message). A non-empty error message
// means the caller should report it to the user.
func (p *Plugin) resolveConnectionName(connName string, available []string) (string, []string, string) {
	if len(available) == 0 {
		return "", nil, "no NATS connections configured"
	}

	if connName == "" {
		if len(available) == 1 {
			return available[0], available, ""
		}
		return "", available, "multiple connections available, specify connection_name"
	}

	if !slices.Contains(available, connName) {
		return "", available, fmt.Sprintf("connection not found: %s", connName)
	}

	return connName, available, ""
}

// resolveLinkedConnectionName resolves the connection name from the linked list.
// If connName is empty and there is exactly one linked connection, it auto-selects it.
func resolveLinkedConnectionName(connName string, linked []string) (string, string) {
	if len(linked) == 0 {
		return "", "no connections linked to this team"
	}

	if connName == "" {
		if len(linked) == 1 {
			return linked[0], ""
		}
		return "", "multiple connections linked, specify connection_name"
	}

	if !slices.Contains(linked, connName) {
		return "", fmt.Sprintf("connection %q is not linked to this team", connName)
	}

	return connName, ""
}

// redactConnections strips sensitive fields from NATS connections for the status response.
func redactConnections(outbound, inbound []NATSConnection) []RedactedNATSConnection {
	connections := make([]RedactedNATSConnection, 0, len(outbound)+len(inbound))
	for _, conn := range outbound {
		connections = append(connections, RedactedNATSConnection{
			Name:      conn.Name,
			Direction: "outbound",
			Address:   conn.Address,
			AuthType:  conn.AuthType,
			Subject:   conn.Subject,
		})
	}
	for _, conn := range inbound {
		connections = append(connections, RedactedNATSConnection{
			Name:      conn.Name,
			Direction: "inbound",
			Address:   conn.Address,
			AuthType:  conn.AuthType,
			Subject:   conn.Subject,
		})
	}
	return connections
}
