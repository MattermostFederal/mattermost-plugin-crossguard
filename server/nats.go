package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/nats-io/nats.go"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
)

const natsConnectTimeout = 30 * time.Second

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
