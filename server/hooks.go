package main

import (
	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
)

func (p *Plugin) isChannelRelayEnabled(channelID string) (*mmModel.Channel, *mmModel.Team, bool) {
	initialized, err := p.kvstore.GetChannelInitialized(channelID)
	if err != nil {
		p.API.LogError("Failed to check channel init status", "channel_id", channelID, "error", err.Error())
		return nil, nil, false
	}
	if !initialized {
		return nil, nil, false
	}

	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		p.API.LogError("Failed to get channel for relay", "channel_id", channelID, "error", appErr.Error())
		return nil, nil, false
	}

	teamInit, err := p.kvstore.GetTeamInitialized(channel.TeamId)
	if err != nil {
		p.API.LogError("Failed to check team init status", "team_id", channel.TeamId, "error", err.Error())
		return nil, nil, false
	}
	if !teamInit {
		return nil, nil, false
	}

	team, appErr := p.API.GetTeam(channel.TeamId)
	if appErr != nil {
		p.API.LogError("Failed to get team for relay", "team_id", channel.TeamId, "error", appErr.Error())
		return nil, nil, false
	}

	return channel, team, true
}

func (p *Plugin) relayToOutbound(data []byte, logContext string) {
	select {
	case p.relaySem <- struct{}{}:
	default:
		p.API.LogWarn("Relay semaphore full, dropping event", "context", logContext)
		return
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer func() { <-p.relaySem }()
		p.publishToOutbound(p.ctx, data)
	}()
}

func (p *Plugin) MessageHasBeenPosted(_ *plugin.Context, post *mmModel.Post) {
	if post.IsSystemMessage() || post.UserId == p.botUserID || post.GetProp("crossguard_relayed") != nil {
		return
	}

	channel, team, ok := p.isChannelRelayEnabled(post.ChannelId)
	if !ok {
		return
	}

	user, appErr := p.API.GetUser(post.UserId)
	if appErr != nil {
		p.API.LogError("Failed to get user for relay", "user_id", post.UserId, "error", appErr.Error())
		return
	}

	data, err := buildPostEnvelope(model.MessageTypePost, post, channel, team.Name, user.Username)
	if err != nil {
		p.API.LogError("Failed to build post envelope", "post_id", post.Id, "error", err.Error())
		return
	}

	p.relayToOutbound(data, "post:"+post.Id)
}

func (p *Plugin) MessageHasBeenUpdated(_ *plugin.Context, newPost *mmModel.Post, _ *mmModel.Post) {
	if newPost.IsSystemMessage() || newPost.UserId == p.botUserID || newPost.GetProp("crossguard_relayed") != nil {
		return
	}

	channel, team, ok := p.isChannelRelayEnabled(newPost.ChannelId)
	if !ok {
		return
	}

	user, appErr := p.API.GetUser(newPost.UserId)
	if appErr != nil {
		p.API.LogError("Failed to get user for relay", "user_id", newPost.UserId, "error", appErr.Error())
		return
	}

	data, err := buildPostEnvelope(model.MessageTypeUpdate, newPost, channel, team.Name, user.Username)
	if err != nil {
		p.API.LogError("Failed to build update envelope", "post_id", newPost.Id, "error", err.Error())
		return
	}

	p.relayToOutbound(data, "update:"+newPost.Id)
}

func (p *Plugin) MessageHasBeenDeleted(_ *plugin.Context, post *mmModel.Post) {
	if post.UserId == p.botUserID {
		return
	}

	channel, team, ok := p.isChannelRelayEnabled(post.ChannelId)
	if !ok {
		return
	}

	data, err := buildDeleteEnvelope(post, channel, team.Name)
	if err != nil {
		p.API.LogError("Failed to build delete envelope", "post_id", post.Id, "error", err.Error())
		return
	}

	p.relayToOutbound(data, "delete:"+post.Id)
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

	channel, team, ok := p.isChannelRelayEnabled(post.ChannelId)
	if !ok {
		return
	}

	user, appErr := p.API.GetUser(reaction.UserId)
	if appErr != nil {
		p.API.LogError("Failed to get user for reaction relay", "user_id", reaction.UserId, "error", appErr.Error())
		return
	}

	data, err := buildReactionEnvelope(model.MessageTypeReactionAdd, reaction, channel, team.Name, user.Username)
	if err != nil {
		p.API.LogError("Failed to build reaction add envelope", "post_id", reaction.PostId, "error", err.Error())
		return
	}

	p.relayToOutbound(data, "reaction_add:"+reaction.PostId)
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

	channel, team, ok := p.isChannelRelayEnabled(post.ChannelId)
	if !ok {
		return
	}

	user, appErr := p.API.GetUser(reaction.UserId)
	if appErr != nil {
		p.API.LogError("Failed to get user for reaction relay", "user_id", reaction.UserId, "error", appErr.Error())
		return
	}

	data, err := buildReactionEnvelope(model.MessageTypeReactionRemove, reaction, channel, team.Name, user.Username)
	if err != nil {
		p.API.LogError("Failed to build reaction remove envelope", "post_id", reaction.PostId, "error", err.Error())
		return
	}

	p.relayToOutbound(data, "reaction_remove:"+reaction.PostId)
}
