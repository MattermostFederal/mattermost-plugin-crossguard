package main

import (
	"context"
	"fmt"
	"time"

	mmModel "github.com/mattermost/mattermost/server/public/model"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
)

type inboundConn struct {
	provider            QueueProvider
	name                string
	fileTransferEnabled bool
	fileFilterMode      string
	fileFilterTypes     string
}

type pendingWatcher struct {
	connName string
	provider QueueProvider
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
		provider, err := p.createProvider(conn, "Inbound")
		if err != nil {
			p.API.LogError("Failed to connect inbound",
				"name", conn.Name, "provider", conn.Provider, "error", err.Error())
			continue
		}

		handler := p.handleInboundMessage(conn.Name)
		if err := provider.Subscribe(ctx, handler); err != nil {
			p.API.LogError("Failed to subscribe inbound",
				"name", conn.Name, "error", err.Error())
			_ = provider.Close()
			continue
		}

		ic := inboundConn{
			provider:            provider,
			name:                conn.Name,
			fileTransferEnabled: conn.FileTransferEnabled,
			fileFilterMode:      conn.FileFilterMode,
			fileFilterTypes:     conn.FileFilterTypes,
		}
		pool = append(pool, ic)
		p.API.LogInfo("Inbound subscription established", "name", conn.Name, "provider", conn.Provider)

		if conn.FileTransferEnabled {
			watchers = append(watchers, pendingWatcher{connName: conn.Name, provider: provider})
		}
	}

	// Store pool before starting watchers so getInboundConn can find them.
	p.inboundMu.Lock()
	p.inboundConns = pool
	p.inboundMu.Unlock()

	for _, w := range watchers {
		p.wg.Add(1)
		p.fileWatcherWg.Add(1)
		go func(ctx context.Context, connName string, provider QueueProvider) {
			defer p.wg.Done()
			defer p.fileWatcherWg.Done()
			p.watchFiles(ctx, connName, provider)
		}(ctx, w.connName, w.provider)
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
		_ = ic.provider.Close()
	}
}

func (p *Plugin) reconnectInbound() {
	p.closeInbound()
	p.connectInbound()
}

func (p *Plugin) handleInboundMessage(connName string) func(data []byte) error {
	return func(data []byte) error {
		select {
		case p.relaySem <- struct{}{}:
		default:
			p.API.LogWarn("Relay semaphore full, dropping inbound message", "conn", connName)
			return nil
		}

		p.wg.Go(func() {
			defer func() { <-p.relaySem }()

			select {
			case <-p.ctx.Done():
				return
			default:
			}

			format := model.DetectFormat(data)
			env, err := model.Unmarshal(data, format)
			if err != nil {
				p.API.LogError("Failed to unmarshal inbound message", "conn", connName, "error", err.Error())
				return
			}

			var (
				missing  bool
				remoteID string
			)
			switch env.Type {
			case model.MessageTypePost:
				if env.PostMessage == nil {
					p.API.LogError("Inbound post: missing payload", "conn", connName)
					return
				}
				missing = p.handleInboundPost(connName, env.PostMessage, false)
				remoteID = env.PostMessage.PostID
			case model.MessageTypeUpdate:
				if env.PostMessage == nil {
					p.API.LogError("Inbound update: missing payload", "conn", connName)
					return
				}
				missing = p.handleInboundUpdate(connName, env.PostMessage)
				remoteID = env.PostMessage.PostID
			case model.MessageTypeDelete:
				if env.DeleteMessage == nil {
					p.API.LogError("Inbound delete: missing payload", "conn", connName)
					return
				}
				missing = p.handleInboundDelete(connName, env.DeleteMessage)
				remoteID = env.DeleteMessage.PostID
			case model.MessageTypeReactionAdd:
				if env.ReactionMessage == nil {
					p.API.LogError("Inbound reaction add: missing payload", "conn", connName)
					return
				}
				missing = p.handleInboundReaction(connName, env.ReactionMessage, true)
				remoteID = env.ReactionMessage.PostID
			case model.MessageTypeReactionRemove:
				if env.ReactionMessage == nil {
					p.API.LogError("Inbound reaction remove: missing payload", "conn", connName)
					return
				}
				missing = p.handleInboundReaction(connName, env.ReactionMessage, false)
				remoteID = env.ReactionMessage.PostID
			case model.MessageTypeTest:
				if env.TestMessage != nil {
					p.API.LogInfo("Received inbound test message", "conn", connName, "id", env.TestMessage.ID)
				} else {
					p.API.LogInfo("Received inbound test message", "conn", connName)
				}
			default:
				p.API.LogWarn("Unknown inbound message type", "conn", connName, "type", env.Type)
				return
			}

			if missing && p.retryQueue != nil {
				if !p.retryQueue.Enqueue(connName, data, remoteID, env.Type) {
					p.API.LogError("Missing message: queue full, dropping message",
						"conn", connName, "type", env.Type, "remote_post_id", remoteID, "queue_size", retryQueueMaxSize)
					return
				}
				p.API.LogWarn("Missing message: queuing for retry",
					"conn", connName, "type", env.Type, "remote_post_id", remoteID, "queue_size", p.retryQueue.Len())
			}
		})

		return nil
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

// handleInboundPost creates a local post from a remote PostMessage. If the
// message is a thread reply whose root has no mapping yet, it returns
// missing=true when lastAttempt=false, so the caller can enqueue for retry.
// On lastAttempt=true, it creates the post as standalone (no RootId) instead.
func (p *Plugin) handleInboundPost(connName string, postMsg *model.PostMessage, lastAttempt bool) (missing bool) {
	team, channel, err := p.resolveTeamAndChannel(connName, postMsg.TeamName, postMsg.ChannelName)
	if err != nil {
		p.API.LogWarn("Inbound post: resolve failed", "conn", connName, "error", err.Error())
		return false
	}

	// Idempotency check for at-least-once delivery (Azure Queue).
	existingLocalID, err := p.kvstore.GetPostMapping(connName, postMsg.PostID)
	if err != nil {
		p.API.LogWarn("Inbound post: idempotency lookup failed, processing anyway",
			"conn", connName, "postID", postMsg.PostID, "error", err.Error())
	}
	if existingLocalID != "" {
		return false // already processed, skip
	}

	userID, err := p.resolveInboundUser(postMsg.Username, connName, team.Id, channel.Id)
	if err != nil {
		p.API.LogError("Inbound post: resolve user failed", "conn", connName, "username", postMsg.Username, "error", err.Error())
		return false
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
		switch {
		case localRootID != "":
			post.RootId = localRootID
		case !lastAttempt:
			// Root not found yet, queue for retry
			return true
		default:
			// lastAttempt and still no root - create as standalone
			p.API.LogWarn("Inbound post: root not found after retries, creating standalone",
				"conn", connName, "remote_root_id", postMsg.RootID)
		}
	}

	created, appErr := p.API.CreatePost(post)
	if appErr != nil {
		p.API.LogError("Inbound post: create failed", "conn", connName, "error", appErr.Error())
		return false
	}

	if err := p.kvstore.SetPostMapping(connName, postMsg.PostID, created.Id); err != nil {
		p.API.LogError("Inbound post: failed to store post mapping", "conn", connName, "remote_id", postMsg.PostID, "local_id", created.Id, "error", err.Error())
	}
	return false
}

// handleInboundUpdate returns missing=true if the post mapping is not yet
// available (kv lookup error or empty), signaling the caller to enqueue the
// message for retry.
func (p *Plugin) handleInboundUpdate(connName string, postMsg *model.PostMessage) (missing bool) {
	localPostID, err := p.kvstore.GetPostMapping(connName, postMsg.PostID)
	if err != nil {
		p.API.LogError("Inbound update: failed to look up post mapping",
			"conn", connName, "remote_id", postMsg.PostID, "error", err.Error())
		return true
	}
	if localPostID == "" {
		return true
	}

	existing, appErr := p.API.GetPost(localPostID)
	if appErr != nil {
		p.API.LogError("Inbound update: failed to get local post", "conn", connName, "local_id", localPostID, "error", appErr.Error())
		return false
	}

	existing.Message = postMsg.MessageText
	if _, appErr := p.API.UpdatePost(existing); appErr != nil {
		p.API.LogError("Inbound update: failed to update post", "conn", connName, "local_id", localPostID, "error", appErr.Error())
	}
	return false
}

func (p *Plugin) handleInboundDelete(connName string, deleteMsg *model.DeleteMessage) (missing bool) {
	localPostID, err := p.kvstore.GetPostMapping(connName, deleteMsg.PostID)
	if err != nil {
		p.API.LogError("Inbound delete: failed to look up post mapping",
			"conn", connName, "remote_id", deleteMsg.PostID, "error", err.Error())
		return true
	}
	if localPostID == "" {
		return true
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
	return false
}

func (p *Plugin) handleInboundReaction(connName string, reactionMsg *model.ReactionMessage, add bool) (missing bool) {
	localPostID, err := p.kvstore.GetPostMapping(connName, reactionMsg.PostID)
	if err != nil {
		p.API.LogError("Inbound reaction: failed to look up post mapping",
			"conn", connName, "remote_id", reactionMsg.PostID, "error", err.Error())
		return true
	}
	if localPostID == "" {
		return true
	}

	team, channel, err := p.resolveTeamAndChannel(connName, reactionMsg.TeamName, reactionMsg.ChannelName)
	if err != nil {
		p.API.LogWarn("Inbound reaction: resolve failed", "conn", connName, "error", err.Error())
		return false
	}

	userID, err := p.resolveInboundUser(reactionMsg.Username, connName, team.Id, channel.Id)
	if err != nil {
		p.API.LogError("Inbound reaction: resolve user failed", "conn", connName, "username", reactionMsg.Username, "error", err.Error())
		return false
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
	return false
}

// watchFiles uses the provider's WatchFiles to monitor for new file uploads.
func (p *Plugin) watchFiles(ctx context.Context, connName string, provider QueueProvider) {
	p.API.LogInfo("File watcher started", "conn", connName)

	err := provider.WatchFiles(ctx, func(key string, data []byte, headers map[string]string) error {
		return p.handleInboundFile(ctx, connName, key, data, headers)
	})
	if err != nil {
		p.API.LogError("File watcher exited with error", "conn", connName, "error", err.Error())
	}
}

const (
	postMappingMaxRetries = 3
	postMappingRetryDelay = time.Second
)

func (p *Plugin) handleInboundFile(ctx context.Context, connName, key string, data []byte, headers map[string]string) error {
	headerConn := headers[headerConnName]
	remotePostID := headers[headerPostID]
	filename := headers[headerFilename]

	if headerConn == "" || remotePostID == "" || filename == "" {
		p.API.LogWarn("Inbound file: missing required headers, skipping",
			"key", key, headerConnName, headerConn, headerPostID, remotePostID, headerFilename, filename)
		return nil
	}

	if headerConn != connName {
		return nil
	}

	ic := p.getInboundConn(connName)
	if ic == nil {
		p.API.LogWarn("Inbound file: connection no longer active, skipping",
			"conn", connName, "filename", filename)
		return nil
	}

	if !isFileAllowed(filename, ic.fileFilterMode, ic.fileFilterTypes) {
		p.API.LogInfo("Inbound file filtered by policy",
			"filename", filename, "conn", connName)
		return nil
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
				return ctx.Err()
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
		return nil
	}

	// Block until a semaphore slot is available (or context is cancelled).
	select {
	case p.fileSem <- struct{}{}:
		defer func() { <-p.fileSem }()
	case <-ctx.Done():
		return ctx.Err()
	}

	existing, appErr := p.API.GetPost(localPostID)
	if appErr != nil {
		p.API.LogError("Inbound file: failed to get local post",
			"local_post_id", localPostID, "error", appErr.Error())
		return nil
	}

	fileInfo, appErr := p.API.UploadFile(data, existing.ChannelId, filename)
	if appErr != nil {
		p.API.LogError("Failed to upload file to Mattermost",
			"filename", filename, "error", appErr.Error())
		return nil
	}

	existing.FileIds = append(existing.FileIds, fileInfo.Id)
	if _, appErr := p.API.UpdatePost(existing); appErr != nil {
		p.API.LogError("Failed to attach file to post",
			"local_post_id", localPostID, "file_id", fileInfo.Id, "error", appErr.Error())
	}

	return nil
}
