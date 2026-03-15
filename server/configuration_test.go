package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
)

func TestParseConnections(t *testing.T) {
	t.Run("empty string returns nil", func(t *testing.T) {
		conns, err := parseConnections("")
		require.NoError(t, err)
		assert.Nil(t, conns)
	})

	t.Run("empty array string returns nil", func(t *testing.T) {
		conns, err := parseConnections("[]")
		require.NoError(t, err)
		assert.Nil(t, conns)
	})

	t.Run("whitespace only returns nil", func(t *testing.T) {
		conns, err := parseConnections("   ")
		require.NoError(t, err)
		assert.Nil(t, conns)
	})

	t.Run("valid JSON parses correctly", func(t *testing.T) {
		input := `[{"name":"test","address":"nats://localhost:4222","subject":"crossguard.test","tls_enabled":false,"auth_type":"none","token":"","username":"","password":"","client_cert":"","client_key":"","ca_cert":""}]`
		conns, err := parseConnections(input)
		require.NoError(t, err)
		require.Len(t, conns, 1)
		assert.Equal(t, "test", conns[0].Name)
		assert.Equal(t, "nats://localhost:4222", conns[0].Address)
		assert.Equal(t, "crossguard.test", conns[0].Subject)
	})

	t.Run("multiple connections parse correctly", func(t *testing.T) {
		input := `[{"name":"first","address":"nats://host1:4222","subject":"crossguard.sub1","auth_type":"none"},{"name":"second","address":"nats://host2:4222","subject":"crossguard.sub2","auth_type":"token","token":"mytoken"}]`
		conns, err := parseConnections(input)
		require.NoError(t, err)
		require.Len(t, conns, 2)
		assert.Equal(t, "first", conns[0].Name)
		assert.Equal(t, "second", conns[1].Name)
		assert.Equal(t, "token", conns[1].AuthType)
		assert.Equal(t, "mytoken", conns[1].Token)
	})

	t.Run("malformed JSON returns error", func(t *testing.T) {
		_, err := parseConnections("{not valid json")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse connections")
	})
}

func TestConfigurationValidate(t *testing.T) {
	t.Run("empty config is valid", func(t *testing.T) {
		cfg := &configuration{}
		assert.NoError(t, cfg.validate())
	})

	t.Run("empty arrays are valid", func(t *testing.T) {
		cfg := &configuration{
			InboundConnections:  "[]",
			OutboundConnections: "[]",
		}
		assert.NoError(t, cfg.validate())
	})

	t.Run("valid connections pass validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "conn1", Address: "nats://localhost:4222", Subject: "crossguard.test", AuthType: "none"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{
			InboundConnections: string(data),
		}
		assert.NoError(t, cfg.validate())
	})

	t.Run("missing name fails validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "", Address: "nats://localhost:4222", Subject: "crossguard.test", AuthType: "none"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("missing address fails validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "", Subject: "crossguard.test", AuthType: "none"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "address is required")
	})

	t.Run("missing subject fails validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "nats://localhost:4222", Subject: "", AuthType: "none"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "subject is required")
	})

	t.Run("subject without required prefix fails validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "nats://localhost:4222", Subject: "bad.prefix.sub", AuthType: "none"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "subject must start with")
	})

	t.Run("duplicate names fail validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "dup", Address: "nats://host1:4222", Subject: "crossguard.sub1", AuthType: "none"},
			{Name: "dup", Address: "nats://host2:4222", Subject: "crossguard.sub2", AuthType: "none"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate name")
	})

	t.Run("invalid auth_type fails validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "invalid"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "auth_type must be")
	})

	t.Run("token auth without token fails", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "token", Token: ""},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "token is required")
	})

	t.Run("credentials auth without username fails", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "credentials", Username: "", Password: "pass"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "username is required")
	})

	t.Run("credentials auth without password fails", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "credentials", Username: "user", Password: ""},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "password is required")
	})

	t.Run("valid credentials auth passes", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "credentials", Username: "user", Password: "pass"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		assert.NoError(t, cfg.validate())
	})

	t.Run("valid token auth passes", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "token", Token: "mytoken"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		assert.NoError(t, cfg.validate())
	})

	t.Run("name with uppercase letters fails validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "MyConn", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name must contain only lowercase letters, numbers, and hyphens")
	})

	t.Run("name with spaces fails validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "my conn", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name must contain only lowercase letters, numbers, and hyphens")
	})

	t.Run("name with special characters fails validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "my_conn!", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name must contain only lowercase letters, numbers, and hyphens")
	})

	t.Run("valid name with hyphens passes", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "my-nats-conn", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		assert.NoError(t, cfg.validate())
	})

	t.Run("name with leading hyphen fails validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "-leading", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name must contain only lowercase letters, numbers, and hyphens")
	})

	t.Run("name with trailing hyphen fails validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "trailing-", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name must contain only lowercase letters, numbers, and hyphens")
	})

	t.Run("name with consecutive hyphens fails validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "my--conn", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name must contain only lowercase letters, numbers, and hyphens")
	})

	t.Run("malformed JSON reports error", func(t *testing.T) {
		cfg := &configuration{InboundConnections: "not json"}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "inbound connections")
	})

	t.Run("duplicate names across inbound and outbound fail validation", func(t *testing.T) {
		inbound := []NATSConnection{
			{Name: "shared-name", Address: "nats://host1:4222", Subject: "crossguard.sub1", AuthType: "none"},
		}
		outbound := []NATSConnection{
			{Name: "shared-name", Address: "nats://host2:4222", Subject: "crossguard.sub2", AuthType: "none"},
		}
		inData, _ := json.Marshal(inbound)
		outData, _ := json.Marshal(outbound)
		cfg := &configuration{
			InboundConnections:  string(inData),
			OutboundConnections: string(outData),
		}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate name")
	})

	t.Run("errors from both directions are aggregated", func(t *testing.T) {
		cfg := &configuration{
			InboundConnections:  "not json",
			OutboundConnections: "also not json",
		}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "inbound")
		assert.Contains(t, err.Error(), "outbound")
	})
}

func TestIsTestMessage(t *testing.T) {
	t.Run("valid test message is detected", func(t *testing.T) {
		envelope, err := model.NewMessage(model.MessageTypeTest, model.TestMessage{ID: "abc-123"})
		require.NoError(t, err)
		data, err := model.Marshal(envelope)
		require.NoError(t, err)

		result, ok := isTestMessage(data)
		require.True(t, ok)
		assert.Equal(t, "abc-123", result.ID)
	})

	t.Run("non-test message type is not detected", func(t *testing.T) {
		envelope, err := model.NewMessage("regular_message", map[string]string{"content": "hello"})
		require.NoError(t, err)
		data, err := model.Marshal(envelope)
		require.NoError(t, err)

		result, ok := isTestMessage(data)
		assert.False(t, ok)
		assert.Nil(t, result)
	})

	t.Run("invalid JSON is not detected", func(t *testing.T) {
		result, ok := isTestMessage([]byte("not json"))
		assert.False(t, ok)
		assert.Nil(t, result)
	})

	t.Run("empty type is not detected", func(t *testing.T) {
		envelope, err := model.NewMessage("", model.TestMessage{ID: "123"})
		require.NoError(t, err)
		data, err := model.Marshal(envelope)
		require.NoError(t, err)

		result, ok := isTestMessage(data)
		assert.False(t, ok)
		assert.Nil(t, result)
	})
}

func TestBuildTestMessage(t *testing.T) {
	data, msgID, err := buildTestMessage()
	require.NoError(t, err)
	require.NotEmpty(t, msgID)
	require.NotEmpty(t, data)

	envelope, err := model.UnmarshalMessage(data)
	require.NoError(t, err)
	assert.Equal(t, model.MessageTypeTest, envelope.Type)
	assert.NotEmpty(t, envelope.Timestamp)

	var testMsg model.TestMessage
	err = envelope.Decode(&testMsg)
	require.NoError(t, err)
	assert.Equal(t, msgID, testMsg.ID)
}
