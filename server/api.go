package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/nats-io/nats.go"
)

const maxRequestBodySize = 5 << 20 // 5 MB

func (p *Plugin) initAPI() {
	router := mux.NewRouter()
	router.HandleFunc("/api/v1/test-connection", p.handleTestConnection).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/teams/{team_id}/init", p.handleInitTeam).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/teams/{team_id}/teardown", p.handleTeardownTeam).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/channels/{channel_id}/init", p.handleInitChannel).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/channels/{channel_id}/teardown", p.handleTeardownChannel).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/dialog/select-connection", p.handleDialogSelectConnection).Methods(http.MethodPost)
	router.HandleFunc("/api/v1/status", p.handleGlobalStatus).Methods(http.MethodGet)
	router.HandleFunc("/api/v1/teams/{team_id}/status", p.handleTeamStatus).Methods(http.MethodGet)
	router.HandleFunc("/api/v1/channels/{channel_id}/status", p.handleChannelStatus).Methods(http.MethodGet)
	p.router = router
}

func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	p.router.ServeHTTP(w, r)
}

func (p *Plugin) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	if !user.IsSystemAdmin() {
		writeJSONError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var conn NATSConnection
	if err := json.NewDecoder(r.Body).Decode(&conn); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(conn.Address) == "" {
		writeJSONError(w, "address is required", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(conn.Subject) == "" {
		writeJSONError(w, "subject is required", http.StatusBadRequest)
		return
	}

	if !strings.HasPrefix(conn.Subject, subjectPrefix) {
		writeJSONError(w, fmt.Sprintf("subject must start with %q", subjectPrefix), http.StatusBadRequest)
		return
	}

	switch conn.AuthType {
	case AuthTypeNone, AuthTypeToken, AuthTypeCredentials, "":
		// valid
	default:
		writeJSONError(w, "auth_type must be \"none\", \"token\", or \"credentials\"", http.StatusBadRequest)
		return
	}

	if conn.AuthType == AuthTypeToken && strings.TrimSpace(conn.Token) == "" {
		writeJSONError(w, "token is required when auth_type is \"token\"", http.StatusBadRequest)
		return
	}

	if conn.AuthType == AuthTypeCredentials {
		if strings.TrimSpace(conn.Username) == "" || strings.TrimSpace(conn.Password) == "" {
			writeJSONError(w, "username and password are required when auth_type is \"credentials\"", http.StatusBadRequest)
			return
		}
	}

	nc, err := connectNATS(conn)
	if err != nil {
		p.API.LogError("NATS connection test failed", "address", conn.Address, "error", err.Error())
		writeJSONError(w, "failed to connect to NATS server", http.StatusBadGateway)
		return
	}
	defer nc.Close()

	direction := r.URL.Query().Get("direction")

	switch direction {
	case "inbound":
		p.handleTestInbound(w, nc, conn)
	case "outbound", "":
		p.handleTestOutbound(w, nc, conn)
	default:
		writeJSONError(w, "direction must be \"inbound\" or \"outbound\"", http.StatusBadRequest)
	}
}

func (p *Plugin) handleTestOutbound(w http.ResponseWriter, nc *nats.Conn, conn NATSConnection) {
	data, msgID, err := buildTestMessage()
	if err != nil {
		p.API.LogError("Failed to build test message", "error", err.Error())
		writeJSONError(w, "failed to build test message", http.StatusInternalServerError)
		return
	}

	if err := nc.Publish(conn.Subject, data); err != nil {
		p.API.LogError("Failed to publish test message", "subject", conn.Subject, "error", err.Error())
		writeJSONError(w, "failed to publish test message", http.StatusBadGateway)
		return
	}

	if err := nc.Flush(); err != nil {
		p.API.LogError("Failed to flush test message", "subject", conn.Subject, "error", err.Error())
		writeJSONError(w, "failed to confirm test message delivery", http.StatusBadGateway)
		return
	}

	p.API.LogInfo("Test message sent", "subject", conn.Subject, "msg_id", msgID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Test message sent with ID: " + msgID,
		"id":      msgID,
	})
}

func (p *Plugin) handleTestInbound(w http.ResponseWriter, nc *nats.Conn, conn NATSConnection) {
	sub, err := nc.SubscribeSync(conn.Subject)
	if err != nil {
		p.API.LogError("Failed to subscribe for inbound test", "subject", conn.Subject, "error", err.Error())
		writeJSONError(w, "failed to subscribe to subject", http.StatusBadGateway)
		return
	}
	defer func() { _ = sub.Unsubscribe() }()

	if err := nc.Flush(); err != nil {
		p.API.LogError("Failed to flush NATS connection", "error", err.Error())
		writeJSONError(w, "failed to flush NATS connection", http.StatusBadGateway)
		return
	}

	p.API.LogInfo("Inbound test subscription successful", "subject", conn.Subject)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Connected and subscribed to subject successfully",
	})
}

func writeJSONError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// getAuthenticatedUser extracts the user ID from the request header, looks up
// the user, and returns it. Returns nil and writes an error response if auth fails.
func (p *Plugin) getAuthenticatedUser(w http.ResponseWriter, r *http.Request) *model.User {
	userID := r.Header.Get("Mattermost-User-Id")
	if userID == "" {
		writeJSONError(w, "not authenticated", http.StatusUnauthorized)
		return nil
	}

	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		p.API.LogError("Failed to get user", "user_id", userID, "error", appErr.Error())
		writeJSONError(w, "failed to get user", http.StatusInternalServerError)
		return nil
	}

	return user
}

func (p *Plugin) handleInitTeam(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	teamID := mux.Vars(r)["team_id"]
	if !model.IsValidId(teamID) {
		writeJSONError(w, "invalid team_id", http.StatusBadRequest)
		return
	}

	if !p.isTeamAdminOrSystemAdmin(user.Id, teamID) {
		writeJSONError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	connName, allConns, resolveErr := p.resolveConnectionName(r.URL.Query().Get("connection_name"), p.getAllConnectionNames())
	if resolveErr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":       resolveErr,
			"connections": allConns,
		})
		return
	}

	team, _, svcErr := p.initTeamForCrossGuard(user, teamID, connName)
	if svcErr != nil {
		writeJSONError(w, svcErr.Message, svcErr.Status)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":          "ok",
		"team_id":         team.Id,
		"team_name":       team.Name,
		"connection_name": connName,
	})
}

func (p *Plugin) handleInitChannel(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	channelID := mux.Vars(r)["channel_id"]
	if !model.IsValidId(channelID) {
		writeJSONError(w, "invalid channel_id", http.StatusBadRequest)
		return
	}

	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		writeJSONError(w, "channel not found", http.StatusNotFound)
		return
	}

	if !p.isChannelAdminOrHigher(user.Id, channelID, channel.TeamId) {
		writeJSONError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	connName, allConns, resolveErr := p.resolveConnectionName(r.URL.Query().Get("connection_name"), p.getAllConnectionNames())
	if resolveErr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":       resolveErr,
			"connections": allConns,
		})
		return
	}

	ch, _, svcErr := p.initChannelForCrossGuard(user, channelID, connName)
	if svcErr != nil {
		writeJSONError(w, svcErr.Message, svcErr.Status)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":          "ok",
		"channel_id":      ch.Id,
		"channel_name":    ch.Name,
		"connection_name": connName,
	})
}

func (p *Plugin) handleTeardownChannel(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	channelID := mux.Vars(r)["channel_id"]
	if !model.IsValidId(channelID) {
		writeJSONError(w, "invalid channel_id", http.StatusBadRequest)
		return
	}

	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		writeJSONError(w, "channel not found", http.StatusNotFound)
		return
	}

	if !p.isChannelAdminOrHigher(user.Id, channelID, channel.TeamId) {
		writeJSONError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	teamConns, err := p.kvstore.GetTeamConnections(channel.TeamId)
	if err != nil {
		writeJSONError(w, "failed to check team connections", http.StatusInternalServerError)
		return
	}

	connName, _, resolveErr := p.resolveConnectionName(r.URL.Query().Get("connection_name"), teamConns)
	if resolveErr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":       resolveErr,
			"connections": teamConns,
		})
		return
	}

	ch, svcErr := p.teardownChannelForCrossGuard(user, channelID, connName)
	if svcErr != nil {
		writeJSONError(w, svcErr.Message, svcErr.Status)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":          "ok",
		"channel_id":      ch.Id,
		"channel_name":    ch.Name,
		"connection_name": connName,
	})
}

func (p *Plugin) handleTeardownTeam(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	teamID := mux.Vars(r)["team_id"]
	if !model.IsValidId(teamID) {
		writeJSONError(w, "invalid team_id", http.StatusBadRequest)
		return
	}

	if !p.isTeamAdminOrSystemAdmin(user.Id, teamID) {
		writeJSONError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	connName, allConns, resolveErr := p.resolveConnectionName(r.URL.Query().Get("connection_name"), p.getAllConnectionNames())
	if resolveErr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":       resolveErr,
			"connections": allConns,
		})
		return
	}

	team, svcErr := p.teardownTeamForCrossGuard(user, teamID, connName)
	if svcErr != nil {
		writeJSONError(w, svcErr.Message, svcErr.Status)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":          "ok",
		"team_id":         team.Id,
		"team_name":       team.Name,
		"connection_name": connName,
	})
}

func (p *Plugin) handleGlobalStatus(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	if !user.IsSystemAdmin() {
		writeJSONError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	resp, svcErr := p.getGlobalStatus()
	if svcErr != nil {
		writeJSONError(w, svcErr.Message, svcErr.Status)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (p *Plugin) handleDialogSelectConnection(w http.ResponseWriter, r *http.Request) {
	var req model.SubmitDialogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Cancelled {
		w.WriteHeader(http.StatusOK)
		return
	}

	userID := req.UserId
	if userID == "" {
		writeJSONError(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		writeJSONError(w, "failed to get user", http.StatusInternalServerError)
		return
	}

	connName, _ := req.Submission["connection_name"].(string)
	if connName == "" {
		writeJSON(w, http.StatusOK, model.SubmitDialogResponse{
			Errors: map[string]string{"connection_name": "Please select a connection."},
		})
		return
	}

	parts := strings.SplitN(req.State, ":", 2)
	if len(parts) != 2 {
		writeJSONError(w, "invalid dialog state", http.StatusBadRequest)
		return
	}
	action, targetID := parts[0], parts[1]

	var responseText string

	switch action {
	case actionInitTeam, actionTeardownTeam:
		if !p.isTeamAdminOrSystemAdmin(userID, targetID) {
			writeJSONError(w, "insufficient permissions", http.StatusForbidden)
			return
		}
		if action == actionInitTeam {
			team, alreadyLinked, svcErr := p.initTeamForCrossGuard(user, targetID, connName)
			switch {
			case svcErr != nil:
				responseText = svcErr.Message
			case alreadyLinked:
				responseText = fmt.Sprintf("Connection `%s` is already linked to this team. (team ID: %s, team name: %s)", connName, team.Id, team.Name)
			default:
				responseText = fmt.Sprintf("Connection `%s` linked to this team successfully.", connName)
			}
		} else {
			_, svcErr := p.teardownTeamForCrossGuard(user, targetID, connName)
			if svcErr != nil {
				responseText = svcErr.Message
			} else {
				responseText = fmt.Sprintf("Connection `%s` unlinked from this team successfully.", connName)
			}
		}

	case actionInitChannel, actionTeardownChannel:
		channel, chErr := p.API.GetChannel(targetID)
		if chErr != nil {
			writeJSONError(w, "channel not found", http.StatusBadRequest)
			return
		}
		if !p.isChannelAdminOrHigher(userID, targetID, channel.TeamId) {
			writeJSONError(w, "insufficient permissions", http.StatusForbidden)
			return
		}
		if action == actionInitChannel {
			ch, alreadyLinked, svcErr := p.initChannelForCrossGuard(user, targetID, connName)
			switch {
			case svcErr != nil:
				responseText = svcErr.Message
			case alreadyLinked:
				responseText = fmt.Sprintf("Connection `%s` is already linked to this channel. (channel ID: %s, channel name: %s)", connName, ch.Id, ch.Name)
			default:
				responseText = fmt.Sprintf("Connection `%s` linked to this channel successfully.", connName)
			}
		} else {
			_, svcErr := p.teardownChannelForCrossGuard(user, targetID, connName)
			if svcErr != nil {
				responseText = svcErr.Message
			} else {
				responseText = fmt.Sprintf("Connection `%s` unlinked from this channel successfully.", connName)
			}
		}

	default:
		writeJSONError(w, "unknown action", http.StatusBadRequest)
		return
	}

	p.API.SendEphemeralPost(userID, &model.Post{
		UserId:    p.botUserID,
		ChannelId: req.ChannelId,
		Message:   responseText,
	})

	w.WriteHeader(http.StatusOK)
}

func (p *Plugin) handleTeamStatus(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	teamID := mux.Vars(r)["team_id"]
	if !model.IsValidId(teamID) {
		writeJSONError(w, "invalid team_id", http.StatusBadRequest)
		return
	}

	member, appErr := p.API.GetTeamMember(teamID, user.Id)
	if appErr != nil || member == nil || member.DeleteAt > 0 {
		writeJSONError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	resp, svcErr := p.getTeamStatus(teamID)
	if svcErr != nil {
		writeJSONError(w, svcErr.Message, svcErr.Status)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (p *Plugin) handleChannelStatus(w http.ResponseWriter, r *http.Request) {
	user := p.getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	channelID := mux.Vars(r)["channel_id"]
	if !model.IsValidId(channelID) {
		writeJSONError(w, "invalid channel_id", http.StatusBadRequest)
		return
	}

	member, appErr := p.API.GetChannelMember(channelID, user.Id)
	if appErr != nil || member == nil {
		writeJSONError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	resp, svcErr := p.getChannelStatus(channelID)
	if svcErr != nil {
		writeJSONError(w, svcErr.Message, svcErr.Status)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}
