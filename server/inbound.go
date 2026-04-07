package main

import (
	"context"
	"fmt"
	"time"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
)

type inboundConn struct {
	nc                  *nats.Conn
	sub                 *nats.Subscription
	name                string
	fileTransferEnabled bool
	fileFilterMode      string
	fileFilterTypes     string
}

type pendingWatcher struct {
	connName string
	nc       *nats.Conn
}

func (p *Plugin) connectInbound() {
	p.inboundCtx, p.inboundCancel = context.WithCancel(p.ctx)
	ctx := p.inboundCtx // local copy for goroutines, avoids race on struct field

	cfg := p.getConfiguration()
	conns, err := cfg.GetInboundConnections()
	if err != nil {
		p.API.LogError("Failed to parse inbound connections", "error", err.Error())
		return
	}

	var pool []inboundConn
	var watchers []pendingWatcher
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

		ic := inboundConn{
			nc:                  nc,
			sub:                 sub,
			name:                conn.Name,
			fileTransferEnabled: conn.FileTransferEnabled,
			fileFilterMode:      conn.FileFilterMode,
			fileFilterTypes:     conn.FileFilterTypes,
		}
		pool = append(pool, ic)
		p.API.LogInfo("Inbound NATS subscription established", "name", conn.Name, "subject", conn.Subject)

		if conn.FileTransferEnabled {
			watchers = append(watchers, pendingWatcher{connName: conn.Name, nc: nc})
		}
	}

	// Store pool before starting watchers so getInboundConn can find them.
	p.inboundMu.Lock()
	p.inboundConns = pool
	p.inboundMu.Unlock()

	for _, w := range watchers {
		p.wg.Add(1)
		p.fileWatcherWg.Add(1)
		go func(ctx context.Context, connName string, nc *nats.Conn) {
			defer p.wg.Done()
			defer p.fileWatcherWg.Done()
			p.watchObjectStore(ctx, connName, nc)
		}(ctx, w.connName, w.nc)
	}
}

func (p *Plugin) getInboundConn(connName string) *inboundConn {
	p.inboundMu.RLock()
	defer p.inboundMu.RUnlock()
	for i := range p.inboundConns {
		if p.inboundConns[i].name == connName {
			return &p.inboundConns[i]
		}
	}
	return nil
}

func (p *Plugin) closeInbound() {
	if p.inboundCancel != nil {
		p.inboundCancel()
	}

	// Wait for file watcher goroutines to finish before closing connections.
	// This prevents spurious errors from watchers using closed connections
	// and ensures no old watchers overlap with new ones on reconnect.
	p.fileWatcherWg.Wait()

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

		p.wg.Go(func() {
			defer func() { <-p.relaySem }()

			select {
			case <-p.ctx.Done():
				return
			default:
			}

			format := model.DetectFormat(msg.Data)
			env, err := model.Unmarshal(msg.Data, format)
			if err != nil {
				p.API.LogError("Failed to unmarshal inbound message", "conn", connName, "error", err.Error())
				return
			}

			switch env.Type {
			case model.MessageTypePost:
				if env.PostMessage == nil {
					p.API.LogError("Inbound post: missing payload", "conn", connName)
					return
				}
				p.handleInboundPost(connName, env.PostMessage)
			case model.MessageTypeUpdate:
				if env.PostMessage == nil {
					p.API.LogError("Inbound update: missing payload", "conn", connName)
					return
				}
				p.handleInboundUpdate(connName, env.PostMessage)
			case model.MessageTypeDelete:
				if env.DeleteMessage == nil {
					p.API.LogError("Inbound delete: missing payload", "conn", connName)
					return
				}
				p.handleInboundDelete(connName, env.DeleteMessage)
			case model.MessageTypeReactionAdd:
				if env.ReactionMessage == nil {
					p.API.LogError("Inbound reaction add: missing payload", "conn", connName)
					return
				}
				p.handleInboundReaction(connName, env.ReactionMessage, true)
			case model.MessageTypeReactionRemove:
				if env.ReactionMessage == nil {
					p.API.LogError("Inbound reaction remove: missing payload", "conn", connName)
					return
				}
				p.handleInboundReaction(connName, env.ReactionMessage, false)
			case model.MessageTypeTest:
				if env.TestMessage != nil {
					p.API.LogInfo("Received inbound test message", "conn", connName, "id", env.TestMessage.ID)
				} else {
					p.API.LogInfo("Received inbound test message", "conn", connName)
				}
			default:
				p.API.LogWarn("Unknown inbound message type", "conn", connName, "type", env.Type)
			}
		})
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

func (p *Plugin) handleInboundPost(connName string, postMsg *model.PostMessage) {
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
		Message:   postMsg.MessageText,
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

func (p *Plugin) handleInboundUpdate(connName string, postMsg *model.PostMessage) {
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

	existing.Message = postMsg.MessageText
	if _, appErr := p.API.UpdatePost(existing); appErr != nil {
		p.API.LogError("Inbound update: failed to update post", "conn", connName, "local_id", localPostID, "error", appErr.Error())
	}
}

func (p *Plugin) handleInboundDelete(connName string, deleteMsg *model.DeleteMessage) {
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

func (p *Plugin) handleInboundReaction(connName string, reactionMsg *model.ReactionMessage, add bool) {
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

func (p *Plugin) watchObjectStore(ctx context.Context, connName string, nc *nats.Conn) {
	objectStore, err := getOrCreateObjectStore(ctx, nc, objectStoreBucket)
	if err != nil {
		p.API.LogError("Failed to open object store for file watcher",
			"conn", connName, "error", err.Error())
		return
	}

	watcher, err := objectStore.Watch(ctx, jetstream.UpdatesOnly())
	if err != nil {
		p.API.LogError("Failed to start object store watcher",
			"conn", connName, "error", err.Error())
		return
	}
	defer func() { _ = watcher.Stop() }()

	p.API.LogInfo("Object store file watcher started", "conn", connName)

	for {
		select {
		case <-ctx.Done():
			return
		case info, ok := <-watcher.Updates():
			if !ok {
				return
			}
			if info == nil {
				continue
			}
			if info.Deleted {
				continue
			}
			p.handleInboundFile(ctx, connName, objectStore, info)
		}
	}
}

const (
	postMappingMaxRetries = 3
	postMappingRetryDelay = time.Second
)

func (p *Plugin) handleInboundFile(ctx context.Context, connName string, store jetstream.ObjectStore, info *jetstream.ObjectInfo) {
	headerConn := info.Headers.Get(headerConnName)
	remotePostID := info.Headers.Get(headerPostID)
	filename := info.Headers.Get(headerFilename)

	if headerConn == "" || remotePostID == "" || filename == "" {
		p.API.LogWarn("Inbound file: missing required headers, skipping",
			"key", info.Name, headerConnName, headerConn, headerPostID, remotePostID, headerFilename, filename)
		return
	}

	if headerConn != connName {
		return
	}

	ic := p.getInboundConn(connName)
	if ic == nil {
		p.API.LogWarn("Inbound file: connection no longer active, skipping",
			"conn", connName, "filename", filename)
		return
	}

	if !isFileAllowed(filename, ic.fileFilterMode, ic.fileFilterTypes) {
		p.API.LogInfo("Inbound file filtered by policy",
			"filename", filename, "conn", connName)
		return
	}

	var localPostID string
	var lookupErr error
	for attempt := range postMappingMaxRetries {
		localPostID, lookupErr = p.kvstore.GetPostMapping(connName, remotePostID)
		if lookupErr == nil && localPostID != "" {
			break
		}
		if attempt < postMappingMaxRetries-1 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(attempt+1) * postMappingRetryDelay):
			}
		}
	}
	if localPostID == "" {
		if lookupErr != nil {
			p.API.LogError("Inbound file: post mapping lookup failed after retries",
				"conn", connName, "remote_post_id", remotePostID, "error", lookupErr.Error())
		} else {
			p.API.LogWarn("Inbound file: no post mapping found after retries",
				"conn", connName, "remote_post_id", remotePostID)
		}
		return
	}

	// Block until a semaphore slot is available (or context is cancelled).
	// The watcher loop is a dedicated goroutine, so blocking is safe here.
	select {
	case p.fileSem <- struct{}{}:
		defer func() { <-p.fileSem }()
	case <-ctx.Done():
		return
	}

	data, err := store.GetBytes(ctx, info.Name)
	if err != nil {
		p.API.LogError("Failed to download file from object store",
			"key", info.Name, "conn", connName, "error", err.Error())
		return
	}

	existing, appErr := p.API.GetPost(localPostID)
	if appErr != nil {
		p.API.LogError("Inbound file: failed to get local post",
			"local_post_id", localPostID, "error", appErr.Error())
		return
	}

	fileInfo, appErr := p.API.UploadFile(data, existing.ChannelId, filename)
	if appErr != nil {
		p.API.LogError("Failed to upload file to Mattermost",
			"filename", filename, "error", appErr.Error())
		return
	}

	existing.FileIds = append(existing.FileIds, fileInfo.Id)
	if _, appErr := p.API.UpdatePost(existing); appErr != nil {
		p.API.LogError("Failed to attach file to post",
			"local_post_id", localPostID, "file_id", fileInfo.Id, "error", appErr.Error())
	}
}
