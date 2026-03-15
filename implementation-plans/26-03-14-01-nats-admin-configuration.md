# NATS.io Admin Console Configuration for CrossGuard

## Overview

Add an admin console configuration section to CrossGuard that allows admins to configure multiple inbound and outbound NATS.io connections. Each connection specifies a NATS server address, subject/queue name, and security settings. Includes a "Test Connection" button that publishes a well-known test message to verify connectivity.

## Problem Statement

CrossGuard needs to relay messages between Mattermost and NATS.io queues. Before implementing the actual NATS connectivity, we need the admin UI and backend configuration to define which NATS servers and subjects to connect to, with support for multiple independent connections in each direction. Admins also need the ability to verify that a connection works before relying on it.

## Current State

- `plugin.json`: Has a single custom section "crossguard_info" with no settings
- `server/configuration.go`: Empty `configuration struct{}`
- `server/plugin.go`: Plugin struct with OnActivate/OnDeactivate, no ServeHTTP handler
- `webapp/src/components/AdminPanel.tsx`: Placeholder showing plugin name/version
- `webapp/src/index.tsx`: Registers "crossguard_info" custom section via `registerAdminConsoleCustomSection`
- No NATS dependencies exist yet
- No HTTP API endpoints exist yet

## Design Principles

| Pattern | Our Approach | Avoid | Reference |
|---------|-------------|--------|-----------|
| Config storage | JSON string fields parsed to typed structs | Direct slice fields in config struct | Calls plugin ICEServersConfigs pattern |
| Admin UI | `registerAdminConsoleCustomSetting` for each config setting | Custom section with own save logic | Mattermost plugin API |
| Component reuse | One `NATSConnectionSettings` component for both directions | Separate inbound/outbound components | DRY |
| Sensitive data | `type="password"` inputs, server-side only storage | Custom encryption layer | Standard plugin pattern |
| Simplicity | Minimal component count, inline styles, inline types | Deep component hierarchies | Existing AdminPanel.tsx pattern |
| Test connectivity | Dedicated API endpoint, well-known message format | Ad-hoc testing | Standard admin panel pattern |

## Requirements

- [ ] Multiple outbound NATS connections (Mattermost -> NATS)
- [ ] Multiple inbound NATS connections (NATS -> Mattermost)
- [ ] Each connection configures: name, address, subject, TLS, auth type (none/token/credentials), credentials, cert paths
- [ ] Admin console UI to add, edit, remove connections
- [ ] Go backend struct with JSON parsing and validation
- [ ] Integrates with Mattermost's built-in save mechanism
- [ ] "Test Connection" button per connection that publishes a test message to NATS
- [ ] Well-known test message format with unit ID and send timestamp
- [ ] Inbound reader detects test messages, logs to info, and skips normal processing

## Out of Scope

- Full message relay implementation (separate task)
- Message format/transformation configuration
- Channel-to-subject mapping (separate task)
- Environment variable overrides
- Client-side validation (server-side only for now)

## Technical Approach

### Architecture

```
plugin.json
  sections[0]: "crossguard_info" (existing info panel, custom section)
  sections[1]: "NATSConfiguration" (standard section, NOT custom)
    settings[0]: "InboundConnections" (type: "custom", default: "[]")
    settings[1]: "OutboundConnections" (type: "custom", default: "[]")

Go Backend:
  configuration.InboundConnections  -> string (JSON of []NATSConnection)
  configuration.OutboundConnections -> string (JSON of []NATSConnection)
  GetInboundConnections()  -> []NATSConnection, error
  GetOutboundConnections() -> []NATSConnection, error
  validate() -> error (logged as warning, does NOT block activation)

  ServeHTTP -> router
    POST /api/v1/test-connection -> handleTestConnection()

Webapp Registration:
  registerAdminConsoleCustomSection("crossguard_info", AdminPanel)  // existing
  registerAdminConsoleCustomSetting("InboundConnections", NATSConnectionSettings)
  registerAdminConsoleCustomSetting("OutboundConnections", NATSConnectionSettings)
```

### NATSConnection Struct (Go)

```go
type NATSConnection struct {
    Name       string `json:"name"`        // unique friendly label, used as identifier
    Address    string `json:"address"`     // nats://host:4222
    Subject    string `json:"subject"`     // NATS subject/queue name
    TLSEnabled bool   `json:"tls_enabled"`
    AuthType   string `json:"auth_type"`   // "none", "token", "credentials"
    Token      string `json:"token"`
    Username   string `json:"username"`
    Password   string `json:"password"`
    ClientCert string `json:"client_cert"` // file path on server
    ClientKey  string `json:"client_key"`  // file path on server
    CACert     string `json:"ca_cert"`     // file path on server
}

type configuration struct {
    InboundConnections  string `json:"InboundConnections"`
    OutboundConnections string `json:"OutboundConnections"`
}
```

### Go Helper Methods

```go
func (c *configuration) GetInboundConnections() ([]NATSConnection, error) {
    return parseConnections(c.InboundConnections)
}

func (c *configuration) GetOutboundConnections() ([]NATSConnection, error) {
    return parseConnections(c.OutboundConnections)
}

func parseConnections(raw string) ([]NATSConnection, error) {
    if raw == "" || raw == "[]" {
        return nil, nil
    }
    var conns []NATSConnection
    if err := json.Unmarshal([]byte(raw), &conns); err != nil {
        return nil, fmt.Errorf("failed to parse connections: %w", err)
    }
    return conns, nil
}

func (c *configuration) validate() error {
    // Parse both lists, validate each connection:
    // - Name non-empty and unique within each list
    // - Address non-empty
    // - Subject non-empty
    // - If AuthType=="token": Token non-empty
    // - If AuthType=="credentials": Username and Password non-empty
    // - AuthType must be "none", "token", or "credentials"
    // Returns aggregated errors (does NOT block activation)
}
```

### Test Connection Feature

#### Test Message Format

A well-known, well-formatted markdown message used to verify NATS connectivity:

```go
type TestMessage struct {
    Type      string `json:"type"`       // always "crossguard_test"
    UnitID    string `json:"unit_id"`    // UUID identifying this specific test
    Timestamp string `json:"timestamp"`  // RFC3339 send time
}
```

#### Test Message Detection

When the inbound reader processes messages from a NATS queue, it checks if the message is a test message by:
1. Attempting to unmarshal as `TestMessage`
2. Checking if `Type == "crossguard_test"`
3. If it is a test message: log at INFO level with the unit ID and timestamp, then skip normal processing
4. If not: proceed with normal message relay

```go
func isTestMessage(data []byte) (*TestMessage, bool) {
    var msg TestMessage
    if err := json.Unmarshal(data, &msg); err != nil {
        return nil, false
    }
    return &msg, msg.Type == "crossguard_test"
}
```

Info log output:
```
CrossGuard test message received: unit_id=a1b2c3d4 subject=test.inbound sent_at=2026-03-14T15:04:05Z connection=Production Inbound
```

#### API Endpoint

```
POST /api/v1/test-connection
```

Request body: the `NATSConnection` JSON object (the full connection config, sent directly from the admin UI form state so it can test unsaved connections too).

Response:
- `200 OK` with `{"status": "ok", "unit_id": "..."}` on successful publish
- `400 Bad Request` if connection config is invalid
- `502 Bad Gateway` with `{"error": "..."}` if NATS connection or publish fails

The handler:
1. Validates the request is from a system admin (check user session)
2. Parses the `NATSConnection` from the request body
3. Builds NATS connection options from the config (address, TLS, auth)
4. Connects to NATS (with a short timeout, e.g. 5 seconds)
5. Constructs a `TestMessage` with a new UUID and current timestamp
6. Publishes the serialized test message to the configured subject
7. Disconnects and returns the result

#### NATS Client Helper

A shared helper function to build a NATS connection from a `NATSConnection` config:

```go
func connectNATS(conn NATSConnection) (*nats.Conn, error) {
    opts := []nats.Option{
        nats.Name("crossguard-" + conn.Name),
        nats.Timeout(5 * time.Second),
    }

    switch conn.AuthType {
    case "token":
        opts = append(opts, nats.Token(conn.Token))
    case "credentials":
        opts = append(opts, nats.UserInfo(conn.Username, conn.Password))
    }

    if conn.TLSEnabled {
        // Configure TLS with optional client certs and CA
    }

    return nats.Connect(conn.Address, opts...)
}
```

This helper will be reused later when implementing the full message relay.

#### UI "Test Connection" Button

Each saved connection card displays a "Test Connection" button. When clicked:
1. Button shows a spinner/loading state
2. POSTs the connection config to `/plugins/crossguard/api/v1/test-connection`
3. On success: shows green "Connection successful" with the unit ID
4. On failure: shows red error message with the NATS error details
5. Result message auto-dismisses after 10 seconds or on next action

The test button works on both saved and unsaved connection configs (it sends the current form state, not the persisted config).

### NATSConnection Interface (TypeScript, inline in component)

```typescript
interface NATSConnection {
    name: string;        // unique identifier for this connection
    address: string;
    subject: string;
    tls_enabled: boolean;
    auth_type: 'none' | 'token' | 'credentials';
    token: string;
    username: string;
    password: string;
    client_cert: string;
    client_key: string;
    ca_cert: string;
}
```

### Component Design (Simplified)

One component file `NATSConnectionSettings.tsx` handles everything:
- Parses `value` prop (JSON string) into `NATSConnection[]`
- Renders list of connections with summary info (name, address, subject)
- "Add Connection" button at the top
- Each connection has Edit/Delete/Test Connection buttons
- Inline expanded form for add/edit with all fields
- On any change: serialize to JSON, call `onChange(id, json)` and `setSaveNeeded()`
- Enforces unique `name` per connection list (prevents save if duplicate names exist)
- Uses inline styles consistent with existing `AdminPanel.tsx` pattern
- Types defined inline at top of file (no separate types file)
- No separate SCSS file
- Test Connection button sends POST to plugin API and displays result inline

The component distinguishes inbound vs outbound via the `id` prop to show appropriate labels.

### Custom Setting Props (from Mattermost framework)

```typescript
interface CustomSettingProps {
    id: string;           // "InboundConnections" or "OutboundConnections"
    value: string;        // JSON string of NATSConnection[]
    onChange: (id: string, value: string) => void;
    setSaveNeeded: () => void;
    disabled: boolean;
    config: object;
    currentState: object;
    license: object;
    setByEnv: boolean;
}
```

### Empty/Default State Handling

- `plugin.json` sets `"default": "[]"` for both settings
- Go helpers treat both `""` and `"[]"` as empty (return nil slice, no error)
- TypeScript parses with try/catch, falls back to `[]` on any parse error
- UI shows "No connections configured. Click Add Connection to get started." when empty

## Decisions

| Question | Decision | Rationale |
|----------|----------|-----------|
| Config storage | JSON strings in plugin config | Mattermost custom settings store values as strings |
| Connection identity | `name` field must be unique per list | Simple, human-readable identifier for edit/delete; enforced in Go validation and UI |
| One component or two | One reusable component | Same UI, distinguishable by `id` prop |
| Component count | Single file with inline form | Simplest approach for a config form, extract if it grows past ~300 lines |
| Validation | Server-side only (Go) | Sufficient for admin-only page, avoids duplicate logic |
| Validation on error | Log warning, do NOT block activation | Plugin should activate even with partial config |
| Sensitive fields | Stored in plugin config (admin-only access) | Standard Mattermost plugin pattern |
| Styles | Inline styles | Matches existing AdminPanel.tsx, no SCSS build complexity |
| Test message format | JSON with `type: "crossguard_test"` + markdown body | Easy to detect programmatically, human-readable when inspected |
| Test uses saved or unsaved config | Unsaved (current form state) | Allows testing before committing to save |
| NATS dependency | `github.com/nats-io/nats.go` | Official Go client for NATS |

## Files to Modify

| File | Change |
|------|--------|
| `plugin.json` | Add `NATSConfiguration` section with `InboundConnections` and `OutboundConnections` custom settings |
| `server/configuration.go` | Add `NATSConnection` struct, `TestMessage` struct, update `configuration` struct, add parse/validate/test message helpers |
| `server/plugin.go` | Add `ServeHTTP` method with router, initialize router in `OnActivate` |
| `webapp/src/index.tsx` | Register `NATSConnectionSettings` for both custom settings via `registerAdminConsoleCustomSetting` |
| `go.mod` | Add `github.com/nats-io/nats.go` dependency |

## Files to Create

| File | Purpose |
|------|---------|
| `webapp/src/components/NATSConnectionSettings.tsx` | Single component: connection list + inline editor + test button, with types defined inline |
| `server/api.go` | HTTP API handler for `POST /api/v1/test-connection` |
| `server/nats.go` | NATS connection helper (`connectNATS`), test message construction, test message detection (`isTestMessage`) |

## Tasks

1. [ ] Add `github.com/nats-io/nats.go` dependency: `go get github.com/nats-io/nats.go`
2. [ ] Update `plugin.json` with `NATSConfiguration` section containing two custom settings
3. [ ] Run `make apply` to regenerate `server/manifest.go` and `webapp/src/manifest.ts`
4. [ ] Update `server/configuration.go`: add `NATSConnection` struct, `TestMessage` struct, update `configuration`, add `parseConnections`, `GetInboundConnections`, `GetOutboundConnections`, `validate`, and `isTestMessage` methods
5. [ ] Create `server/nats.go`: `connectNATS` helper, `buildTestMessage` function
6. [ ] Create `server/api.go`: `ServeHTTP` with gorilla mux router, `handleTestConnection` endpoint (admin-only)
7. [ ] Update `server/plugin.go`: add router field, initialize router in `OnActivate`
8. [ ] Create `webapp/src/components/NATSConnectionSettings.tsx`: single component with inline types, connection list display, inline add/edit form, test connection button with status display, JSON serialize/deserialize, name uniqueness enforcement
9. [ ] Update `webapp/src/index.tsx`: register `NATSConnectionSettings` for both `InboundConnections` and `OutboundConnections`
10. [ ] Write Go unit tests in `server/configuration_test.go` for `parseConnections`, `validate`, `isTestMessage`, and `buildTestMessage`
11. [ ] Run `make check-style` and `make test`
12. [ ] Run `make deploy` and manually test in Docker environment

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| JSON parse errors from malformed config | Graceful fallback to empty array on both Go and TS sides |
| Sensitive data in plugin config | Standard plugin pattern: admin-only access, password inputs mask values |
| Config struct field name mismatch | Setting keys in plugin.json match Go struct exported field names exactly |
| Concurrent admin edits | Last-write-wins (standard Mattermost plugin limitation), not addressed here |
| NATS server unreachable during test | 5-second timeout, clear error message returned to UI |
| Test message processed as real message | `isTestMessage` check at start of inbound processing, `type` field discriminator |

## Testing Plan

**Unit (Go)**:
- Test `parseConnections` with valid JSON, empty string, `"[]"`, malformed JSON
- Test `validate` with various auth types and missing fields
- Test `buildTestMessage` produces valid JSON with correct `type`, `unit_id`, and `timestamp`
- Test `isTestMessage` correctly identifies test messages and rejects non-test messages

**Manual**: Deploy to Docker, open System Console -> Plugins -> Cross Guard:
- Add/edit/remove inbound and outbound connections
- Verify save/load round-trip (save, reload page, connections persist)
- Test all auth types (none, token, credentials)
- Click "Test Connection" on a saved connection (with a running NATS server)
- Verify test success/failure UI feedback
- Check Go server logs for test message detection on inbound
- Check Go server logs for config validation output

## Acceptance Criteria

- [ ] Admin console shows "NATS Configuration" section with Inbound and Outbound subsections
- [ ] Can add multiple connections with all fields (name, address, subject, TLS, auth)
- [ ] Can edit and remove existing connections
- [ ] Config persists across page reloads (save/load works)
- [ ] Go backend parses JSON strings into typed `[]NATSConnection` structs
- [ ] Validation logs warnings for invalid configs without blocking activation
- [ ] Sensitive fields (token, password) are masked in the UI
- [ ] Empty state shows helpful message
- [ ] "Test Connection" button publishes a test message to NATS and shows success/failure
- [ ] Test message contains unit ID (UUID) and send timestamp in RFC3339 format
- [ ] Test message contains only type, unit_id, and timestamp (no body)
- [ ] Inbound reader detects test messages via `type: "crossguard_test"` and logs at INFO level
- [ ] Test messages are not processed as normal relay messages

## Verification

1. `make check-style` passes
2. `make test` passes (including new configuration and test message tests)
3. `make deploy` succeeds
4. Open http://localhost:8065 -> System Console -> Plugins -> Cross Guard
5. Verify "NATS Configuration" section appears with Inbound and Outbound settings
6. Add an inbound connection: address `nats://localhost:4222`, subject `test.inbound`, auth type "none"
7. Add an outbound connection: address `nats://localhost:4222`, subject `test.outbound`, auth type "token", token "mytoken"
8. Save, reload page, verify both connections persist with correct values
9. Edit a connection, save, verify changes persist
10. Delete a connection, save, verify it is removed
11. Click "Test Connection" on an outbound connection, verify success feedback with unit ID
12. Check server logs: confirm test message logged at INFO with unit ID and timestamp
13. Click "Test Connection" with an invalid address, verify error feedback
