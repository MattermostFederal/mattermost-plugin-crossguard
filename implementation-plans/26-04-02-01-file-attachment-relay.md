# File Attachment Relay via NATS JetStream Object Store

## Context

Cross Guard relays messages between Mattermost servers over NATS, but only text content is supported today. Posts with file attachments lose their files during relay. This plan adds file transfer capability using NATS JetStream Object Store, with per-connection controls for enabling file transfers and filtering by file type.

## Problem Statement

When a user posts a message with file attachments on Server A, only the text arrives on Server B. Files are completely ignored in the current relay pipeline. Admins need the ability to:
1. Toggle file transfer on/off per connection
2. Control which file types are allowed (allowlist) or blocked (denylist) per connection

## Current State

- `PostMessage` (server/model/post_message.go) has no file fields
- `buildPostEnvelope` (server/nats.go:208) only extracts `post.Message`, ignoring `post.FileIds`
- `handleInboundPost` (server/inbound.go:191) creates posts with text only
- No JetStream or Object Store usage anywhere
- `NATSConnection` (server/configuration.go:22) has no file-related config
- nats.go v1.49.0 is already in go.mod (supports JetStream)

### Current Gaps
- No file metadata in any message struct
- No file download/upload logic
- No JetStream initialization
- No file type filtering mechanism

## Design Principles

| Pattern | Our Approach | Avoid | Reference |
|---------|-------------|-------|-----------|
| File transfer | JetStream Object Store with TTL cleanup | Embedding file bytes in NATS messages (1MB limit) | - |
| Relay integrity | Text and files are independent paths. Watcher attaches files via UpdatePost as they arrive. | Coupling text relay to file operations | hooks.go:63-86 |
| Concurrency | Separate semaphore for file ops | Sharing the text relay semaphore (starvation risk) | plugin.go:85 |
| Cleanup | TTL-based auto-expiry (no receiver-delete) | Receiver-side deletion (breaks fan-out) | - |
| Backward compat | Not a concern, all servers upgrade together | N/A | - |

## Requirements

- [ ] Files attached to posts are relayed across NATS connections
- [ ] Per-connection toggle to enable/disable file transfers
- [ ] Per-connection file type filter: allowlist or denylist mode
- [ ] Text relay is never blocked or degraded by file operations

## Out of Scope

- File changes on post edits (Mattermost edits only change text, not attachments)
- End-to-end encryption of file content
- File relay for direct/group messages (only team channels are relayed today)
- Streaming large files (files are buffered via Plugin API `GetFile` which returns `[]byte`)

## Technical Approach

### 1. Model Changes (server/model/post_message.go)

No changes to `PostMessage`. File metadata does not travel in the NATS message envelope. Instead, file metadata is carried as Object Store headers/metadata on the objects themselves, and the receiver discovers files via the Object Store watcher (see section 6).

### 2. Configuration Changes (server/configuration.go)

Add three fields to `NATSConnection`:

```go
type NATSConnection struct {
    // ... existing fields ...
    FileTransferEnabled bool   `json:"file_transfer_enabled"`
    FileFilterMode      string `json:"file_filter_mode"`  // "", "allow", "deny"
    FileFilterTypes     string `json:"file_filter_types"` // ".pdf,.docx,.png"
}
```

Add validation in `validateConnectionList`:
- `FileFilterMode` must be `""`, `"allow"`, or `"deny"`
- If mode is `"allow"` or `"deny"`, `FileFilterTypes` must be non-empty
- Each entry should start with `.` (warn if not)

Add helper method:

```go
func (c NATSConnection) IsFileAllowed(filename string) bool
```

Extracts extension, normalizes to lowercase, checks against parsed filter list.

Max file size: use the Mattermost server's configured limit via `p.API.GetConfig().FileSettings.MaxFileSize` rather than a hardcoded constant. This respects the admin's existing server configuration and avoids a second knob to manage.

### 3. Connection Struct Extensions

Add only file-related fields to the conn structs (not the full NATSConnection):

**server/plugin.go** - update `outboundConn`:
```go
type outboundConn struct {
    nc                  *nats.Conn
    subject             string
    name                string
    fileTransferEnabled bool
    fileFilterMode      string
    fileFilterTypes     string
}
```

**server/inbound.go** - update `inboundConn`:
```go
type inboundConn struct {
    nc                  *nats.Conn
    sub                 *nats.Subscription
    name                string
    fileTransferEnabled bool
    fileFilterMode      string
    fileFilterTypes     string
}
```

Add a file-specific semaphore and an inbound-cycle context to Plugin struct:

```go
type Plugin struct {
    // ... existing fields ...
    fileSem        chan struct{}       // separate semaphore for file operations
    inboundCtx     context.Context    // per-cycle context for inbound watchers
    inboundCancel  context.CancelFunc // cancelled in closeInbound() to stop watchers
}
```

Initialize in OnActivate with capacity 10:
```go
p.fileSem = make(chan struct{}, 10)
```

**Watcher lifecycle**: The `inboundCtx` is a child of `p.ctx`, created fresh in each `connectInbound()` call and cancelled in `closeInbound()`. This ensures that when `reconnectInbound()` is called (e.g., on config change), old watcher goroutines are stopped before new ones start. Without this, `p.ctx` is only cancelled in `OnDeactivate`, so old watchers would leak across config changes.

In `connectInbound`:
```go
p.inboundCtx, p.inboundCancel = context.WithCancel(p.ctx)
```

In `closeInbound` (before closing connections):
```go
if p.inboundCancel != nil {
    p.inboundCancel()
}
```

JetStream context: obtain on-demand via `jetstream.New(nc)` (new `github.com/nats-io/nats.go/jetstream` sub-package) rather than caching. Do NOT use the legacy `nc.JetStream()` API, which returns incompatible types (`nats.JetStreamContext` vs `jetstream.JetStream`). The new sub-package accepts `context.Context` and is the preferred API.

### 4. Object Store Helpers (server/nats.go)

Add Object Store helper to `nats.go` (no separate file needed, one function doesn't justify a new file):

```go
func getOrCreateObjectStore(ctx context.Context, nc *nats.Conn, bucketName string) (jetstream.ObjectStore, error) {
    js, err := jetstream.New(nc)
    if err != nil {
        return nil, fmt.Errorf("failed to create JetStream context: %w", err)
    }
    // CreateOrUpdateObjectStore is idempotent
    return js.CreateOrUpdateObjectStore(ctx, jetstream.ObjectStoreConfig{
        Bucket:  bucketName,
        MaxAge:  time.Hour,
    })
}
```

Bucket config:
- Name: `"crossguard-files"`
- MaxAge: 1 hour (TTL, auto-cleanup of unclaimed objects)

Object key format: `{postID}/{randomID}` (avoids filename encoding issues).

**Object metadata headers** (set by sender, read by receiver watcher):
```go
Headers: nats.Header{
    "X-Post-Id":   []string{post.Id},       // remote post ID for KV lookup
    "X-Conn-Name": []string{connName},       // connection name for KV lookup
    "X-Filename":  []string{fileInfo.Name},   // original filename
}
```

The receiver watcher uses `X-Post-Id` + `X-Conn-Name` to look up the local post ID via the existing KV post mapping (`pm-{connName}-{remotePostID}`), then attaches the file.

**Cleanup strategy**: TTL-only. The receiver does NOT delete objects after download. This is critical because the same message (with the same FileRefs) may be published to multiple outbound connections (fan-out). If the first receiver deletes, subsequent receivers fail. The 1-hour TTL handles cleanup automatically.

### 5. Outbound File Handling (server/hooks.go, server/nats.go)

**No changes to the text relay path.** The existing `buildPostEnvelope` + `publishToOutbound` flow remains unchanged. Text messages continue to flow over core NATS exactly as today.

**File uploads are a separate, independent operation.** After publishing the text message, if the post has files and any outbound connection has file transfer enabled, upload each file to the Object Store with metadata headers.

**hooks.go** - `MessageHasBeenPosted` (add after existing relay call):
```go
// Existing text relay (unchanged)
data, err := buildPostEnvelope(model.MessageTypePost, post, channel, team.Name, user.Username)
p.relayToOutbound(data, connNames, "post:"+post.Id)

// File upload (new, independent of text relay)
if len(post.FileIds) > 0 {
    p.uploadPostFiles(post, connNames)
}
```

**nats.go** - new `uploadPostFiles`:
```go
func (p *Plugin) uploadPostFiles(post *mmModel.Post, conns []store.TeamConnection) {
    // Find outbound connections that have file transfer enabled
    p.outboundMu.RLock()
    var fileConns []outboundConn
    for _, oc := range p.outboundConns {
        if oc.fileTransferEnabled && isOutboundLinked(oc.name, conns) {
            fileConns = append(fileConns, oc)
        }
    }
    p.outboundMu.RUnlock()
    if len(fileConns) == 0 { return }

    fileInfos, appErr := p.API.GetFileInfosForPost(post.Id, false)
    if appErr != nil { log error, return }

    // Get max file size with nil guard (MaxFileSize is *int64, could be nil)
    var maxFileSize int64 = 100 * 1024 * 1024 // default 100MB
    if cfg := p.API.GetConfig(); cfg != nil && cfg.FileSettings.MaxFileSize != nil {
        maxFileSize = *cfg.FileSettings.MaxFileSize
    }

    for _, fi := range fileInfos {
        if fi.Size > maxFileSize { skip, log warning; continue }

        // Download file bytes ONCE, share across all outbound connections
        fileData, appErr := p.API.GetFile(fi.Id)
        if appErr != nil { log error, continue }

        for _, oc := range fileConns {
            if !oc.IsFileAllowed(fi.Name) { skip, log info; continue }

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

                objectStore, err := getOrCreateObjectStore(p.ctx, oc.nc, "crossguard-files")
                if err != nil { log error, return }

                key := post.Id + "/" + mmModel.NewId()
                _, err = objectStore.PutBytes(p.ctx, key, data, jetstream.ObjectMeta{
                    Name: key,
                    Headers: nats.Header{
                        "X-Post-Id":   []string{post.Id},
                        "X-Conn-Name": []string{oc.name},
                        "X-Filename":  []string{fi.Name},
                    },
                })
                if err != nil { log error }
            }(oc, fi, fileData)
        }
    }
}
```

**Key changes from initial draft:**
- File bytes downloaded once per file, shared across connections (avoids N redundant `GetFile` calls for N outbound connections)
- `MaxFileSize` nil guard with 100MB default (prevents panic on nil `*int64`)
- Uses `getOrCreateObjectStore(ctx, nc, ...)` with context parameter (new jetstream API)

**`MessageHasBeenUpdated`**: No file handling. Updates only relay text changes (same as today). Mattermost post edits cannot change file attachments.

### 6. Inbound File Handling (server/inbound.go)

**No changes to `handleInboundPost`.** Text relay is completely untouched. File handling is a separate watcher that runs independently.

**Object Store Watcher approach**: Each inbound connection with `fileTransferEnabled` starts a watcher goroutine on the `"crossguard-files"` Object Store bucket. When a new object appears, the watcher reads its metadata headers, looks up the local post, downloads the file, uploads it to local Mattermost, and attaches it via `UpdatePost`.

**Start watcher in `connectInbound`** (alongside existing subscription):
```go
if conn.FileTransferEnabled {
    p.wg.Add(1)
    go p.watchObjectStore(conn, nc)
}
```

**New `watchObjectStore`** (uses `p.inboundCtx` for clean shutdown on config change):
```go
func (p *Plugin) watchObjectStore(conn NATSConnection, nc *nats.Conn) {
    defer p.wg.Done()

    objectStore, err := getOrCreateObjectStore(p.inboundCtx, nc, "crossguard-files")
    if err != nil {
        p.API.LogError("Failed to open object store for file watcher",
            "conn", conn.Name, "error", err.Error())
        return
    }

    watcher, err := objectStore.Watch(p.inboundCtx, jetstream.UpdatesOnly())
    if err != nil {
        p.API.LogError("Failed to start object store watcher",
            "conn", conn.Name, "error", err.Error())
        return
    }
    defer watcher.Stop()

    p.API.LogInfo("Object store file watcher started", "conn", conn.Name)

    for {
        select {
        case <-p.inboundCtx.Done():
            return
        case info, ok := <-watcher.Updates():
            if !ok { return }
            if info == nil { continue }  // nil = initial values done
            if info.Deleted { continue } // skip delete markers

            p.handleInboundFile(conn, objectStore, info)
        }
    }
}
```

**Critical**: The watcher selects on `p.inboundCtx.Done()` (not `p.ctx.Done()`). This ensures the watcher stops when `closeInbound()` cancels `p.inboundCancel`, preventing goroutine leaks across config changes.

**New `handleInboundFile`**:
```go
func (p *Plugin) handleInboundFile(conn NATSConnection, store jetstream.ObjectStore, info *jetstream.ObjectInfo) {
    // Extract metadata from headers
    connName := info.Headers.Get("X-Conn-Name")
    remotePostID := info.Headers.Get("X-Post-Id")
    filename := info.Headers.Get("X-Filename")

    if connName == "" || remotePostID == "" || filename == "" {
        return // not a crossguard file, or missing metadata
    }

    // Only process files for this connection
    if connName != conn.Name { return }

    // Check file filter
    if !conn.IsFileAllowed(filename) {
        p.API.LogInfo("Inbound file filtered by policy",
            "filename", filename, "conn", conn.Name)
        return
    }

    // Look up local post via existing KV mapping, with retry.
    // The text message (which creates the mapping) usually arrives before the file,
    // but under load they can arrive nearly simultaneously. Retry briefly.
    var localPostID string
    for attempt := range 3 {
        localPostID, err = p.kvstore.GetPostMapping(conn.Name, remotePostID)
        if err == nil && localPostID != "" {
            break
        }
        if attempt < 2 {
            select {
            case <-p.inboundCtx.Done():
                return
            case <-time.After(time.Duration(attempt+1) * time.Second):
            }
        }
    }
    if localPostID == "" {
        p.API.LogWarn("Inbound file: no post mapping found after retries",
            "conn", conn.Name, "remote_post_id", remotePostID)
        return
    }

    // Acquire file semaphore
    select {
    case p.fileSem <- struct{}{}:
        defer func() { <-p.fileSem }()
    default:
        p.API.LogWarn("File semaphore full, skipping inbound file",
            "filename", filename, "conn", conn.Name)
        return
    }

    // Download from Object Store
    data, err := store.GetBytes(p.ctx, info.Name)
    if err != nil {
        p.API.LogError("Failed to download file from object store",
            "key", info.Name, "conn", conn.Name, "error", err.Error())
        return
    }

    // Get the local post to find its channel
    existing, appErr := p.API.GetPost(localPostID)
    if appErr != nil {
        p.API.LogError("Inbound file: failed to get local post",
            "local_post_id", localPostID, "error", appErr.Error())
        return
    }

    // Upload to local Mattermost
    fileInfo, appErr := p.API.UploadFile(data, existing.ChannelId, filename)
    if appErr != nil {
        p.API.LogError("Failed to upload file to Mattermost",
            "filename", filename, "error", appErr.Error())
        return
    }

    // Attach to post via UpdatePost
    existing.FileIds = append(existing.FileIds, fileInfo.Id)
    if _, appErr := p.API.UpdatePost(existing); appErr != nil {
        p.API.LogError("Failed to attach file to post",
            "local_post_id", localPostID, "file_id", fileInfo.Id, "error", appErr.Error())
    }
}
```

**Key benefits of the watcher approach**:
- No race condition: the watcher only fires when an object is fully written to the store
- No retry/backoff logic needed: files are processed as they arrive
- No changes to PostMessage or the text relay path
- No per-file goroutines: single watcher loop per connection processes files sequentially
- Clean separation: text relay and file relay are completely independent systems
- `UpdatePost` with `FileIds` is supported by the Mattermost Plugin API (SafeUpdate defaults to false)
- Watcher respects `p.ctx` for clean shutdown via `OnDeactivate`

**Post mapping timing**: The watcher depends on the KV post mapping (`pm-{connName}-{remotePostID}`) already existing when the file object arrives. Since the text message arrives and is processed first (creating the mapping), and file upload is slower than a core NATS publish, this ordering is naturally satisfied. If a file arrives before its post mapping exists (e.g., under heavy load), `handleInboundFile` retries the lookup 3 times with 1s/2s backoff before giving up. If still not found after retries, the file is skipped and cleaned up by TTL.

**Helper to get inbound config**:
```go
func (p *Plugin) getInboundConn(connName string) *inboundConn
```

### 7. Frontend Changes (webapp/src/components/NATSConnectionSettings.tsx)

**Update `NATSConnection` interface**:
```typescript
interface NATSConnection {
    // ... existing fields ...
    file_transfer_enabled: boolean;
    file_filter_mode: '' | 'allow' | 'deny';
    file_filter_types: string;
}
```

**Update `emptyConnection`**:
```typescript
file_transfer_enabled: false,
file_filter_mode: '',
file_filter_types: '',
```

**Add "File Transfer" section in the form**, after the Security section:
- Checkbox: "Enable File Transfer"
- When enabled, show:
  - Dropdown: File Filter Mode ("None", "Allow only these types", "Block these types")
  - When mode is allow or deny, show:
    - Text input: File Types (placeholder: ".pdf,.docx,.png,.jpg")

**No plugin.json changes needed**: The connections are stored as JSON strings in custom settings. New fields serialize automatically.

## Decisions

| Question | Decision | Rationale |
|----------|----------|-----------|
| Cleanup strategy | TTL-only (1 hour), no receiver-delete | Receiver-delete breaks fan-out (multiple receivers) |
| Semaphore | Separate file semaphore (10 slots) | Prevents file ops from starving text relay |
| Object key format | `{postID}/{randomID}` | Avoids filename encoding issues |
| JetStream context | On-demand via jetstream.New(nc) (new sub-package) | Simpler, correct on reconnect, accepts context.Context |
| File metadata transport | Object Store headers (X-Post-Id, X-Conn-Name, X-Filename) | No changes to PostMessage needed, clean separation |
| Post updates with files | Not supported | MM edits don't change attachments |
| Filter naming | "allow"/"deny" (not whitelist/blacklist) | Modern terminology |
| Shared NATS requirement | Document, log warning at startup | Cannot enforce programmatically |
| Partial file failure | Each file attached independently as it arrives | Text always goes through |
| Inbound file discovery | Object Store Watch (watcher loop per inbound connection) | No race condition, no retry/backoff needed, files processed as they arrive |
| Watcher lifecycle | Per-cycle inboundCtx cancelled in closeInbound() | Prevents goroutine leaks across config changes |
| Post mapping timing | Retry 3 times with 1s/2s backoff in handleInboundFile | Handles near-simultaneous text/file arrival under load |
| File download dedup | Download file bytes once, share across outbound connections | Avoids N redundant GetFile calls for N connections |
| MaxFileSize safety | Nil guard with 100MB default | Prevents panic on nil *int64 in server config |
| Object Store helpers | In nats.go (not a separate file) | One function, all NATS ops in one file |

## Files to Modify

| File | Change |
|------|--------|
| `server/configuration.go` | Add file config fields to NATSConnection, validation, IsFileAllowed helper |
| `server/plugin.go` | Add fileSem, inboundCtx/inboundCancel to Plugin struct, update outboundConn struct, init in OnActivate |
| `server/hooks.go` | Add uploadPostFiles call after existing text relay in MessageHasBeenPosted |
| `server/nats.go` | Add uploadPostFiles, getOrCreateObjectStore (independent of text relay path) |
| `server/inbound.go` | Update inboundConn struct, create/cancel inboundCtx in connect/closeInbound, add Object Store watcher (watchObjectStore, handleInboundFile with post mapping retry) |
| `webapp/src/components/NATSConnectionSettings.tsx` | Add File Transfer section to connection form |

## Tasks

1. [ ] Add file config fields to NATSConnection with validation and IsFileAllowed helper (server/configuration.go)
2. [ ] Add getOrCreateObjectStore helper to server/nats.go (using jetstream.New(nc), not legacy API)
3. [ ] Update outboundConn/inboundConn structs with file config fields (server/plugin.go, server/inbound.go)
4. [ ] Add fileSem, inboundCtx/inboundCancel to Plugin struct, initialize in OnActivate (server/plugin.go)
5. [ ] Wire inboundCtx lifecycle: create in connectInbound, cancel in closeInbound (server/inbound.go)
6. [ ] Implement outbound file upload: uploadPostFiles in nats.go (download once, share across conns), call from MessageHasBeenPosted in hooks.go
7. [ ] Implement Object Store watcher: watchObjectStore (using inboundCtx), handleInboundFile with post mapping retry (server/inbound.go)
8. [ ] Start watcher in connectInbound for file-enabled connections (server/inbound.go)
9. [ ] Add File Transfer section to NATSConnectionSettings.tsx (webapp/src/)
10. [ ] Write unit tests for IsFileAllowed, getOrCreateObjectStore, post mapping retry logic
11. [ ] Integration test: deploy to docker environment, post with file attachment, verify relay

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| NATS server does not have JetStream enabled | Log clear warning at connection startup when FileTransferEnabled=true. Text relay still works. |
| Outbound/inbound use different NATS clusters | Log warning if Object Store bucket creation fails. Document shared NATS as requirement. |
| Memory pressure from buffering large files | Server's MaxFileSize config limits file size. File semaphore of 10 caps concurrent file ops. |
| Semaphore starvation of text relay | Separate semaphore for file ops (fileSem=10 vs relaySem=50) |
| Object Store TTL expires before receiver processes | 1-hour TTL is generous. If receiver is down >1 hour, text messages are also lost (core NATS is fire-and-forget). |
| File arrives before post mapping exists in KV | Watcher retries post mapping lookup 3 times with 1s/2s backoff. If still missing, logs warning and skips. File expires via TTL. |
| Fan-out: same FileRef published to multiple receivers | TTL-only cleanup (no receiver-delete). All receivers can download independently. |

## Testing Plan

**Unit**: IsFileAllowed with allow/deny/none modes and edge cases (no extension, mixed case). PostMessage round-trip with Files field.

**Integration**: Deploy to docker env (`make deploy`). Post a message with file attachment on Server A. Verify file appears on Server B. Test with file transfer disabled (only text relayed). Test with allowlist filter (blocked types not relayed).

## Acceptance Criteria

- [ ] File attachments on posts are relayed to the receiving Mattermost server
- [ ] Files appear as proper attachments on the relayed post (not inline links)
- [ ] Admin can enable/disable file transfer per connection
- [ ] Admin can set allow or deny filter for file extensions per connection
- [ ] Text relay continues to work when file transfer is disabled
- [ ] Text relay is not degraded when file operations are slow or fail

## Checklist

- [ ] **Diagnostics**: File transfer failures should be logged at appropriate levels (Error for failures, Info for filtered files, Warn for skipped large files)
- [ ] **Slash command**: No new slash command needed for file transfer (it is automatic based on connection config)
