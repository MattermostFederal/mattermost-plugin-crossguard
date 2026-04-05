package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

const (
	natsConnectTimeout      = 30 * time.Second
	natsRelayConnectTimeout = 5 * time.Second
	natsPublishMaxRetries   = 3
	natsPublishBaseDelay    = 500 * time.Millisecond
	natsPublishMaxDelay     = 5 * time.Second
	natsMaxReconnects       = -1 // unlimited
	natsReconnectWait       = 2 * time.Second
)

func connectNATS(conn NATSConnection) (*nats.Conn, error) {
	opts := []nats.Option{
		nats.Timeout(natsConnectTimeout),
	}

	if conn.TLSEnabled {
		hasCert := conn.ClientCert != ""
		hasKey := conn.ClientKey != ""
		if hasCert != hasKey {
			return nil, fmt.Errorf("both client_cert and client_key must be provided together")
		}

		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}

		if conn.ClientCert != "" && conn.ClientKey != "" {
			cert, err := tls.LoadX509KeyPair(conn.ClientCert, conn.ClientKey)
			if err != nil {
				return nil, fmt.Errorf("failed to load client certificate: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}

		if conn.CACert != "" {
			caCert, err := os.ReadFile(conn.CACert)
			if err != nil {
				return nil, fmt.Errorf("failed to read CA certificate: %w", err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("failed to parse CA certificate")
			}
			tlsConfig.RootCAs = caCertPool
		}

		opts = append(opts, nats.Secure(tlsConfig))
	}

	switch conn.AuthType {
	case AuthTypeToken:
		opts = append(opts, nats.Token(conn.Token))
	case AuthTypeCredentials:
		opts = append(opts, nats.UserInfo(conn.Username, conn.Password))
	}

	nc, err := nats.Connect(conn.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS at %s: %w", conn.Address, err)
	}

	return nc, nil
}

func buildTestMessage() ([]byte, string, error) {
	msgID := mmModel.NewId()

	testMsg := model.TestMessage{ID: msgID}

	envelope, err := model.NewMessage(model.MessageTypeTest, testMsg)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create test message envelope: %w", err)
	}

	data, err := model.Marshal(envelope)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal test message envelope: %w", err)
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
		nc, err := connectNATSPersistent(conn, p, "Outbound")
		if err != nil {
			p.API.LogError("Failed to connect outbound NATS for relay",
				"name", conn.Name, "address", conn.Address, "error", err.Error())
			continue
		}
		pool = append(pool, outboundConn{
			nc:                  nc,
			subject:             conn.Subject,
			name:                conn.Name,
			fileTransferEnabled: conn.FileTransferEnabled,
			fileFilterMode:      conn.FileFilterMode,
			fileFilterTypes:     conn.FileFilterTypes,
		})
		p.API.LogInfo("Outbound NATS connection established for relay", "name", conn.Name, "address", conn.Address)
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
		_ = oc.nc.Drain()
		oc.nc.Close()
	}
}

func (p *Plugin) reconnectOutbound() {
	p.closeOutbound()
	p.connectOutbound()
}

func connectNATSPersistent(conn NATSConnection, p *Plugin, direction string) (*nats.Conn, error) {
	opts := []nats.Option{
		nats.Timeout(natsRelayConnectTimeout),
		nats.MaxReconnects(natsMaxReconnects),
		nats.ReconnectWait(natsReconnectWait),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			}
			p.API.LogWarn(direction+" NATS disconnected", "name", conn.Name, "error", errMsg)
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			p.API.LogInfo(direction+" NATS reconnected", "name", conn.Name)
		}),
	}

	if conn.TLSEnabled {
		hasCert := conn.ClientCert != ""
		hasKey := conn.ClientKey != ""
		if hasCert != hasKey {
			return nil, fmt.Errorf("both client_cert and client_key must be provided together")
		}

		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}

		if conn.ClientCert != "" && conn.ClientKey != "" {
			cert, err := tls.LoadX509KeyPair(conn.ClientCert, conn.ClientKey)
			if err != nil {
				return nil, fmt.Errorf("failed to load client certificate: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}

		if conn.CACert != "" {
			caCert, err := os.ReadFile(conn.CACert)
			if err != nil {
				return nil, fmt.Errorf("failed to read CA certificate: %w", err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("failed to parse CA certificate")
			}
			tlsConfig.RootCAs = caCertPool
		}

		opts = append(opts, nats.Secure(tlsConfig))
	}

	switch conn.AuthType {
	case AuthTypeToken:
		opts = append(opts, nats.Token(conn.Token))
	case AuthTypeCredentials:
		opts = append(opts, nats.UserInfo(conn.Username, conn.Password))
	}

	nc, err := nats.Connect(conn.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS at %s: %w", conn.Address, err)
	}

	return nc, nil
}

func buildPostEnvelope(msgType string, post *mmModel.Post, channel *mmModel.Channel, teamName, username string) ([]byte, error) {
	postMsg := model.PostMessage{
		PostID:      post.Id,
		RootID:      post.RootId,
		ChannelID:   post.ChannelId,
		ChannelName: channel.Name,
		TeamID:      channel.TeamId,
		TeamName:    teamName,
		UserID:      post.UserId,
		Username:    username,
		Message:     post.Message,
		CreateAt:    post.CreateAt,
	}

	envelope, err := model.NewMessage(msgType, postMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to create post envelope: %w", err)
	}

	return model.Marshal(envelope)
}

func buildDeleteEnvelope(post *mmModel.Post, channel *mmModel.Channel, teamName string) ([]byte, error) {
	deleteMsg := model.DeleteMessage{
		PostID:      post.Id,
		ChannelID:   post.ChannelId,
		ChannelName: channel.Name,
		TeamID:      channel.TeamId,
		TeamName:    teamName,
	}

	envelope, err := model.NewMessage(model.MessageTypeDelete, deleteMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to create delete envelope: %w", err)
	}

	return model.Marshal(envelope)
}

func buildReactionEnvelope(msgType string, reaction *mmModel.Reaction, channel *mmModel.Channel, teamName, username string) ([]byte, error) {
	reactionMsg := model.ReactionMessage{
		PostID:      reaction.PostId,
		ChannelID:   channel.Id,
		ChannelName: channel.Name,
		TeamID:      channel.TeamId,
		TeamName:    teamName,
		UserID:      reaction.UserId,
		Username:    username,
		EmojiName:   reaction.EmojiName,
	}

	envelope, err := model.NewMessage(msgType, reactionMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to create reaction envelope: %w", err)
	}

	return model.Marshal(envelope)
}

func (p *Plugin) publishToOutbound(ctx context.Context, data []byte, conns []store.TeamConnection) {
	p.outboundMu.RLock()
	pool := make([]outboundConn, len(p.outboundConns))
	copy(pool, p.outboundConns)
	p.outboundMu.RUnlock()

	if len(pool) == 0 {
		return
	}

	for _, oc := range pool {
		if !isOutboundLinked(oc.name, conns) {
			continue
		}

		if !oc.nc.IsConnected() && !oc.nc.IsReconnecting() {
			p.API.LogWarn("Outbound NATS not connected, skipping", "name", oc.name)
			continue
		}

		var lastErr error
		for attempt := range natsPublishMaxRetries {
			if err := oc.nc.Publish(oc.subject, data); err != nil {
				lastErr = err
			} else if err := oc.nc.Flush(); err != nil {
				lastErr = err
			} else {
				lastErr = nil
				break
			}

			delay := min(natsPublishBaseDelay*time.Duration(1<<attempt), natsPublishMaxDelay)

			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}

		if lastErr != nil {
			p.API.LogError("Failed to publish to outbound NATS after retries",
				"name", oc.name, "error", lastErr.Error())
		}
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

const (
	objectStoreBucket  = "crossguard-files"
	objectStoreTTL     = time.Hour
	defaultMaxFileSize = 100 * 1024 * 1024 // 100 MB
	fileSemaphoreSize  = 32
	relaySemaphoreSize = 256

	headerPostID   = "X-Post-Id"
	headerConnName = "X-Conn-Name"
	headerFilename = "X-Filename"
)

func getOrCreateObjectStore(ctx context.Context, nc *nats.Conn, bucketName string) (jetstream.ObjectStore, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}
	obs, err := js.CreateOrUpdateObjectStore(ctx, jetstream.ObjectStoreConfig{
		Bucket: bucketName,
		TTL:    objectStoreTTL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create/update object store bucket %q: %w", bucketName, err)
	}
	return obs, nil
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

				objectStore, err := getOrCreateObjectStore(p.ctx, oc.nc, objectStoreBucket)
				if err != nil {
					p.API.LogError("Failed to open object store for file upload",
						"conn", oc.name, "error", err.Error())
					return
				}

				key := post.Id + "/" + mmModel.NewId()
				meta := jetstream.ObjectMeta{
					Name: key,
					Headers: nats.Header{
						headerPostID:   {post.Id},
						headerConnName: {oc.name},
						headerFilename: {fi.Name},
					},
				}

				_, err = objectStore.Put(p.ctx, meta, bytes.NewReader(data))
				if err != nil {
					p.API.LogError("Failed to upload file to object store",
						"key", key, "conn", oc.name, "error", err.Error())
				}
			}(oc, fi, fileData)
		}
	}
}
