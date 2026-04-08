package main

import (
	"context"
	"time"

	mmModel "github.com/mattermost/mattermost/server/public/model"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

const (
	fileSemaphoreSize  = 32
	relaySemaphoreSize = 256

	headerPostID   = "X-Post-Id"
	headerConnName = "X-Conn-Name"
	headerFilename = "X-Filename"
)

func buildTestMessage(format model.Format) ([]byte, string, error) {
	msgID := mmModel.NewId()
	env := &model.Envelope{
		Type:        model.MessageTypeTest,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		TestMessage: &model.TestMessage{ID: msgID},
	}
	data, err := model.Marshal(env, format)
	if err != nil {
		return nil, "", err
	}
	return data, msgID, nil
}

func (p *Plugin) connectOutbound() {
	cfg := p.getConfiguration()
	conns, err := cfg.GetOutboundConnections()
	if err != nil {
		p.API.LogError("Failed to parse outbound connections for relay", "error", err.Error())
		return
	}

	var pool []outboundConn
	for _, conn := range conns {
		provider, err := p.createProvider(conn, "Outbound")
		if err != nil {
			p.API.LogError("Failed to connect outbound for relay",
				"name", conn.Name, "provider", conn.Provider, "error", err.Error())
			continue
		}
		pool = append(pool, outboundConn{
			provider:            provider,
			name:                conn.Name,
			fileTransferEnabled: conn.FileTransferEnabled,
			fileFilterMode:      conn.FileFilterMode,
			fileFilterTypes:     conn.FileFilterTypes,
			messageFormat:       conn.MessageFormat,
			healthy:             true,
			lastCheckTime:       time.Now(),
		})
		p.API.LogInfo("Outbound connection established for relay", "name", conn.Name, "provider", conn.Provider)
	}

	p.outboundMu.Lock()
	p.outboundConns = pool
	p.outboundMu.Unlock()
}

func (p *Plugin) closeOutbound() {
	p.outboundMu.Lock()
	conns := p.outboundConns
	p.outboundConns = nil
	p.outboundMu.Unlock()

	for _, oc := range conns {
		_ = oc.provider.Close()
	}
}

func (p *Plugin) reconnectOutbound() {
	p.closeOutbound()
	p.connectOutbound()
}

func buildPostEnvelope(msgType string, post *mmModel.Post, channel *mmModel.Channel, teamName, username string) *model.Envelope {
	pm := model.PostMessage{
		PostID:      post.Id,
		RootID:      post.RootId,
		ChannelID:   post.ChannelId,
		ChannelName: channel.Name,
		TeamID:      channel.TeamId,
		TeamName:    teamName,
		UserID:      post.UserId,
		Username:    username,
		MessageText: post.Message,
		CreateAt:    post.CreateAt,
	}
	return &model.Envelope{
		Type:        msgType,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		PostMessage: &pm,
	}
}

func buildDeleteEnvelope(post *mmModel.Post, channel *mmModel.Channel, teamName string) *model.Envelope {
	dm := model.DeleteMessage{
		PostID:      post.Id,
		ChannelID:   post.ChannelId,
		ChannelName: channel.Name,
		TeamID:      channel.TeamId,
		TeamName:    teamName,
	}
	return &model.Envelope{
		Type:          model.MessageTypeDelete,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		DeleteMessage: &dm,
	}
}

func buildReactionEnvelope(msgType string, reaction *mmModel.Reaction, channel *mmModel.Channel, teamName, username string) *model.Envelope {
	rm := model.ReactionMessage{
		PostID:      reaction.PostId,
		ChannelID:   channel.Id,
		ChannelName: channel.Name,
		TeamID:      channel.TeamId,
		TeamName:    teamName,
		UserID:      reaction.UserId,
		Username:    username,
		EmojiName:   reaction.EmojiName,
	}
	return &model.Envelope{
		Type:            msgType,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		ReactionMessage: &rm,
	}
}

const healthRecheckInterval = 30 * time.Second

func (p *Plugin) publishToOutbound(ctx context.Context, env *model.Envelope, conns []store.TeamConnection) {
	p.outboundMu.RLock()
	pool := make([]outboundConn, len(p.outboundConns))
	copy(pool, p.outboundConns)
	p.outboundMu.RUnlock()

	if len(pool) == 0 {
		return
	}

	for i, oc := range pool {
		if !isOutboundLinked(oc.name, conns) {
			continue
		}

		// Skip unhealthy connections unless it is time for a recheck.
		if !oc.healthy && time.Since(oc.lastCheckTime) < healthRecheckInterval {
			continue
		}

		format := model.Format(oc.messageFormat)
		if format == "" {
			format = model.FormatJSON
		}
		data, err := model.Marshal(env, format)
		if err != nil {
			p.API.LogError("Failed to serialize outbound message",
				"name", oc.name, "format", string(format), "error", err.Error())
			continue
		}

		// Check message size limit and truncate if needed.
		maxSize := oc.provider.MaxMessageSize()
		if maxSize > 0 && len(data) > maxSize && env.PostMessage != nil {
			originalText := env.PostMessage.MessageText
			env.PostMessage.MessageText = truncateToFit(env, format, maxSize)
			p.API.LogWarn("Message truncated to fit provider size limit",
				"connection", oc.name, "maxSize", maxSize, "originalSize", len(data))
			data, err = model.Marshal(env, format)
			env.PostMessage.MessageText = originalText
			if err != nil {
				p.API.LogError("Failed to serialize truncated message",
					"name", oc.name, "error", err.Error())
				continue
			}
		}

		if err := oc.provider.Publish(ctx, data); err != nil {
			p.API.LogError("Failed to publish to outbound after retries",
				"name", oc.name, "error", err.Error())
			p.updateOutboundHealth(i, false)
		} else {
			p.updateOutboundHealth(i, true)
		}
	}
}

func (p *Plugin) updateOutboundHealth(index int, healthy bool) {
	p.outboundMu.Lock()
	defer p.outboundMu.Unlock()
	if index < len(p.outboundConns) {
		p.outboundConns[index].healthy = healthy
		p.outboundConns[index].lastCheckTime = time.Now()
	}
}

// isOutboundLinked checks if an outbound connection name is in the team's linked list.
func isOutboundLinked(outboundName string, conns []store.TeamConnection) bool {
	for _, tc := range conns {
		if tc.Direction == "outbound" && tc.Connection == outboundName {
			return true
		}
	}
	return false
}

func (p *Plugin) uploadPostFiles(post *mmModel.Post, conns []store.TeamConnection) {
	p.outboundMu.RLock()
	var fileConns []outboundConn
	for _, oc := range p.outboundConns {
		if oc.fileTransferEnabled && isOutboundLinked(oc.name, conns) {
			fileConns = append(fileConns, oc)
		}
	}
	p.outboundMu.RUnlock()

	if len(fileConns) == 0 {
		return
	}

	var maxFileSize int64 = defaultMaxFileSize
	if cfg := p.API.GetConfig(); cfg != nil && cfg.FileSettings.MaxFileSize != nil {
		maxFileSize = *cfg.FileSettings.MaxFileSize
	}

	for _, fileID := range post.FileIds {
		fi, appErr := p.API.GetFileInfo(fileID)
		if appErr != nil {
			p.API.LogError("Failed to get file info for relay",
				"file_id", fileID, "post_id", post.Id, "error", appErr.Error())
			continue
		}
		if fi.Size > maxFileSize {
			p.API.LogWarn("Skipping oversized file for relay",
				"file", fi.Name, "size", fi.Size, "max", maxFileSize)
			continue
		}

		fileData, appErr := p.API.GetFile(fi.Id)
		if appErr != nil {
			p.API.LogError("Failed to download file for relay",
				"file_id", fi.Id, "file", fi.Name, "error", appErr.Error())
			continue
		}

		for _, oc := range fileConns {
			if !isFileAllowed(fi.Name, oc.fileFilterMode, oc.fileFilterTypes) {
				p.API.LogInfo("Outbound file filtered by policy",
					"file", fi.Name, "conn", oc.name)
				continue
			}

			p.wg.Add(1)
			go func(oc outboundConn, fi *mmModel.FileInfo, data []byte) {
				defer p.wg.Done()

				select {
				case p.fileSem <- struct{}{}:
					defer func() { <-p.fileSem }()
				default:
					p.API.LogWarn("File semaphore full, skipping file upload",
						"file", fi.Name, "conn", oc.name)
					return
				}

				key := post.Id + "/" + mmModel.NewId()
				headers := map[string]string{
					headerPostID:   post.Id,
					headerConnName: oc.name,
					headerFilename: fi.Name,
				}

				if err := oc.provider.UploadFile(p.ctx, key, data, headers); err != nil {
					p.API.LogError("Failed to upload file",
						"key", key, "conn", oc.name, "error", err.Error())
				}
			}(oc, fi, fileData)
		}
	}
}

// createProvider constructs a QueueProvider based on the connection config.
func (p *Plugin) createProvider(cfg ConnectionConfig, direction string) (QueueProvider, error) {
	switch cfg.Provider {
	case ProviderNATS, "":
		if cfg.NATS == nil {
			return nil, errMissingNATSConfig
		}
		return newNATSProvider(*cfg.NATS, p.API, direction)
	case ProviderAzure:
		if cfg.Azure == nil {
			return nil, errMissingAzureConfig
		}
		return newAzureProvider(*cfg.Azure, p.API)
	default:
		return nil, errUnknownProvider(cfg.Provider)
	}
}

const safetyMargin = 500

// truncateToFit truncates the message text to fit within the provider's size limit.
func truncateToFit(env *model.Envelope, format model.Format, maxSize int) string {
	// Measure overhead by marshaling with empty message text.
	originalText := env.PostMessage.MessageText
	env.PostMessage.MessageText = ""
	overhead, err := model.Marshal(env, format)
	env.PostMessage.MessageText = originalText
	if err != nil {
		return originalText
	}

	available := maxSize - len(overhead) - safetyMargin
	if available <= 0 {
		return "\n[message truncated]"
	}

	text := originalText
	if len(text) > available {
		// Truncate at a UTF-8 safe boundary.
		text = text[:available]
		// Find the last valid UTF-8 boundary.
		for len(text) > 0 && text[len(text)-1]&0xC0 == 0x80 {
			text = text[:len(text)-1]
		}
		if len(text) > 0 && text[len(text)-1]&0x80 != 0 {
			text = text[:len(text)-1]
		}
	}

	return text + "\n[message truncated]"
}
