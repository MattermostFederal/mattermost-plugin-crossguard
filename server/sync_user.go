package main

import (
	"fmt"
	"strings"

	mmModel "github.com/mattermost/mattermost/server/public/model"
)

const (
	syncUserPosition  = "crossguard-sync"
	maxUsernameLength = 64
)

// ensureSyncUser finds or creates a synthetic user representing a remote sender,
// ensures team and channel membership, and returns the user ID.
func (p *Plugin) ensureSyncUser(username, connName, teamID, channelID string) (string, error) {
	munged := username + "." + connName
	if len(munged) > maxUsernameLength {
		maxUser := maxUsernameLength - len(connName) - 1
		if maxUser < 1 {
			return "", fmt.Errorf("connection name too long to create sync username")
		}
		munged = username[:maxUser] + "." + connName
		p.API.LogWarn("Truncated sync username to fit limit", "original", username, "munged", munged)
	}

	user, appErr := p.API.GetUserByUsername(munged)
	if appErr == nil {
		if user.Position != syncUserPosition {
			return "", fmt.Errorf("username %q exists but is not a sync user", munged)
		}
		p.ensureMembership(user.Id, teamID, channelID)
		return user.Id, nil
	}

	remoteID := "crossguard-" + connName
	newUser := &mmModel.User{
		Username:  munged,
		Email:     mmModel.NewId() + "@crossguard.local",
		Password:  mmModel.NewId() + "!Aa1",
		Roles:     "system_user",
		Nickname:  username,
		FirstName: username,
		LastName:  "(via " + connName + ")",
		Position:  syncUserPosition,
		RemoteId:  &remoteID,
	}

	created, createErr := p.API.CreateUser(newUser)
	if createErr != nil {
		if strings.Contains(createErr.Error(), "already") {
			user, appErr = p.API.GetUserByUsername(munged)
			if appErr != nil {
				return "", fmt.Errorf("failed to get user after create race: %w", appErr)
			}
			if user.Position != syncUserPosition {
				return "", fmt.Errorf("username %q exists but is not a sync user", munged)
			}
			p.ensureMembership(user.Id, teamID, channelID)
			return user.Id, nil
		}
		return "", fmt.Errorf("failed to create sync user %q: %w", munged, createErr)
	}

	p.ensureMembership(created.Id, teamID, channelID)
	return created.Id, nil
}

func (p *Plugin) ensureMembership(userID, teamID, channelID string) {
	if _, appErr := p.API.CreateTeamMember(teamID, userID); appErr != nil {
		if !strings.Contains(appErr.Error(), "already") {
			p.API.LogWarn("Failed to add sync user to team", "user_id", userID, "team_id", teamID, "error", appErr.Error())
		}
	}
	if _, appErr := p.API.AddChannelMember(channelID, userID); appErr != nil {
		if !strings.Contains(appErr.Error(), "already") {
			p.API.LogWarn("Failed to add sync user to channel", "user_id", userID, "channel_id", channelID, "error", appErr.Error())
		}
	}
}
