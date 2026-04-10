package main

import (
	"encoding/json"
	"errors"
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

	ProviderNATS  = "nats"
	ProviderAzure = "azure"
)

var (
	errMissingNATSConfig  = errors.New("nats config block is required when provider is \"nats\"")
	errMissingAzureConfig = errors.New("azure config block is required when provider is \"azure\"")
)

func errUnknownProvider(p string) error {
	return fmt.Errorf("unknown provider %q, must be \"nats\" or \"azure\"", p)
}

// ConnectionConfig represents a single connection configuration.
// It supports both NATS and Azure providers via nested sub-structs.
type ConnectionConfig struct {
	Name     string `json:"name"`
	Provider string `json:"provider"` // "nats" or "azure"

	// Common fields
	FileTransferEnabled bool   `json:"file_transfer_enabled"`
	FileFilterMode      string `json:"file_filter_mode"`  // "", "allow", "deny"
	FileFilterTypes     string `json:"file_filter_types"` // ".pdf,.docx,.png"
	MessageFormat       string `json:"message_format"`    // "json" or "xml"

	// Provider-specific (exactly one must be set, matching Provider)
	NATS  *NATSProviderConfig  `json:"nats,omitempty"`
	Azure *AzureProviderConfig `json:"azure,omitempty"`
}

// NATSProviderConfig holds NATS-specific connection settings.
type NATSProviderConfig struct {
	Name       string `json:"name,omitempty"` // populated from parent at parse time
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
}

// AzureProviderConfig holds Azure Queue Storage and Blob Storage connection settings.
type AzureProviderConfig struct {
	ConnectionString  string `json:"connection_string"`
	QueueName         string `json:"queue_name"`
	BlobContainerName string `json:"blob_container_name"`
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

func parseConnections(raw string) ([]ConnectionConfig, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "[]" {
		return nil, nil
	}

	var connections []ConnectionConfig
	if err := json.Unmarshal([]byte(trimmed), &connections); err != nil {
		return nil, fmt.Errorf("failed to parse connections: %w", err)
	}

	// Default provider to NATS and populate nested name field.
	for i := range connections {
		if connections[i].Provider == "" {
			connections[i].Provider = ProviderNATS
		}
		if connections[i].NATS != nil {
			connections[i].NATS.Name = connections[i].Name
		}
	}

	return connections, nil
}

func (c *configuration) GetInboundConnections() ([]ConnectionConfig, error) {
	return parseConnections(c.InboundConnections)
}

func (c *configuration) GetOutboundConnections() ([]ConnectionConfig, error) {
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

func validateConnectionList(connections []ConnectionConfig, direction string, allNames map[string]bool) []string {
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

		switch conn.Provider {
		case ProviderNATS, "":
			errs = append(errs, validateNATSConnection(conn, prefix)...)
		case ProviderAzure:
			errs = append(errs, validateAzureConnection(conn, prefix)...)
		default:
			errs = append(errs, fmt.Sprintf("%s: provider must be \"nats\" or \"azure\"", prefix))
		}

		if conn.MessageFormat == "" {
			connections[i].MessageFormat = "json"
			conn.MessageFormat = "json"
		}
		switch conn.MessageFormat {
		case "json", "xml":
			// valid
		default:
			errs = append(errs, fmt.Sprintf("%s: message_format must be \"json\" or \"xml\"", prefix))
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

func validateNATSConnection(conn ConnectionConfig, prefix string) []string {
	var errs []string

	if conn.NATS == nil {
		errs = append(errs, fmt.Sprintf("%s: nats config block is required when provider is \"nats\"", prefix))
		return errs
	}

	nats := conn.NATS

	if strings.TrimSpace(nats.Address) == "" {
		errs = append(errs, fmt.Sprintf("%s: address is required", prefix))
	}

	trimmedSubject := strings.TrimSpace(nats.Subject)
	switch {
	case trimmedSubject == "":
		errs = append(errs, fmt.Sprintf("%s: subject is required", prefix))
	case !strings.HasPrefix(trimmedSubject, subjectPrefix):
		errs = append(errs, fmt.Sprintf("%s: subject must start with %q", prefix, subjectPrefix))
	}

	switch nats.AuthType {
	case AuthTypeNone, AuthTypeToken, AuthTypeCredentials, "":
		// valid
	default:
		errs = append(errs, fmt.Sprintf("%s: auth_type must be \"none\", \"token\", or \"credentials\"", prefix))
	}

	if nats.AuthType == AuthTypeToken && strings.TrimSpace(nats.Token) == "" {
		errs = append(errs, fmt.Sprintf("%s: token is required when auth_type is \"token\"", prefix))
	}

	if nats.AuthType == AuthTypeCredentials {
		if strings.TrimSpace(nats.Username) == "" {
			errs = append(errs, fmt.Sprintf("%s: username is required when auth_type is \"credentials\"", prefix))
		}
		if strings.TrimSpace(nats.Password) == "" {
			errs = append(errs, fmt.Sprintf("%s: password is required when auth_type is \"credentials\"", prefix))
		}
	}

	hasCert := nats.ClientCert != ""
	hasKey := nats.ClientKey != ""
	if hasCert != hasKey {
		errs = append(errs, fmt.Sprintf("%s: both client_cert and client_key must be provided together", prefix))
	}

	return errs
}

func validateAzureConnection(conn ConnectionConfig, prefix string) []string {
	var errs []string

	if conn.Azure == nil {
		errs = append(errs, fmt.Sprintf("%s: azure config block is required when provider is \"azure\"", prefix))
		return errs
	}

	az := conn.Azure

	if strings.TrimSpace(az.ConnectionString) == "" {
		errs = append(errs, fmt.Sprintf("%s: connection_string is required", prefix))
	}

	if strings.TrimSpace(az.QueueName) == "" {
		errs = append(errs, fmt.Sprintf("%s: queue_name is required", prefix))
	}

	if conn.FileTransferEnabled && strings.TrimSpace(az.BlobContainerName) == "" {
		errs = append(errs, fmt.Sprintf("%s: blob_container_name is required when file_transfer_enabled is true", prefix))
	}

	return errs
}

func isTestMessage(data []byte) (*model.TestMessage, bool) {
	format := model.DetectFormat(data)
	env, err := model.Unmarshal(data, format)
	if err != nil {
		return nil, false
	}

	if env.Type != model.MessageTypeTest || env.TestMessage == nil {
		return nil, false
	}

	return env.TestMessage, true
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
