package main

import (
	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

// isChannelRelayEnabled checks if a channel's relay is active and returns
// the channel, team, and the team's linked connection names.
func (p *Plugin) isChannelRelayEnabled(channelID string) (*mmModel.Channel, *mmModel.Team, []store.TeamConnection) {
	channelConns, err := p.kvstore.GetChannelConnections(channelID)
	if err != nil {
		p.API.LogError("Failed to check channel connections", "channel_id", channelID, "error", err.Error())
		return nil, nil, nil
	}
	if len(channelConns) == 0 {
		return nil, nil, nil
	}

	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		p.API.LogError("Failed to get channel for relay", "channel_id", channelID, "error", appErr.Error())
		return nil, nil, nil
	}

	teamConns, err := p.kvstore.GetTeamConnections(channel.TeamId)
	if err != nil {
		p.API.LogError("Failed to check team connections", "team_id", channel.TeamId, "error", err.Error())
		return nil, nil, nil
	}
	if len(teamConns) == 0 {
		return nil, nil, nil
	}

	team, appErr := p.API.GetTeam(channel.TeamId)
	if appErr != nil {
		p.API.LogError("Failed to get team for relay", "team_id", channel.TeamId, "error", appErr.Error())
		return nil, nil, nil
	}

	return channel, team, channelConns
}

func (p *Plugin) relayToOutbound(env *model.Envelope, connNames []store.TeamConnection, logContext string) {
	select {
	case p.relaySem <- struct{}{}:
	default:
		p.API.LogWarn("Relay semaphore full, dropping event", "context", logContext)
		return
	}

	p.wg.Go(func() {
		defer func() { <-p.relaySem }()
		p.publishToOutbound(p.ctx, env, connNames)
	})
}

func (p *Plugin) MessageHasBeenPosted(_ *plugin.Context, post *mmModel.Post) {
	if post.IsSystemMessage() || post.UserId == p.botUserID || post.GetProp("crossguard_relayed") != nil {
		return
	}

	channel, team, connNames := p.isChannelRelayEnabled(post.ChannelId)
	if connNames == nil {
		return
	}

	user, appErr := p.API.GetUser(post.UserId)
	if appErr != nil {
		p.API.LogError("Failed to get user for relay", "user_id", post.UserId, "error", appErr.Error())
		return
	}

	env := buildPostEnvelope(model.MessageTypePost, post, channel, team.Name, user.Username)
	p.relayToOutbound(env, connNames, "post:"+post.Id)

	if len(post.FileIds) > 0 {
		p.uploadPostFiles(post, connNames)
	}
}

func (p *Plugin) MessageHasBeenUpdated(_ *plugin.Context, newPost *mmModel.Post, _ *mmModel.Post) {
	if newPost.IsSystemMessage() || newPost.UserId == p.botUserID || newPost.GetProp("crossguard_relayed") != nil {
		return
	}

	channel, team, connNames := p.isChannelRelayEnabled(newPost.ChannelId)
	if connNames == nil {
		return
	}

	user, appErr := p.API.GetUser(newPost.UserId)
	if appErr != nil {
		p.API.LogError("Failed to get user for relay", "user_id", newPost.UserId, "error", appErr.Error())
		return
	}

	env := buildPostEnvelope(model.MessageTypeUpdate, newPost, channel, team.Name, user.Username)
	p.relayToOutbound(env, connNames, "update:"+newPost.Id)
}

func (p *Plugin) MessageHasBeenDeleted(_ *plugin.Context, post *mmModel.Post) {
	if post.UserId == p.botUserID {
		return
	}

	isDeletingFlag, err := p.kvstore.IsDeletingFlagSet(post.Id)
	if err != nil {
		p.API.LogError("Failed to check delete flag, skipping relay to avoid loop",
			"post_id", post.Id, "error", err.Error())
		return
	}
	if isDeletingFlag {
		return
	}

	channel, team, connNames := p.isChannelRelayEnabled(post.ChannelId)
	if connNames == nil {
		return
	}

	env := buildDeleteEnvelope(post, channel, team.Name)
	p.relayToOutbound(env, connNames, "delete:"+post.Id)
}

func (p *Plugin) ReactionHasBeenAdded(_ *plugin.Context, reaction *mmModel.Reaction) {
	post, appErr := p.API.GetPost(reaction.PostId)
	if appErr != nil {
		p.API.LogError("Failed to get post for reaction relay", "post_id", reaction.PostId, "error", appErr.Error())
		return
	}

	if post.IsSystemMessage() || post.UserId == p.botUserID {
		return
	}

	user, appErr := p.API.GetUser(reaction.UserId)
	if appErr != nil {
		p.API.LogError("Failed to get user for reaction relay", "user_id", reaction.UserId, "error", appErr.Error())
		return
	}

	if user.Position == "crossguard-sync" {
		return
	}

	channel, team, connNames := p.isChannelRelayEnabled(post.ChannelId)
	if connNames == nil {
		return
	}

	env := buildReactionEnvelope(model.MessageTypeReactionAdd, reaction, channel, team.Name, user.Username)
	p.relayToOutbound(env, connNames, "reaction_add:"+reaction.PostId)
}

func (p *Plugin) ReactionHasBeenRemoved(_ *plugin.Context, reaction *mmModel.Reaction) {
	post, appErr := p.API.GetPost(reaction.PostId)
	if appErr != nil {
		p.API.LogError("Failed to get post for reaction relay", "post_id", reaction.PostId, "error", appErr.Error())
		return
	}

	if post.IsSystemMessage() || post.UserId == p.botUserID {
		return
	}

	user, appErr := p.API.GetUser(reaction.UserId)
	if appErr != nil {
		p.API.LogError("Failed to get user for reaction relay", "user_id", reaction.UserId, "error", appErr.Error())
		return
	}

	if user.Position == "crossguard-sync" {
		return
	}

	channel, team, connNames := p.isChannelRelayEnabled(post.ChannelId)
	if connNames == nil {
		return
	}

	env := buildReactionEnvelope(model.MessageTypeReactionRemove, reaction, channel, team.Name, user.Username)
	p.relayToOutbound(env, connNames, "reaction_remove:"+reaction.PostId)
}
