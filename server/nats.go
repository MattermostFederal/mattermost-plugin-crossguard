package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"slices"
	"time"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/nats-io/nats.go"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
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
		pool = append(pool, outboundConn{nc: nc, subject: conn.Subject, name: conn.Name})
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

func (p *Plugin) publishToOutbound(ctx context.Context, data []byte, connNames []string) {
	p.outboundMu.RLock()
	conns := make([]outboundConn, len(p.outboundConns))
	copy(conns, p.outboundConns)
	p.outboundMu.RUnlock()

	if len(conns) == 0 {
		return
	}

	for _, oc := range conns {
		if !isOutboundLinked(oc.name, connNames) {
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
func isOutboundLinked(outboundName string, connNames []string) bool {
	return slices.Contains(connNames, "outbound:"+outboundName)
}
