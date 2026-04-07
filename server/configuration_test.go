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

	t.Run("empty message_format defaults to json and passes", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none", MessageFormat: ""},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{OutboundConnections: string(data)}
		assert.NoError(t, cfg.validate())
	})

	t.Run("xml message_format passes", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none", MessageFormat: "xml"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{OutboundConnections: string(data)}
		assert.NoError(t, cfg.validate())
	})

	t.Run("invalid message_format fails validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none", MessageFormat: "yaml"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{OutboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "message_format must be")
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

func TestIsUsernameLookupEnabled(t *testing.T) {
	t.Run("nil defaults to true", func(t *testing.T) {
		cfg := &configuration{}
		assert.True(t, cfg.isUsernameLookupEnabled())
	})

	t.Run("explicitly true", func(t *testing.T) {
		cfg := &configuration{UsernameLookup: new(true)}
		assert.True(t, cfg.isUsernameLookupEnabled())
	})

	t.Run("explicitly false", func(t *testing.T) {
		cfg := &configuration{UsernameLookup: new(false)}
		assert.False(t, cfg.isUsernameLookupEnabled())
	})
}

func TestIsRestrictedToSystemAdmins(t *testing.T) {
	t.Run("nil defaults to false", func(t *testing.T) {
		cfg := &configuration{}
		assert.False(t, cfg.isRestrictedToSystemAdmins())
	})

	t.Run("explicitly true", func(t *testing.T) {
		cfg := &configuration{RestrictToSystemAdmins: new(true)}
		assert.True(t, cfg.isRestrictedToSystemAdmins())
	})

	t.Run("explicitly false", func(t *testing.T) {
		cfg := &configuration{RestrictToSystemAdmins: new(false)}
		assert.False(t, cfg.isRestrictedToSystemAdmins())
	})
}

func TestIsTestMessage(t *testing.T) {
	t.Run("valid test message is detected via JSON", func(t *testing.T) {
		env := &model.Envelope{
			Type:        model.MessageTypeTest,
			Timestamp:   "2026-04-06T12:00:00Z",
			TestMessage: &model.TestMessage{ID: "abc-123"},
		}
		data, err := model.Marshal(env, model.FormatJSON)
		require.NoError(t, err)

		result, ok := isTestMessage(data)
		require.True(t, ok)
		assert.Equal(t, "abc-123", result.ID)
	})

	t.Run("valid test message is detected via XML", func(t *testing.T) {
		env := &model.Envelope{
			Type:        model.MessageTypeTest,
			Timestamp:   "2026-04-06T12:00:00Z",
			TestMessage: &model.TestMessage{ID: "xml-456"},
		}
		data, err := model.Marshal(env, model.FormatXML)
		require.NoError(t, err)

		result, ok := isTestMessage(data)
		require.True(t, ok)
		assert.Equal(t, "xml-456", result.ID)
	})

	t.Run("non-test message type is not detected", func(t *testing.T) {
		env := &model.Envelope{
			Type:        "regular_message",
			Timestamp:   "2026-04-06T12:00:00Z",
			PostMessage: &model.PostMessage{PostID: "p1"},
		}
		data, err := model.Marshal(env, model.FormatJSON)
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
		env := &model.Envelope{
			Type:        "",
			Timestamp:   "2026-04-06T12:00:00Z",
			TestMessage: &model.TestMessage{ID: "123"},
		}
		data, err := model.Marshal(env, model.FormatJSON)
		require.NoError(t, err)

		result, ok := isTestMessage(data)
		assert.False(t, ok)
		assert.Nil(t, result)
	})
}

func TestIsFileAllowed(t *testing.T) {
	t.Run("no filter mode allows all files", func(t *testing.T) {
		conn := NATSConnection{FileFilterMode: ""}
		assert.True(t, conn.IsFileAllowed("document.pdf"))
		assert.True(t, conn.IsFileAllowed("image.png"))
		assert.True(t, conn.IsFileAllowed("noext"))
	})

	t.Run("allow mode permits listed types", func(t *testing.T) {
		conn := NATSConnection{FileFilterMode: "allow", FileFilterTypes: ".pdf,.docx,.png"}
		assert.True(t, conn.IsFileAllowed("report.pdf"))
		assert.True(t, conn.IsFileAllowed("doc.docx"))
		assert.True(t, conn.IsFileAllowed("image.png"))
		assert.False(t, conn.IsFileAllowed("script.exe"))
		assert.False(t, conn.IsFileAllowed("noext"))
	})

	t.Run("deny mode blocks listed types", func(t *testing.T) {
		conn := NATSConnection{FileFilterMode: "deny", FileFilterTypes: ".exe,.bat"}
		assert.False(t, conn.IsFileAllowed("virus.exe"))
		assert.False(t, conn.IsFileAllowed("script.bat"))
		assert.True(t, conn.IsFileAllowed("document.pdf"))
		assert.True(t, conn.IsFileAllowed("image.png"))
	})

	t.Run("case insensitive matching", func(t *testing.T) {
		conn := NATSConnection{FileFilterMode: "allow", FileFilterTypes: ".PDF,.Docx"}
		assert.True(t, conn.IsFileAllowed("REPORT.pdf"))
		assert.True(t, conn.IsFileAllowed("doc.DOCX"))
		assert.False(t, conn.IsFileAllowed("image.png"))
	})

	t.Run("types without leading dot are normalized", func(t *testing.T) {
		conn := NATSConnection{FileFilterMode: "allow", FileFilterTypes: "pdf,docx"}
		assert.True(t, conn.IsFileAllowed("report.pdf"))
		assert.True(t, conn.IsFileAllowed("doc.docx"))
		assert.False(t, conn.IsFileAllowed("image.png"))
	})

	t.Run("file with no extension in deny mode", func(t *testing.T) {
		conn := NATSConnection{FileFilterMode: "deny", FileFilterTypes: ".exe"}
		assert.True(t, conn.IsFileAllowed("Makefile"))
	})

	t.Run("spaces in filter types are trimmed", func(t *testing.T) {
		conn := NATSConnection{FileFilterMode: "allow", FileFilterTypes: " .pdf , .docx "}
		assert.True(t, conn.IsFileAllowed("report.pdf"))
		assert.True(t, conn.IsFileAllowed("doc.docx"))
	})
}

func TestFileFilterValidation(t *testing.T) {
	t.Run("invalid file_filter_mode fails validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none", FileFilterMode: "invalid"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "file_filter_mode must be")
	})

	t.Run("allow mode without types fails validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none", FileFilterMode: "allow", FileFilterTypes: ""},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "file_filter_types is required")
	})

	t.Run("deny mode without types fails validation", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none", FileFilterMode: "deny", FileFilterTypes: ""},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		err := cfg.validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "file_filter_types is required")
	})

	t.Run("allow mode with types passes", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none", FileFilterMode: "allow", FileFilterTypes: ".pdf"},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		assert.NoError(t, cfg.validate())
	})

	t.Run("file transfer enabled without filter passes", func(t *testing.T) {
		conns := []NATSConnection{
			{Name: "test", Address: "nats://localhost:4222", Subject: "crossguard.sub", AuthType: "none", FileTransferEnabled: true},
		}
		data, _ := json.Marshal(conns)
		cfg := &configuration{InboundConnections: string(data)}
		assert.NoError(t, cfg.validate())
	})
}

func TestBuildTestMessage(t *testing.T) {
	t.Run("JSON format", func(t *testing.T) {
		data, msgID, err := buildTestMessage(model.FormatJSON)
		require.NoError(t, err)
		require.NotEmpty(t, msgID)
		require.NotEmpty(t, data)

		env, err := model.Unmarshal(data, model.FormatJSON)
		require.NoError(t, err)
		assert.Equal(t, model.MessageTypeTest, env.Type)
		assert.NotEmpty(t, env.Timestamp)
		require.NotNil(t, env.TestMessage)
		assert.Equal(t, msgID, env.TestMessage.ID)
	})

	t.Run("XML format", func(t *testing.T) {
		data, msgID, err := buildTestMessage(model.FormatXML)
		require.NoError(t, err)
		require.NotEmpty(t, msgID)
		require.NotEmpty(t, data)

		env, err := model.Unmarshal(data, model.FormatXML)
		require.NoError(t, err)
		assert.Equal(t, model.MessageTypeTest, env.Type)
		assert.NotEmpty(t, env.Timestamp)
		require.NotNil(t, env.TestMessage)
		assert.Equal(t, msgID, env.TestMessage.ID)
	})
}
