# Azure Queue Storage Provider Abstraction

## Context

Cross Guard currently uses NATS exclusively for cross-domain message relay and file transfer (via JetStream Object Store). We need to support Azure Queue Storage for messaging and Azure Blob Storage for file transfer, allowing deployments to choose their transport per connection. This enables Cross Guard to operate in Azure-native environments without requiring NATS infrastructure.

## Problem Statement

All messaging code is tightly coupled to NATS types (`*nats.Conn`, `*nats.Subscription`, `nats.MsgHandler`, JetStream Object Store). Configuration, validation, admin UI, and test-connection API all assume NATS. Adding Azure support requires abstracting the transport layer. No backward compatibility with existing config format is required; existing deployments will reconfigure connections after upgrading.

## Current State

- `outboundConn` holds `*nats.Conn` directly (`server/plugin.go:22-30`)
- `inboundConn` holds `*nats.Conn` + `*nats.Subscription` (`server/inbound.go:15-22`)
- File transfer uses JetStream Object Store (`server/nats.go:347-446`, `server/inbound.go:402-533`)
- Config struct is `NATSConnection` with NATS-specific fields (`server/configuration.go:28-46`)
- Publish retry loop lives in `publishToOutbound` (`server/nats.go:267-323`)
- Admin UI: `webapp/src/components/NATSConnectionSettings.tsx`
- Plugin settings schema: `plugin.json` with "NATSConfiguration" section

### Current Gaps
- No queue provider abstraction exists
- No support for HTTP-based queue transports
- No support for poll-based message consumption
- File transfer is NATS JetStream-specific

## Design Principles

| Pattern | Our Approach | Avoid | Reference |
|---------|-------------|-------|-----------|
| Interface granularity | Single `QueueProvider` interface | Separate interfaces per concern | Simplicity reviewer: "message and file operations are always paired per connection" |
| Package structure | Flat in `server/` package | Sub-packages per provider | Matches existing codebase structure (`server/model/`, `server/store/` only) |
| Config shape | Nested sub-structs | Flat struct with azure_ prefixes | User preference; clean break, no backward compat needed |
| Provider construction | Simple if/else in connect methods | Factory pattern | Only 2 providers; factory is premature |
| Retry ownership | Inside provider implementations | Caller-side retry loops | Each transport has different retry semantics |
| Polling model | Azure Queue sleep-based polling (5s interval) | Busy-loop short polling | Azure Queue has no native long-poll; poll then sleep in a select loop with context cancellation |
| Handler errors | Return error from handler for ack/nack | Fire-and-forget void handler | Azure needs ack semantics for message deletion |
| Auth | Connection string only | Managed Identity (deferred) | Connection string is sufficient for initial release; Managed Identity adds azidentity dependency and conditional auth path |

## Requirements

- [ ] Abstract queue transport behind a provider interface
- [ ] Implement NATS provider by extracting existing code
- [ ] Implement Azure Queue Storage provider (inbound + outbound)
- [ ] Implement Azure Blob Storage provider for file transfer
- [ ] Support connection string auth for Azure
- [ ] Provider-aware configuration validation
- [ ] Updated admin UI with provider selector
- [ ] Test connection endpoint works for both providers
- [ ] Handle Azure Queue 64KB message size limit

## Out of Scope

- Azure Service Bus (FIFO queues) support
- Azure Managed Identity auth (will be added in a future iteration)
- Cross-provider file relay (e.g., upload via NATS, download via Azure)
- Azure Event Grid for blob change notifications (use polling instead)
- Configurable poll intervals in admin UI (hardcode sensible defaults)

## Technical Approach

### 1. Provider Interface (`server/provider.go`)

Single interface combining message and file operations. Handler returns error to support Azure ack/nack semantics.

```go
package main

type QueueProvider interface {
    // Publish sends a message. Includes internal retries appropriate to transport.
    // A returned error is a final failure.
    Publish(ctx context.Context, data []byte) error

    // Subscribe starts delivering messages to the handler.
    // NATS: push-based subscription. Azure: long-polling goroutine.
    // Handler returning nil = message processed (Azure deletes it).
    // Handler returning error = message not processed (Azure lets visibility timeout expire).
    Subscribe(ctx context.Context, handler func(data []byte) error) error

    // UploadFile uploads a file with metadata. No-op if file transfer not supported.
    UploadFile(ctx context.Context, key string, data []byte, headers map[string]string) error

    // WatchFiles watches for new files and calls handler.
    // Handler returning nil = file processed (provider may clean up).
    // Handler returning error = file not processed (retry later).
    WatchFiles(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error

    // MaxMessageSize returns the provider's message size limit in bytes.
    // Returns 0 for no limit. Caller checks before Publish.
    MaxMessageSize() int

    // Close gracefully shuts down. In-flight operations complete or are abandoned.
    Close() error
}
```

No separate `IsConnected()` on the interface. Instead, each provider tracks a `lastError` field internally. The `outboundConn` struct holds a `healthy bool` flag updated after each `Publish()` call (true on success, false on error). `publishToOutbound` skips connections where `healthy == false` with a periodic recheck (every 30s, attempt one publish to see if the connection recovered). This avoids blocking on HTTP timeouts to dead Azure connections while still allowing recovery.

No Logger interface. Pass `plugin.API` directly to provider constructors (matches existing codebase pattern).

### 2. Configuration Changes (`server/configuration.go`)

Rename `NATSConnection` to `ConnectionConfig` with nested sub-structs. No backward compatibility with the old flat format is needed; existing deployments will reconfigure their connections after upgrading.

```go
type ConnectionConfig struct {
    Name     string `json:"name"`
    Provider string `json:"provider"` // "nats" or "azure"

    // Common fields
    FileTransferEnabled bool   `json:"file_transfer_enabled"`
    FileFilterMode      string `json:"file_filter_mode"`
    FileFilterTypes     string `json:"file_filter_types"`
    MessageFormat       string `json:"message_format"`

    // Provider-specific (exactly one must be set, matching Provider)
    NATS  *NATSProviderConfig  `json:"nats,omitempty"`
    Azure *AzureProviderConfig `json:"azure,omitempty"`
}

type NATSProviderConfig struct {
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

type AzureProviderConfig struct {
    ConnectionString  string `json:"connection_string"`
    QueueName         string `json:"queue_name"`
    BlobContainerName string `json:"blob_container_name"`
}
```

**Validation**: `validateConnectionList` requires `Provider` to be set ("nats" or "azure") and dispatches to provider-specific validators:
- `validateNATSConnection`: requires `NATS` sub-struct, validates address, subject (prefix), TLS, auth (existing checks)
- `validateAzureConnection`: requires `Azure` sub-struct, validates connection_string required, queue_name required, blob_container_name required if file_transfer_enabled

### 3. Refactor Connection Structs (`server/plugin.go`)

```go
type outboundConn struct {
    provider            QueueProvider
    name                string
    fileTransferEnabled bool
    fileFilterMode      string
    fileFilterTypes     string
    messageFormat       string
    healthy             bool      // updated after each Publish; false skips connection with periodic 30s recheck
    lastCheckTime       time.Time // when healthy was last evaluated
}

type inboundConn struct {
    provider            QueueProvider
    name                string
    fileTransferEnabled bool
    fileFilterMode      string
    fileFilterTypes     string
}
```

Remove all `*nats.Conn` and `*nats.Subscription` fields. Remove `nats.go` import from `plugin.go`.

### 4. NATS Provider (`server/nats_provider.go`)

Extract from existing `server/nats.go`:

```go
type natsProvider struct {
    nc      *nats.Conn
    subject string
    api     plugin.API
}
```

- `Publish`: includes the retry loop currently in `publishToOutbound` (3 retries, exponential backoff)
- `Subscribe`: wraps `nc.Subscribe(subject, handler)`, adapting `nats.MsgHandler` to `func([]byte) error` (NATS ignores the error return since delivery is fire-and-forget)
- `UploadFile`: JetStream Object Store put (extracted from `uploadPostFiles`)
- `WatchFiles`: JetStream Object Store watcher (extracted from `watchObjectStore`)
- `MaxMessageSize`: returns 0 (no practical limit)
- `Close`: drain + close the NATS connection

Constructor: `func newNATSProvider(cfg NATSProviderConfig, api plugin.API, direction string) (QueueProvider, error)`

Moves `connectNATS`, `connectNATSPersistent`, TLS setup, auth setup into this file.

### 5. Azure Queue Storage Provider (`server/azure_provider.go`)

```go
type azureProvider struct {
    queueClient *azqueue.QueueClient
    blobClient  *azblob.ContainerClient
    api         plugin.API
}
```

- `Publish`: Base64-encode data, call `EnqueueMessage`. Check `MaxMessageSize()` is caller's responsibility. Internal retry via Azure SDK's built-in retry policy.
- `Subscribe`: Start a polling goroutine using a `select` loop with context cancellation. Each iteration: `DequeueMessages(batchSize=32, visibilityTimeout=5min)`. For each message, call handler. If handler returns nil, `DeleteMessage`. If handler returns error, message becomes visible again after timeout. When queue is empty, sleep 5s via `select { case <-time.After(5*time.Second): case <-ctx.Done(): return }`. Azure Queue Storage has no native long-poll; this sleep-based approach provides near-real-time delivery without busy-looping.
- `UploadFile`: Upload blob to container with metadata headers. Blob name: `{connection-name}/{post-id}/{unique-id}`.
- `WatchFiles`: Polling goroutine listing blobs newer than last-seen marker. Process each blob, delete after successful handler invocation. Poll every 30 seconds using `select { case <-time.After(30*time.Second): case <-ctx.Done(): return }` for clean shutdown.
- `MaxMessageSize`: returns 48000 (48KB, safe limit before Base64 encoding to 64KB)
- `Close`: cancel the context (which unblocks the `select` in Subscribe/WatchFiles immediately), then wait for in-flight operations to finish. The `select`-based sleep ensures Close does not block for the full poll interval.

Constructor: `func newAzureProvider(cfg AzureProviderConfig, api plugin.API) (QueueProvider, error)`

Auth: Use `ConnectionString` to create queue and blob clients. Managed Identity support will be added in a future iteration.

### 6. Refactor `server/nats.go` -> `server/connections.go`

This file becomes the plugin-level connection orchestration (provider-agnostic):

- `connectOutbound()`: Parse config, create provider via if/else on `cfg.Provider`, store in `outboundConn.provider`
- `closeOutbound()`: Call `oc.provider.Close()` for each connection
- `publishToOutbound()`: Check `oc.provider.MaxMessageSize()`, marshal envelope, call `oc.provider.Publish(ctx, data)`. No caller-side retry loop (retries are inside provider).
- `uploadPostFiles()`: Call `oc.provider.UploadFile()` for each file per connection
- Remove all NATS-specific code (moves to `nats_provider.go`)
- Keep envelope builders (`buildPostEnvelope`, etc.) here since they are transport-agnostic

### 7. Refactor `server/inbound.go`

- `connectInbound()`: Create provider via if/else, call `provider.Subscribe(ctx, handler)`. Handler signature changes from `nats.MsgHandler` to `func(data []byte) error`.
- `handleInboundMessage()`: Returns `func(data []byte) error` instead of `nats.MsgHandler`. Body is nearly identical (unmarshal, dispatch). Returns error for transient failures (Azure retries via visibility timeout) or nil for permanent failures and successes (Azure deletes the message). Error classification:

  | Failure | Return | Rationale |
  |---------|--------|-----------|
  | Unmarshal/decode error | `nil` (discard) | Bad data will never succeed on retry |
  | Unknown team/channel | `nil` (discard) | Permanent config mismatch, log warning |
  | User resolution failed | `nil` (discard) | Permanent, user does not exist on this server |
  | API CreatePost failed (5xx) | `error` (retry) | Mattermost server may be temporarily down |
  | API CreatePost failed (4xx) | `nil` (discard) | Bad request data will not succeed on retry |
  | KV store error | `error` (retry) | Transient storage issue |

- File watching: Call `provider.WatchFiles(ctx, handler)` instead of `watchObjectStore`.
- `handleInboundFile`: Becomes the WatchFiles handler callback. Signature changes to accept `(key string, data []byte, headers map[string]string) error`. Same transient/permanent error classification applies.
- `closeInbound()`: Call `ic.provider.Close()` for each connection.

### 8. Refactor `server/api.go` Test Connection

`handleTestConnection` accepts `ConnectionConfig` instead of `NATSConnection`. Dispatches to provider-specific test logic:

- NATS: existing connect + publish/subscribe test (extracted to `testNATSConnection`)
- Azure: create queue client, enqueue test message, dequeue it, delete it (`testAzureConnection`)

### 9. Refactor `server/service.go`

- Rename `RedactedNATSConnection` to `RedactedConnection`, add `Provider` field
- Rename `getNATSConnectionMap` to `getConnectionMap`
- Redaction logic: for NATS, redact token/password/certs. For Azure, redact connection_string.
- Update status endpoints to show provider type per connection

### 10. Message Size Handling

Before calling `Publish`, check `MaxMessageSize()`. If the serialized envelope exceeds the limit, truncate the message text to fit within the limit and log a warning. Mattermost's post character limit (~16,383 chars) fits comfortably within the 48KB Azure Queue limit after Base64 encoding, so truncation should be extremely rare and only occur with unusually large envelope metadata.

```go
maxSize := oc.provider.MaxMessageSize()
if maxSize > 0 && len(data) > maxSize {
    env.MessageText = truncateToFit(env, format, maxSize)
    p.API.LogWarn("Message truncated to fit provider size limit",
        "connection", oc.name, "maxSize", maxSize, "originalSize", len(data))
    data, _ = model.Marshal(env, format)
}
```

**Truncation logic** (`truncateToFit` in `server/connections.go`):
1. Marshal the envelope with an empty `MessageText` to measure the overhead (envelope metadata, XML/JSON tags, other fields).
2. Calculate `availableTextBytes = maxSize - overhead - safetyMargin(500 bytes)`.
3. Truncate `MessageText` at a UTF-8 safe boundary to `availableTextBytes`.
4. Append `"\n[message truncated]"` indicator.

**Why not split into multiple messages**: Azure Queue Storage does not guarantee FIFO ordering. Split chunks could arrive out of order, causing reply posts to reference a parent that does not yet exist. Truncation is simpler and avoids this data integrity risk. Message splitting can be revisited if real-world truncation events are observed (add sequence numbers and reassembly at that point).

### 11. Azure Inbound Idempotency

Azure Queue has at-least-once delivery. To prevent duplicate posts, check post mapping before creating:

In `handleInboundPost`, before `p.API.CreatePost()`:
```go
existingLocalID, _ := p.kvstore.GetPostMapping(connName, postMsg.PostID)
if existingLocalID != "" {
    return nil // already processed, skip
}
```

This is already partially in place for updates/deletes (they look up mappings). Adding it to post creation handles Azure redelivery.

### 12. Update Admin UI (`webapp/src/components/ConnectionSettings.tsx`)

Rename from `NATSConnectionSettings.tsx`. Changes:

- Add "Provider" dropdown (NATS / Azure Queue Storage) at the top of each connection card
- Conditionally render fields based on provider:
  - NATS: Address, Subject, TLS toggle + cert fields, Auth type + credentials
  - Azure: Connection String, Queue Name, Blob Container Name (when file transfer enabled)
- Common fields always shown: Name, Message Format, File Transfer toggle + filter
- Update card header to show provider badge
- Update `emptyConnection` to include `provider: 'nats'`

### 13. Update `plugin.json`

- Rename "NATSConfiguration" section display name to "Connection Configuration"
- Update setting IDs and help text to mention both NATS and Azure

### 14. Add Azure SDK Dependencies

```
go get github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue
go get github.com/Azure/azure-sdk-for-go/sdk/storage/azblob
```

## Decisions

| Question | Decision | Rationale |
|----------|----------|-----------|
| Interface count | Single QueueProvider | Message and file ops are always paired per connection; 4 interfaces is over-engineering |
| Retry ownership | Inside provider | NATS retries via TCP reconnect; Azure retries via HTTP SDK. Different semantics per transport |
| IsConnected method | Removed | Meaningless for HTTP-based providers; errors surface through Publish |
| Polling approach | Azure long-polling (20s) | Near-real-time without busy-loop; Azure SDK supports this natively |
| Message size limit | Truncate with warning log | Azure Queue is not FIFO, so split chunks can arrive out of order. Truncation is safe; splitting can be revisited with sequence numbers if needed |
| Duplicate prevention | Idempotency check via post mapping | Azure has at-least-once delivery; check before CreatePost |
| Blob cleanup | Delete after successful download | Matches NATS Object Store TTL concept; avoids external lifecycle policy dependency |
| Config migration | Clean break, no backward compat | Existing deployments reconfigure after upgrade; simpler code |

## Files to Modify

| File | Change |
|------|--------|
| `server/provider.go` | **NEW** - QueueProvider interface definition |
| `server/nats_provider.go` | **NEW** - NATS implementation extracted from nats.go |
| `server/azure_provider.go` | **NEW** - Azure Queue Storage + Blob Storage implementation |
| `server/nats.go` | **RENAME** to `server/connections.go`, remove NATS-specific code, keep envelope builders |
| `server/plugin.go` | Refactor outboundConn/inboundConn to use QueueProvider; remove nats import |
| `server/inbound.go` | Refactor to use QueueProvider; change handler signatures |
| `server/configuration.go` | Rename NATSConnection to ConnectionConfig; add nested structs; provider-aware validation |
| `server/api.go` | Refactor test-connection to dispatch by provider |
| `server/service.go` | Rename RedactedNATSConnection; add Provider field; update helpers |
| `server/hooks.go` | Minimal changes (calls publishToOutbound which is now provider-agnostic) |
| `webapp/src/components/NATSConnectionSettings.tsx` | **RENAME** to ConnectionSettings.tsx; add provider selector; conditional fields |
| `webapp/src/index.tsx` | Update component registration to new name |
| `plugin.json` | Update section display names and help text |
| `go.mod` | Add Azure SDK dependencies |

## Tasks

### Phase 1: Provider Abstraction + NATS Extraction (no behavioral change)
1. [ ] Create `server/provider.go` with QueueProvider interface
2. [ ] Create `server/nats_provider.go` extracting NATS logic from `server/nats.go`
3. [ ] Refactor `outboundConn`/`inboundConn` structs to use QueueProvider
4. [ ] Rename `server/nats.go` to `server/connections.go`, make provider-agnostic
5. [ ] Refactor `server/inbound.go` to use QueueProvider (handler signature change)
6. [ ] Update `server/configuration.go` (rename struct, nested config, provider-aware validation)
7. [ ] Update `server/api.go` test connection to work with new config types
8. [ ] Update `server/service.go` (rename types, add Provider field)
9. [ ] Run `make check-style && make test` to verify no regressions

### Phase 2: Azure Queue Storage Implementation
10. [ ] Add Azure SDK dependencies to `go.mod`
11. [ ] Create `server/azure_provider.go` with outbound (Publish) support
12. [ ] Add Azure inbound (Subscribe) with long-polling
13. [ ] Add Azure Blob Storage file upload (UploadFile)
14. [ ] Add Azure Blob Storage file watching (WatchFiles)
15. [ ] Add idempotency check in `handleInboundPost` for at-least-once delivery
16. [ ] Add message truncation logic (`truncateToFit`) for oversized posts in `connections.go`
17. [ ] Add Azure-specific config validation
18. [ ] Write unit tests for Azure provider (mock Azure SDK)
19. [ ] Run `make check-style && make test`

### Phase 3: Admin UI + Configuration
20. [ ] Rename and refactor `NATSConnectionSettings.tsx` to `ConnectionSettings.tsx`
21. [ ] Add provider selector dropdown
22. [ ] Add conditional field rendering (NATS vs Azure)
23. [ ] Update `plugin.json` section names and help text
24. [ ] Update `webapp/src/index.tsx` component registration
25. [ ] Update test-connection API call in webapp to include provider

### Phase 4: Integration Testing
26. [ ] Test NATS connections work end-to-end with new nested config format
27. [ ] Test Azure Queue outbound with Azurite (local emulator)
28. [ ] Test Azure Queue inbound polling with Azurite
29. [ ] Test Azure Blob file transfer with Azurite
30. [ ] Test mixed providers (NATS outbound + Azure inbound on different connections)
31. [ ] Test config change/reconnect lifecycle for both providers

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Azure 64KB message limit hit by long posts | Split oversized posts into multiple envelopes; first as original post, rest as threaded replies |
| Azure at-least-once delivery causes duplicate posts | Idempotency check via post mapping before CreatePost |
| Azure polling latency vs NATS push | Long-polling (20s) provides near-real-time; document the trade-off |
| Azure blob arrival before post mapping exists | Existing retry mechanism (3 attempts, 1s delay) handles this; increase to 5 attempts for Azure |
| Reconnect during in-flight Azure poll | Provider.Close() cancels context; in-flight dequeued messages reappear after visibility timeout |
| Azure SDK dependency size | azqueue/azblob SDKs are lightweight; only import needed packages |
| Azure rate limiting (429) | Azure SDK has built-in retry with Retry-After header support |
| HA blob watching race (multiple instances) | In HA deployments, multiple plugin instances may poll the same blob container and download the same file concurrently. Post-level idempotency prevents duplicate posts, but duplicate downloads waste bandwidth. Known limitation for initial release; can be mitigated later with blob lease acquisition before processing |
| Dead Azure connection blocks publish loop | `outboundConn.healthy` flag skips known-dead connections with periodic 30s recheck, avoiding HTTP timeout blocking on every message |

## Testing Plan

**Unit**: Provider interface conformance tests for both NATS and Azure. Provider-aware validation tests. Message size limit checks.

**Integration**: Use Azurite (Azure Storage Emulator) for Azure provider tests. Docker-compose can include an Azurite container alongside NATS.

**E2E**: Full relay flow with both NATS and Azure connections configured. Post, update, delete, reaction, and file transfer across each provider.

## Acceptance Criteria

- [ ] NATS deployments work with the new nested config format
- [ ] New deployments can configure Azure Queue Storage connections via admin UI
- [ ] Messages relay successfully through Azure Queue Storage (inbound + outbound)
- [ ] Files relay successfully through Azure Blob Storage
- [ ] Connection string auth works for Azure
- [ ] Test connection button works for both NATS and Azure connections
- [ ] Mixed provider deployments work (some connections NATS, some Azure)
- [ ] `make check-style` and `make test` pass

## Checklist

- [ ] **Diagnostics**: Azure provider errors should post to the diagnostics channel
- [ ] **Slash command**: No new slash commands needed; existing `/crossguard` commands work with any provider
