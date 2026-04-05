package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
)

var validNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const subjectPrefix = "crossguard."

const (
	AuthTypeNone        = "none"
	AuthTypeToken       = "token"
	AuthTypeCredentials = "credentials"

	fileFilterModeAllow = "allow"
	fileFilterModeDeny  = "deny"
)

// NATSConnection represents a single NATS connection configuration.
type NATSConnection struct {
	Name       string `json:"name"`
	Address    string `json:"address"`
	Subject    string `json:"subject"`
	TLSEnabled bool   `json:"tls_enabled"`
	AuthType   string `json:"auth_type"`
	Token      string `json:"token"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	ClientCert string `json:"client_cert"`
	ClientKey  string `json:"client_key"`
	CACert     string `json:"ca_cert"`

	FileTransferEnabled bool   `json:"file_transfer_enabled"`
	FileFilterMode      string `json:"file_filter_mode"`  // "", "allow", "deny"
	FileFilterTypes     string `json:"file_filter_types"` // ".pdf,.docx,.png"
}

// IsFileAllowed checks whether a filename passes this connection's file filter.
func (c NATSConnection) IsFileAllowed(filename string) bool {
	return isFileAllowed(filename, c.FileFilterMode, c.FileFilterTypes)
}

func isFileAllowed(filename, filterMode, filterTypes string) bool {
	if filterMode == "" {
		return true
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		ext = "."
	}

	types := parseFilterTypes(filterTypes)
	found := slices.Contains(types, ext)

	if filterMode == fileFilterModeAllow {
		return found
	}
	// deny mode
	return !found
}

func parseFilterTypes(raw string) []string {
	var types []string
	for part := range strings.SplitSeq(raw, ",") {
		t := strings.TrimSpace(strings.ToLower(part))
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, ".") {
			t = "." + t
		}
		types = append(types, t)
	}
	return types
}

type configuration struct {
	InboundConnections     string `json:"InboundConnections"`
	OutboundConnections    string `json:"OutboundConnections"`
	UsernameLookup         *bool  `json:"UsernameLookup"`
	RestrictToSystemAdmins *bool  `json:"RestrictToSystemAdmins"`
}

func (c *configuration) isUsernameLookupEnabled() bool {
	return c.UsernameLookup == nil || *c.UsernameLookup
}

func (c *configuration) isRestrictedToSystemAdmins() bool {
	return c.RestrictToSystemAdmins != nil && *c.RestrictToSystemAdmins
}

func parseConnections(raw string) ([]NATSConnection, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "[]" {
		return nil, nil
	}

	var connections []NATSConnection
	if err := json.Unmarshal([]byte(trimmed), &connections); err != nil {
		return nil, fmt.Errorf("failed to parse connections: %w", err)
	}

	return connections, nil
}

func (c *configuration) GetInboundConnections() ([]NATSConnection, error) {
	return parseConnections(c.InboundConnections)
}

func (c *configuration) GetOutboundConnections() ([]NATSConnection, error) {
	return parseConnections(c.OutboundConnections)
}

func (c *configuration) validate() error {
	var errs []string
	allNames := make(map[string]bool)

	inbound, err := c.GetInboundConnections()
	if err != nil {
		errs = append(errs, fmt.Sprintf("inbound connections: %s", err.Error()))
	} else {
		errs = append(errs, validateConnectionList(inbound, "inbound", allNames)...)
	}

	outbound, err := c.GetOutboundConnections()
	if err != nil {
		errs = append(errs, fmt.Sprintf("outbound connections: %s", err.Error()))
	} else {
		errs = append(errs, validateConnectionList(outbound, "outbound", allNames)...)
	}

	if len(errs) > 0 {
		return fmt.Errorf("configuration errors: %s", strings.Join(errs, "; "))
	}

	return nil
}

func validateConnectionList(connections []NATSConnection, direction string, allNames map[string]bool) []string {
	var errs []string

	for i, conn := range connections {
		prefix := fmt.Sprintf("%s connection %d", direction, i)

		trimmedName := strings.TrimSpace(conn.Name)
		switch {
		case trimmedName == "":
			errs = append(errs, fmt.Sprintf("%s: name is required", prefix))
		case !validNamePattern.MatchString(trimmedName):
			errs = append(errs, fmt.Sprintf("%s: name must contain only lowercase letters, numbers, and hyphens", prefix))
		case allNames[trimmedName]:
			errs = append(errs, fmt.Sprintf("%s: duplicate name %q", prefix, trimmedName))
		default:
			allNames[trimmedName] = true
		}

		if strings.TrimSpace(conn.Address) == "" {
			errs = append(errs, fmt.Sprintf("%s: address is required", prefix))
		}

		trimmedSubject := strings.TrimSpace(conn.Subject)
		switch {
		case trimmedSubject == "":
			errs = append(errs, fmt.Sprintf("%s: subject is required", prefix))
		case !strings.HasPrefix(trimmedSubject, subjectPrefix):
			errs = append(errs, fmt.Sprintf("%s: subject must start with %q", prefix, subjectPrefix))
		}

		switch conn.AuthType {
		case AuthTypeNone, AuthTypeToken, AuthTypeCredentials, "":
			// valid
		default:
			errs = append(errs, fmt.Sprintf("%s: auth_type must be \"none\", \"token\", or \"credentials\"", prefix))
		}

		if conn.AuthType == AuthTypeToken && strings.TrimSpace(conn.Token) == "" {
			errs = append(errs, fmt.Sprintf("%s: token is required when auth_type is \"token\"", prefix))
		}

		if conn.AuthType == AuthTypeCredentials {
			if strings.TrimSpace(conn.Username) == "" {
				errs = append(errs, fmt.Sprintf("%s: username is required when auth_type is \"credentials\"", prefix))
			}
			if strings.TrimSpace(conn.Password) == "" {
				errs = append(errs, fmt.Sprintf("%s: password is required when auth_type is \"credentials\"", prefix))
			}
		}

		switch conn.FileFilterMode {
		case "", fileFilterModeAllow, fileFilterModeDeny:
			// valid
		default:
			errs = append(errs, fmt.Sprintf("%s: file_filter_mode must be \"\", \"allow\", or \"deny\"", prefix))
		}

		if (conn.FileFilterMode == fileFilterModeAllow || conn.FileFilterMode == fileFilterModeDeny) && strings.TrimSpace(conn.FileFilterTypes) == "" {
			errs = append(errs, fmt.Sprintf("%s: file_filter_types is required when file_filter_mode is set", prefix))
		}
	}

	return errs
}

func isTestMessage(data []byte) (*model.TestMessage, bool) {
	envelope, err := model.UnmarshalMessage(data)
	if err != nil {
		return nil, false
	}

	if envelope.Type != model.MessageTypeTest {
		return nil, false
	}

	var testMsg model.TestMessage
	if err := envelope.Decode(&testMsg); err != nil {
		return nil, false
	}

	return &testMsg, true
}

func (p *Plugin) getConfiguration() *configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()

	if p.configuration == nil {
		return &configuration{}
	}

	return p.configuration
}

func (p *Plugin) setConfiguration(configuration *configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.configuration == configuration {
		if p.API != nil {
			p.API.LogWarn("setConfiguration called with the existing configuration")
		}
		return
	}

	p.configuration = configuration
}

func (p *Plugin) OnConfigurationChange() error {
	cfg := new(configuration)

	if err := p.API.LoadPluginConfiguration(cfg); err != nil {
		return fmt.Errorf("failed to load plugin configuration: %w", err)
	}

	if err := cfg.validate(); err != nil {
		p.API.LogWarn("Plugin configuration has validation warnings", "error", err.Error())
	}

	p.setConfiguration(cfg)

	if p.relaySem != nil {
		p.reconnectOutbound()
		p.reconnectInbound()
	}

	return nil
}
