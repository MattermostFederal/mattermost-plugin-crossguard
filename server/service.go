package main

import (
	"fmt"

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
	TeamID      string `json:"team_id"`
	TeamName    string `json:"team_name"`
	Initialized bool   `json:"initialized"`
}

// TeamStatusEntry represents one initialized team in the global status response.
type TeamStatusEntry struct {
	TeamID      string `json:"team_id"`
	TeamName    string `json:"team_name"`
	DisplayName string `json:"display_name"`
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

// initTeamForCrossGuard initializes Cross Guard for a team: writes KV state and
// posts an announcement to town-square. Accepts the authenticated user to avoid
// redundant API lookups.
func (p *Plugin) initTeamForCrossGuard(user *model.User, teamID string) (*model.Team, *apiError) {
	team, appErr := p.API.GetTeam(teamID)
	if appErr != nil {
		return nil, &apiError{Message: "team not found", Status: 404}
	}

	already, err := p.kvstore.GetTeamInitialized(teamID)
	if err == nil && already {
		return team, nil
	}

	if err := p.kvstore.SetTeamInitialized(teamID); err != nil {
		p.API.LogError("Failed to store team init state", "team_id", teamID, "error", err.Error())
		return nil, &apiError{Message: "failed to save team initialization state", Status: 500}
	}

	if err := p.kvstore.AddInitializedTeamID(teamID); err != nil {
		p.API.LogError("Failed to add team to initialized list", "team_id", teamID, "error", err.Error())
		return nil, &apiError{Message: "failed to save team initialization state", Status: 500}
	}

	channel, appErr := p.API.GetChannelByName(teamID, model.DefaultChannelName, false)
	if appErr == nil {
		post := &model.Post{
			UserId:    p.botUserID,
			ChannelId: channel.Id,
			Message:   fmt.Sprintf("Cross Guard initialized for this team by @%s. (team ID: %s, team name: %s)", user.Username, team.Id, team.Name),
		}
		if _, appErr := p.API.CreatePost(post); appErr != nil {
			p.API.LogWarn("Failed to post initialization message", "error", appErr.Error())
		}
	}

	return team, nil
}

// getTeamStatus returns whether a team has been initialized for Cross Guard.
func (p *Plugin) getTeamStatus(teamID string) (*TeamStatusResponse, *apiError) {
	team, appErr := p.API.GetTeam(teamID)
	if appErr != nil {
		return nil, &apiError{Message: "team not found", Status: 404}
	}

	initialized, err := p.kvstore.GetTeamInitialized(teamID)
	if err != nil {
		p.API.LogError("Failed to check team status", "team_id", teamID, "error", err.Error())
		return nil, &apiError{Message: "failed to check team status", Status: 500}
	}

	return &TeamStatusResponse{
		TeamID:      team.Id,
		TeamName:    team.Name,
		Initialized: initialized,
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
		teams = append(teams, TeamStatusEntry{
			TeamID:      team.Id,
			TeamName:    team.Name,
			DisplayName: team.DisplayName,
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
