package main

import (
	"fmt"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/nats-io/nats.go"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
)

type inboundConn struct {
	nc   *nats.Conn
	sub  *nats.Subscription
	name string
}

func (p *Plugin) connectInbound() {
	cfg := p.getConfiguration()
	conns, err := cfg.GetInboundConnections()
	if err != nil {
		p.API.LogError("Failed to parse inbound connections", "error", err.Error())
		return
	}

	var pool []inboundConn
	for _, conn := range conns {
		nc, err := connectNATSPersistent(conn, p, "Inbound")
		if err != nil {
			p.API.LogError("Failed to connect inbound NATS",
				"name", conn.Name, "address", conn.Address, "error", err.Error())
			continue
		}

		sub, err := nc.Subscribe(conn.Subject, p.handleInboundMessage(conn.Name))
		if err != nil {
			p.API.LogError("Failed to subscribe inbound NATS",
				"name", conn.Name, "subject", conn.Subject, "error", err.Error())
			nc.Close()
			continue
		}

		pool = append(pool, inboundConn{nc: nc, sub: sub, name: conn.Name})
		p.API.LogInfo("Inbound NATS subscription established", "name", conn.Name, "subject", conn.Subject)
	}

	p.inboundMu.Lock()
	p.inboundConns = pool
	p.inboundMu.Unlock()
}

func (p *Plugin) closeInbound() {
	p.inboundMu.Lock()
	conns := p.inboundConns
	p.inboundConns = nil
	p.inboundMu.Unlock()

	for _, ic := range conns {
		_ = ic.sub.Unsubscribe()
		_ = ic.nc.Drain()
		ic.nc.Close()
	}
}

func (p *Plugin) reconnectInbound() {
	p.closeInbound()
	p.connectInbound()
}

func (p *Plugin) handleInboundMessage(connName string) nats.MsgHandler {
	return func(msg *nats.Msg) {
		select {
		case p.relaySem <- struct{}{}:
		default:
			p.API.LogWarn("Relay semaphore full, dropping inbound message", "conn", connName)
			return
		}

		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			defer func() { <-p.relaySem }()

			select {
			case <-p.ctx.Done():
				return
			default:
			}

			envelope, err := model.UnmarshalMessage(msg.Data)
			if err != nil {
				p.API.LogError("Failed to unmarshal inbound message", "conn", connName, "error", err.Error())
				return
			}

			switch envelope.Type {
			case model.MessageTypePost:
				p.handleInboundPost(connName, envelope)
			case model.MessageTypeUpdate:
				p.handleInboundUpdate(connName, envelope)
			case model.MessageTypeDelete:
				p.handleInboundDelete(connName, envelope)
			case model.MessageTypeReactionAdd:
				p.handleInboundReaction(connName, envelope, true)
			case model.MessageTypeReactionRemove:
				p.handleInboundReaction(connName, envelope, false)
			case model.MessageTypeTest:
				var testMsg model.TestMessage
				if err := envelope.Decode(&testMsg); err == nil {
					p.API.LogInfo("Received inbound test message", "conn", connName, "id", testMsg.ID)
				} else {
					p.API.LogInfo("Received inbound test message", "conn", connName)
				}
			default:
				p.API.LogWarn("Unknown inbound message type", "conn", connName, "type", envelope.Type)
			}
		}()
	}
}

func (p *Plugin) resolveTeamAndChannel(connName, teamName, channelName string) (*mmModel.Team, *mmModel.Channel, error) {
	// Check for an explicit rewrite rule first. If one exists, it takes
	// precedence over a local team that happens to share the remote name.
	team, rewriteErr := p.findTeamByRewrite(connName, teamName)
	if rewriteErr != nil {
		return nil, nil, fmt.Errorf("failed to check rewrite index: %w", rewriteErr)
	}
	if team == nil {
		var appErr *mmModel.AppError
		team, appErr = p.API.GetTeamByName(teamName)
		if appErr != nil {
			return nil, nil, fmt.Errorf("team %q not found: %w", teamName, appErr)
		}
	}

	conns, err := p.kvstore.GetTeamConnections(team.Id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to check team connections: %w", err)
	}
	linked := false
	for _, tc := range conns {
		if tc.Direction == "inbound" && tc.Connection == connName {
			linked = true
			break
		}
	}
	if !linked {
		p.handleUnlinkedInbound(team, connName)
		return nil, nil, fmt.Errorf("inbound connection %q is not linked to team %q", connName, teamName)
	}

	channel, appErr := p.API.GetChannelByName(team.Id, channelName, false)
	if appErr != nil {
		return nil, nil, fmt.Errorf("channel %q not found in team %q: %w", channelName, teamName, appErr)
	}

	chanConns, err := p.kvstore.GetChannelConnections(channel.Id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to check channel connections: %w", err)
	}
	chanLinked := false
	for _, tc := range chanConns {
		if tc.Direction == "inbound" && tc.Connection == connName {
			chanLinked = true
			break
		}
	}
	if !chanLinked {
		p.handleUnlinkedInboundChannel(team, channel, connName)
		return nil, nil, fmt.Errorf("inbound connection %q is not linked to channel %q in team %q", connName, channelName, teamName)
	}

	return team, channel, nil
}

func (p *Plugin) findTeamByRewrite(connName, remoteTeamName string) (*mmModel.Team, error) {
	localTeamID, err := p.kvstore.GetTeamRewriteIndex(connName, remoteTeamName)
	if err != nil {
		return nil, fmt.Errorf("rewrite index lookup for %s/%s: %w", connName, remoteTeamName, err)
	}
	if localTeamID == "" {
		return nil, nil
	}
	team, appErr := p.API.GetTeam(localTeamID)
	if appErr != nil {
		return nil, fmt.Errorf("rewrite target team %s not found: %w", localTeamID, appErr)
	}
	return team, nil
}

func (p *Plugin) handleInboundPost(connName string, envelope *model.Message) {
	var postMsg model.PostMessage
	if err := envelope.Decode(&postMsg); err != nil {
		p.API.LogError("Failed to decode inbound post", "conn", connName, "error", err.Error())
		return
	}

	team, channel, err := p.resolveTeamAndChannel(connName, postMsg.TeamName, postMsg.ChannelName)
	if err != nil {
		p.API.LogWarn("Inbound post: resolve failed", "conn", connName, "error", err.Error())
		return
	}

	userID, err := p.resolveInboundUser(postMsg.Username, connName, team.Id, channel.Id)
	if err != nil {
		p.API.LogError("Inbound post: resolve user failed", "conn", connName, "username", postMsg.Username, "error", err.Error())
		return
	}

	post := &mmModel.Post{
		UserId:    userID,
		ChannelId: channel.Id,
		Message:   postMsg.Message,
	}
	post.AddProp("crossguard_relayed", true)

	if postMsg.RootID != "" {
		localRootID, err := p.kvstore.GetPostMapping(connName, postMsg.RootID)
		if err != nil {
			p.API.LogWarn("Inbound post: failed to look up root mapping", "conn", connName, "remote_root_id", postMsg.RootID, "error", err.Error())
		}
		if localRootID != "" {
			post.RootId = localRootID
		}
	}

	created, appErr := p.API.CreatePost(post)
	if appErr != nil {
		p.API.LogError("Inbound post: create failed", "conn", connName, "error", appErr.Error())
		return
	}

	if err := p.kvstore.SetPostMapping(connName, postMsg.PostID, created.Id); err != nil {
		p.API.LogError("Inbound post: failed to store post mapping", "conn", connName, "remote_id", postMsg.PostID, "local_id", created.Id, "error", err.Error())
	}
}

func (p *Plugin) handleInboundUpdate(connName string, envelope *model.Message) {
	var postMsg model.PostMessage
	if err := envelope.Decode(&postMsg); err != nil {
		p.API.LogError("Failed to decode inbound update", "conn", connName, "error", err.Error())
		return
	}

	localPostID, err := p.kvstore.GetPostMapping(connName, postMsg.PostID)
	if err != nil {
		p.API.LogError("Inbound update: failed to look up post mapping",
			"conn", connName, "remote_id", postMsg.PostID, "error", err.Error())
		return
	}
	if localPostID == "" {
		p.API.LogWarn("Inbound update: no post mapping found", "conn", connName, "remote_id", postMsg.PostID)
		return
	}

	existing, appErr := p.API.GetPost(localPostID)
	if appErr != nil {
		p.API.LogError("Inbound update: failed to get local post", "conn", connName, "local_id", localPostID, "error", appErr.Error())
		return
	}

	existing.Message = postMsg.Message
	if _, appErr := p.API.UpdatePost(existing); appErr != nil {
		p.API.LogError("Inbound update: failed to update post", "conn", connName, "local_id", localPostID, "error", appErr.Error())
	}
}

func (p *Plugin) handleInboundDelete(connName string, envelope *model.Message) {
	var deleteMsg model.DeleteMessage
	if err := envelope.Decode(&deleteMsg); err != nil {
		p.API.LogError("Failed to decode inbound delete", "conn", connName, "error", err.Error())
		return
	}

	localPostID, err := p.kvstore.GetPostMapping(connName, deleteMsg.PostID)
	if err != nil {
		p.API.LogError("Inbound delete: failed to look up post mapping",
			"conn", connName, "remote_id", deleteMsg.PostID, "error", err.Error())
		return
	}
	if localPostID == "" {
		p.API.LogWarn("Inbound delete: no post mapping found", "conn", connName, "remote_id", deleteMsg.PostID)
		return
	}

	if err := p.kvstore.SetDeletingFlag(localPostID); err != nil {
		p.API.LogError("Inbound delete: failed to set delete flag", "conn", connName, "local_id", localPostID, "error", err.Error())
	}

	if appErr := p.API.DeletePost(localPostID); appErr != nil {
		p.API.LogError("Inbound delete: failed to delete post", "conn", connName, "local_id", localPostID, "error", appErr.Error())
	}

	if err := p.kvstore.ClearDeletingFlag(localPostID); err != nil {
		p.API.LogWarn("Inbound delete: failed to remove delete flag", "conn", connName, "local_id", localPostID, "error", err.Error())
	}

	if err := p.kvstore.DeletePostMapping(connName, deleteMsg.PostID); err != nil {
		p.API.LogWarn("Inbound delete: failed to remove post mapping", "conn", connName, "remote_id", deleteMsg.PostID, "error", err.Error())
	}
}

func (p *Plugin) handleInboundReaction(connName string, envelope *model.Message, add bool) {
	var reactionMsg model.ReactionMessage
	if err := envelope.Decode(&reactionMsg); err != nil {
		p.API.LogError("Failed to decode inbound reaction", "conn", connName, "error", err.Error())
		return
	}

	localPostID, err := p.kvstore.GetPostMapping(connName, reactionMsg.PostID)
	if err != nil {
		p.API.LogError("Inbound reaction: failed to look up post mapping",
			"conn", connName, "remote_id", reactionMsg.PostID, "error", err.Error())
		return
	}
	if localPostID == "" {
		p.API.LogWarn("Inbound reaction: no post mapping found", "conn", connName, "remote_id", reactionMsg.PostID)
		return
	}

	team, channel, err := p.resolveTeamAndChannel(connName, reactionMsg.TeamName, reactionMsg.ChannelName)
	if err != nil {
		p.API.LogWarn("Inbound reaction: resolve failed", "conn", connName, "error", err.Error())
		return
	}

	userID, err := p.resolveInboundUser(reactionMsg.Username, connName, team.Id, channel.Id)
	if err != nil {
		p.API.LogError("Inbound reaction: resolve user failed", "conn", connName, "username", reactionMsg.Username, "error", err.Error())
		return
	}

	reaction := &mmModel.Reaction{
		UserId:    userID,
		PostId:    localPostID,
		EmojiName: reactionMsg.EmojiName,
	}

	if add {
		if _, appErr := p.API.AddReaction(reaction); appErr != nil {
			p.API.LogError("Inbound reaction: add failed", "conn", connName, "post_id", localPostID, "error", appErr.Error())
		}
	} else {
		if appErr := p.API.RemoveReaction(reaction); appErr != nil {
			p.API.LogError("Inbound reaction: remove failed", "conn", connName, "post_id", localPostID, "error", appErr.Error())
		}
	}
}
