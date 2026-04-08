package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
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
	TeamID            string                 `json:"team_id"`
	TeamName          string                 `json:"team_name"`
	TeamDisplayName   string                 `json:"team_display_name"`
	Initialized       bool                   `json:"initialized"`
	LinkedConnections []store.TeamConnection `json:"linked_connections"`
	Connections       []ConnectionStatus     `json:"connections"`
}

// TeamStatusEntry represents one initialized team in the global status response.
type TeamStatusEntry struct {
	TeamID            string                 `json:"team_id"`
	TeamName          string                 `json:"team_name"`
	DisplayName       string                 `json:"display_name"`
	LinkedConnections []store.TeamConnection `json:"linked_connections"`
}

// RedactedConnection exposes only safe fields from a connection config.
type RedactedConnection struct {
	Name                string `json:"name"`
	Direction           string `json:"direction"`
	Provider            string `json:"provider"`
	Address             string `json:"address,omitempty"`
	AuthType            string `json:"auth_type,omitempty"`
	Subject             string `json:"subject,omitempty"`
	FileTransferEnabled bool   `json:"file_transfer_enabled"`
	FileFilterMode      string `json:"file_filter_mode,omitempty"`
	FileFilterTypes     string `json:"file_filter_types,omitempty"`
	MessageFormat       string `json:"message_format,omitempty"`
	QueueName           string `json:"queue_name,omitempty"`
}

// GlobalStatusResponse is the JSON response for the system-wide status endpoint.
type GlobalStatusResponse struct {
	Teams       []TeamStatusEntry    `json:"teams"`
	Connections []RedactedConnection `json:"connections"`
	Warnings    []string             `json:"warnings,omitempty"`
}

const (
	crossguardHeaderPrefix = "\U0001F517 " // link emoji + space
)

// connKey returns a display key for a TeamConnection (e.g. "outbound:my-conn").
func connKey(tc store.TeamConnection) string {
	return tc.Direction + ":" + tc.Connection
}

// connectionDisplayNames formats a slice of TeamConnection as display strings.
func connectionDisplayNames(conns []store.TeamConnection) []string {
	names := make([]string, len(conns))
	for i, tc := range conns {
		names[i] = connKey(tc)
	}
	return names
}

func addCrossguardHeaderPrefix(header string) string {
	if strings.HasPrefix(header, crossguardHeaderPrefix) {
		return header
	}
	return crossguardHeaderPrefix + header
}

func removeCrossguardHeaderPrefix(header string) string {
	return strings.TrimPrefix(header, crossguardHeaderPrefix)
}

func (p *Plugin) publishChannelConnectionUpdate(channelID string, connections []store.TeamConnection) {
	keys := make([]string, len(connections))
	for i, tc := range connections {
		keys[i] = connKey(tc)
	}
	p.API.PublishWebSocketEvent("channel_connections_updated", map[string]any{
		"channel_id":  channelID,
		"connections": strings.Join(keys, ","),
	}, &model.WebsocketBroadcast{ChannelId: channelID})
}

// initTeamForCrossGuard links a connection to a team. If the team was not
// previously initialized, it also adds it to the initialized teams list and
// posts an announcement. Returns (team, alreadyLinked, error).
func (p *Plugin) initTeamForCrossGuard(user *model.User, teamID string, conn store.TeamConnection) (*model.Team, bool, *apiError) {
	team, appErr := p.API.GetTeam(teamID)
	if appErr != nil {
		return nil, false, &apiError{Message: "team not found", Status: 404}
	}

	existing, err := p.kvstore.GetTeamConnections(teamID)
	if err != nil {
		p.API.LogError("Failed to get team connections", "team_id", teamID, "error", err.Error())
		return nil, false, &apiError{Message: "failed to check team initialization state", Status: 500}
	}

	for _, tc := range existing {
		if tc.Matches(conn) {
			return team, true, nil
		}
	}

	if addErr := p.kvstore.AddTeamConnection(teamID, conn); addErr != nil {
		p.API.LogError("Failed to add team connection", "team_id", teamID, "conn", connKey(conn), "error", addErr.Error())
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
		displayName := connKey(conn)
		post := &model.Post{
			UserId:    p.botUserID,
			ChannelId: channel.Id,
			Message:   fmt.Sprintf("Cross Guard connection `%s` linked to this team by @%s. (team ID: %s, team name: %s)", displayName, user.Username, team.Id, team.Name),
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

	allConns := p.getAllConnectionNames()
	connMap := p.getConnectionMap()
	configSet := make(map[string]store.TeamConnection, len(allConns))
	connSet := make(map[string]store.TeamConnection)
	for _, tc := range allConns {
		key := connKey(tc)
		configSet[key] = tc
		connSet[key] = tc
	}
	for _, tc := range conns {
		key := connKey(tc)
		connSet[key] = tc
	}

	relevantKeys := make([]string, 0, len(connSet))
	for key := range connSet {
		relevantKeys = append(relevantKeys, key)
	}
	sort.Strings(relevantKeys)

	linkedSet := make(map[string]struct{}, len(conns))
	for _, tc := range conns {
		linkedSet[connKey(tc)] = struct{}{}
	}

	statuses := make([]ConnectionStatus, 0, len(relevantKeys))
	for _, key := range relevantKeys {
		tc := connSet[key]
		_, inConfig := configSet[key]
		_, isLinked := linkedSet[key]
		cc := connMap[key]
		statuses = append(statuses, ConnectionStatus{
			Name:                tc.Connection,
			Direction:           tc.Direction,
			Linked:              isLinked,
			Orphaned:            !inConfig,
			RemoteTeamName:      tc.RemoteTeamName,
			FileTransferEnabled: cc.FileTransferEnabled,
			FileFilterMode:      cc.FileFilterMode,
			FileFilterTypes:     cc.FileFilterTypes,
			MessageFormat:       cc.MessageFormat,
		})
	}

	return &TeamStatusResponse{
		TeamID:            team.Id,
		TeamName:          team.Name,
		TeamDisplayName:   team.DisplayName,
		Initialized:       len(conns) > 0,
		LinkedConnections: conns,
		Connections:       statuses,
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

// ChannelStatusResponse is the JSON response for a channel's connection status.
type ChannelStatusResponse struct {
	ChannelID          string             `json:"channel_id"`
	ChannelName        string             `json:"channel_name"`
	ChannelDisplayName string             `json:"channel_display_name"`
	TeamName           string             `json:"team_name"`
	TeamConnections    []ConnectionStatus `json:"team_connections"`
}

// ConnectionStatus represents a single connection and whether it is linked.
type ConnectionStatus struct {
	Name                string `json:"name"`
	Direction           string `json:"direction"`
	Linked              bool   `json:"linked"`
	Orphaned            bool   `json:"orphaned,omitempty"`
	RemoteTeamName      string `json:"remote_team_name,omitempty"`
	FileTransferEnabled bool   `json:"file_transfer_enabled"`
	FileFilterMode      string `json:"file_filter_mode,omitempty"`
	FileFilterTypes     string `json:"file_filter_types,omitempty"`
	MessageFormat       string `json:"message_format,omitempty"`
}

// getChannelStatus returns the connection status for a channel, showing
// team-linked connections and any orphaned channel connections.
func (p *Plugin) getChannelStatus(channelID string) (*ChannelStatusResponse, *apiError) {
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		return nil, &apiError{Message: "channel not found", Status: 404}
	}

	if channel.Type == model.ChannelTypeDirect || channel.Type == model.ChannelTypeGroup {
		return nil, &apiError{Message: "Cross Guard is not available for direct or group messages", Status: 400}
	}

	team, appErr := p.API.GetTeam(channel.TeamId)
	if appErr != nil {
		return nil, &apiError{Message: "team not found", Status: 404}
	}

	channelConns, err := p.kvstore.GetChannelConnections(channelID)
	if err != nil {
		p.API.LogError("Failed to get channel connections", "channel_id", channelID, "error", err.Error())
		return nil, &apiError{Message: "failed to get channel connections", Status: 500}
	}

	teamConns, err := p.kvstore.GetTeamConnections(channel.TeamId)
	if err != nil {
		p.API.LogError("Failed to get team connections", "team_id", channel.TeamId, "error", err.Error())
		return nil, &apiError{Message: "failed to get team connections", Status: 500}
	}

	allConns := p.getAllConnectionNames()
	connMap := p.getConnectionMap()
	configSet := make(map[string]store.TeamConnection, len(allConns))
	connSet := make(map[string]store.TeamConnection)
	for _, tc := range allConns {
		key := connKey(tc)
		configSet[key] = tc
	}
	for _, tc := range teamConns {
		key := connKey(tc)
		connSet[key] = tc
	}
	for _, tc := range channelConns {
		key := connKey(tc)
		if _, exists := connSet[key]; !exists {
			connSet[key] = tc
		}
	}

	relevantKeys := make([]string, 0, len(connSet))
	for key := range connSet {
		relevantKeys = append(relevantKeys, key)
	}
	sort.Strings(relevantKeys)

	channelLinkedSet := make(map[string]struct{}, len(channelConns))
	for _, tc := range channelConns {
		channelLinkedSet[connKey(tc)] = struct{}{}
	}

	statuses := make([]ConnectionStatus, 0, len(relevantKeys))
	for _, key := range relevantKeys {
		tc := connSet[key]
		_, inConfig := configSet[key]
		_, isLinked := channelLinkedSet[key]
		cc := connMap[key]
		statuses = append(statuses, ConnectionStatus{
			Name:                tc.Connection,
			Direction:           tc.Direction,
			Linked:              isLinked,
			Orphaned:            !inConfig,
			RemoteTeamName:      tc.RemoteTeamName,
			FileTransferEnabled: cc.FileTransferEnabled,
			FileFilterMode:      cc.FileFilterMode,
			FileFilterTypes:     cc.FileFilterTypes,
			MessageFormat:       cc.MessageFormat,
		})
	}

	return &ChannelStatusResponse{
		ChannelID:          channel.Id,
		ChannelName:        channel.Name,
		ChannelDisplayName: channel.DisplayName,
		TeamName:           team.DisplayName,
		TeamConnections:    statuses,
	}, nil
}

// initChannelForCrossGuard links a connection to a channel. If the channel did
// not previously have any connections, it also marks the channel as shared and
// posts an announcement. Returns (channel, alreadyLinked, error).
func (p *Plugin) initChannelForCrossGuard(user *model.User, channelID string, conn store.TeamConnection) (*model.Channel, bool, *apiError) {
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		return nil, false, &apiError{Message: "channel not found", Status: 404}
	}

	teamConns, err := p.kvstore.GetTeamConnections(channel.TeamId)
	if err != nil {
		p.API.LogError("Failed to get team connections", "team_id", channel.TeamId, "error", err.Error())
		return nil, false, &apiError{Message: "failed to check team initialization state", Status: 500}
	}

	teamHasConn := false
	for _, tc := range teamConns {
		if tc.Matches(conn) {
			teamHasConn = true
			break
		}
	}
	if !teamHasConn {
		if _, _, svcErr := p.initTeamForCrossGuard(user, channel.TeamId, conn); svcErr != nil {
			return nil, false, svcErr
		}
	}

	existing, err := p.kvstore.GetChannelConnections(channelID)
	if err != nil {
		p.API.LogError("Failed to get channel connections", "channel_id", channelID, "error", err.Error())
		return nil, false, &apiError{Message: "failed to check channel connection state", Status: 500}
	}

	for _, tc := range existing {
		if tc.Matches(conn) {
			return channel, true, nil
		}
	}

	if addErr := p.kvstore.AddChannelConnection(channelID, conn); addErr != nil {
		p.API.LogError("Failed to add channel connection", "channel_id", channelID, "conn", connKey(conn), "error", addErr.Error())
		return nil, false, &apiError{Message: "failed to save channel connection state", Status: 500}
	}

	updated, err := p.kvstore.GetChannelConnections(channelID)
	if err != nil {
		p.API.LogError("Failed to re-read channel connections", "channel_id", channelID, "error", err.Error())
		return nil, false, &apiError{Message: "failed to save channel connection state", Status: 500}
	}

	p.publishChannelConnectionUpdate(channelID, updated)

	freshChannel, appErr := p.API.GetChannel(channelID)
	if appErr == nil {
		freshChannel.Header = addCrossguardHeaderPrefix(freshChannel.Header)
		if _, appErr := p.API.UpdateChannel(freshChannel); appErr != nil {
			p.API.LogWarn("Failed to update channel header with CrossGuard prefix", "channel_id", channelID, "error", appErr.Error())
		}
	}

	displayName := connKey(conn)
	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: channel.Id,
		Message:   fmt.Sprintf("Cross Guard connection `%s` linked to this channel by @%s. (channel ID: %s, channel name: %s)", displayName, user.Username, channel.Id, channel.Name),
	}
	if _, appErr := p.API.CreatePost(post); appErr != nil {
		p.API.LogWarn("Failed to post channel init message", "error", appErr.Error())
	}

	return channel, false, nil
}

// teardownChannelForCrossGuard unlinks a connection from a channel. If it was
// the last connection, the channel connections are deleted and the channel is
// unmarked as shared.
func (p *Plugin) teardownChannelForCrossGuard(user *model.User, channelID string, conn store.TeamConnection) (*model.Channel, *apiError) {
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		return nil, &apiError{Message: "channel not found", Status: 404}
	}

	existing, err := p.kvstore.GetChannelConnections(channelID)
	if err != nil {
		p.API.LogError("Failed to get channel connections", "channel_id", channelID, "error", err.Error())
		return nil, &apiError{Message: "failed to check channel connection state", Status: 500}
	}

	if len(existing) == 0 {
		return channel, nil
	}

	found := false
	for _, tc := range existing {
		if tc.Matches(conn) {
			found = true
			break
		}
	}
	if !found {
		return nil, &apiError{Message: fmt.Sprintf("connection %q is not linked to this channel", connKey(conn)), Status: 400}
	}

	if removeErr := p.kvstore.RemoveChannelConnection(channelID, conn); removeErr != nil {
		p.API.LogError("Failed to remove channel connection", "channel_id", channelID, "conn", connKey(conn), "error", removeErr.Error())
		return nil, &apiError{Message: "failed to remove channel connection", Status: 500}
	}

	updated, err := p.kvstore.GetChannelConnections(channelID)
	if err != nil {
		p.API.LogError("Failed to re-read channel connections", "channel_id", channelID, "error", err.Error())
		return nil, &apiError{Message: "failed to check channel connection state", Status: 500}
	}

	if len(updated) == 0 {
		if delErr := p.kvstore.DeleteChannelConnections(channelID); delErr != nil {
			p.API.LogError("Failed to delete channel connections", "channel_id", channelID, "error", delErr.Error())
			return nil, &apiError{Message: "failed to remove channel connections", Status: 500}
		}

		freshChannel, fErr := p.API.GetChannel(channelID)
		if fErr == nil {
			freshChannel.Header = removeCrossguardHeaderPrefix(freshChannel.Header)
			if _, appErr := p.API.UpdateChannel(freshChannel); appErr != nil {
				p.API.LogWarn("Failed to remove CrossGuard prefix from channel header", "channel_id", channelID, "error", appErr.Error())
			}
		}

		p.publishChannelConnectionUpdate(channelID, nil)
	} else {
		p.publishChannelConnectionUpdate(channelID, updated)
	}

	displayName := connKey(conn)
	msg := fmt.Sprintf("Cross Guard connection `%s` unlinked from this channel by @%s.", displayName, user.Username)
	if len(updated) == 0 {
		msg += " All relays for this channel are now inactive."
	}
	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: channel.Id,
		Message:   msg,
	}
	if _, appErr := p.API.CreatePost(post); appErr != nil {
		p.API.LogWarn("Failed to post channel teardown message", "error", appErr.Error())
	}

	return channel, nil
}

// teardownTeamForCrossGuard unlinks a connection from a team. If it was the
// last connection, the team is removed from the initialized list.
func (p *Plugin) teardownTeamForCrossGuard(user *model.User, teamID string, conn store.TeamConnection) (*model.Team, *apiError) {
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

	found := false
	for _, tc := range existing {
		if tc.Matches(conn) {
			found = true
			break
		}
	}
	if !found {
		return nil, &apiError{Message: fmt.Sprintf("connection %q is not linked to this team", connKey(conn)), Status: 400}
	}

	if removeErr := p.kvstore.RemoveTeamConnection(teamID, conn); removeErr != nil {
		p.API.LogError("Failed to remove team connection", "team_id", teamID, "conn", connKey(conn), "error", removeErr.Error())
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
		displayName := connKey(conn)
		msg := fmt.Sprintf("Cross Guard connection `%s` unlinked from this team by @%s.", displayName, user.Username)
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

// getAllConnectionNames returns all connections from config as TeamConnection structs.
func (p *Plugin) getAllConnectionNames() []store.TeamConnection {
	cfg := p.getConfiguration()
	outbound, outErr := cfg.GetOutboundConnections()
	inbound, inErr := cfg.GetInboundConnections()

	var conns []store.TeamConnection
	if outErr != nil {
		p.API.LogWarn("Failed to parse outbound connections", "error", outErr.Error())
	} else {
		for _, conn := range outbound {
			conns = append(conns, store.TeamConnection{Direction: "outbound", Connection: conn.Name})
		}
	}
	if inErr != nil {
		p.API.LogWarn("Failed to parse inbound connections", "error", inErr.Error())
	} else {
		for _, conn := range inbound {
			conns = append(conns, store.TeamConnection{Direction: "inbound", Connection: conn.Name})
		}
	}
	return conns
}

// getConnectionMap returns a map of "direction:name" to ConnectionConfig for config lookups.
func (p *Plugin) getConnectionMap() map[string]ConnectionConfig {
	cfg := p.getConfiguration()
	outbound, outErr := cfg.GetOutboundConnections()
	inbound, inErr := cfg.GetInboundConnections()

	m := make(map[string]ConnectionConfig, len(outbound)+len(inbound))
	if outErr != nil {
		p.API.LogWarn("Failed to parse outbound connections for map", "error", outErr.Error())
	} else {
		for _, conn := range outbound {
			m["outbound:"+conn.Name] = conn
		}
	}
	if inErr != nil {
		p.API.LogWarn("Failed to parse inbound connections for map", "error", inErr.Error())
	} else {
		for _, conn := range inbound {
			m["inbound:"+conn.Name] = conn
		}
	}
	return m
}

// resolveConnectionName resolves the connection name from the given name and available list.
// If connName is empty and there is exactly one connection, it auto-selects it.
// Returns (resolved connection, available list, error message). A non-empty error message
// means the caller should report it to the user.
func (p *Plugin) resolveConnectionName(connName string, available []store.TeamConnection) (store.TeamConnection, []store.TeamConnection, string) {
	if len(available) == 0 {
		return store.TeamConnection{}, nil, "no connections configured"
	}

	if connName == "" {
		if len(available) == 1 {
			return available[0], available, ""
		}
		return store.TeamConnection{}, available, "multiple connections available, specify connection_name"
	}

	for _, tc := range available {
		if connKey(tc) == connName {
			return tc, available, ""
		}
	}

	return store.TeamConnection{}, available, fmt.Sprintf("connection not found: %s", connName)
}

// redactConnections strips sensitive fields from connections for the status response.
func redactConnections(outbound, inbound []ConnectionConfig) []RedactedConnection {
	connections := make([]RedactedConnection, 0, len(outbound)+len(inbound))
	for _, conn := range outbound {
		connections = append(connections, redactConnection(conn, "outbound"))
	}
	for _, conn := range inbound {
		connections = append(connections, redactConnection(conn, "inbound"))
	}
	return connections
}

func redactConnection(conn ConnectionConfig, direction string) RedactedConnection {
	rc := RedactedConnection{
		Name:                conn.Name,
		Direction:           direction,
		Provider:            conn.Provider,
		FileTransferEnabled: conn.FileTransferEnabled,
		FileFilterMode:      conn.FileFilterMode,
		FileFilterTypes:     conn.FileFilterTypes,
		MessageFormat:       conn.MessageFormat,
	}
	if conn.NATS != nil {
		rc.Address = conn.NATS.Address
		rc.AuthType = conn.NATS.AuthType
		rc.Subject = conn.NATS.Subject
	}
	if conn.Azure != nil {
		rc.QueueName = conn.Azure.QueueName
	}
	return rc
}
