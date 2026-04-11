# Azure Blob Storage Message Relay Provider + Missing Message Queue

## Overview

Add a third provider type `azure-blob` that uses Azure Blob Storage for message relay via batched .jsonl files, plus a missing message retry queue for out-of-order message handling across all providers.

## Problem Statement

The plugin currently supports NATS and Azure Queue Storage for message relay. Some environments require Azure Blob Storage as the transport mechanism, where messages are batched into .jsonl files, uploaded on a configurable interval, and polled on the inbound side. Additionally, reactions, deletes, and updates can arrive before the original post they reference, causing them to be silently dropped.

## Current State

- `QueueProvider` interface in `server/provider.go` with `Publish`, `Subscribe`, `UploadFile`, `WatchFiles`, `MaxMessageSize`, `Close`
- Two providers: NATS (`server/nats_provider.go`) and Azure Queue (`server/azure_provider.go`)
- Provider factory in `server/connections.go:326-341`
- Configuration in `server/configuration.go:42-77` with `ConnectionConfig`, `NATSProviderConfig`, `AzureProviderConfig`
- azblob v1.6.4 already in go.mod
- Idempotency via `GetPostMapping` in `server/inbound.go:271-278` prevents duplicate post creation
- `handleInboundMessage` dispatches processing asynchronously via `p.wg.Go()` and always returns nil (`server/inbound.go:122-191`)

### Current Gaps

- No blob-based message relay (only queue-based)
- Reactions/deletes/updates to unknown posts are silently dropped (`inbound.go:321-323`, `345-346`, `374-375`)
- No retry mechanism for out-of-order dependent messages

## Design Principles

| Pattern | Our Approach | Avoid | Reference |
|---------|-------------|-------|-----------|
| Batching | WAL file with configurable flush interval | Individual blob per message (too many API calls) | User requirement |
| HA inbound | KV store CAS lock per blob with age-based stale recovery | Blob leases (Azure-specific), copy-to-processed (extra API calls), pure idempotency (wasted work) | `store/client.go:481-536` |
| File uploads | Deferred until WAL flush confirms, via companion .files.json | Immediate upload (files arrive before posts) | Design review finding |
| Retry queue | In-memory per node, inside async goroutine | Sentinel errors from handler (impossible with async dispatch) | Design review finding |
| Config intervals | Flush interval configurable, poll/retry as constants | Making everything configurable | Existing pattern: `azure_provider.go:23-25` |

## Phase Strategy

| Phase | Focus | Value |
|-------|-------|-------|
| **Phase 1** | Azure Blob provider + retry queue | **Core functionality** |
| **Phase 2** | Compression, deduplication within batches | Optimization |

### Phase 1 Scope (this plan)

## Requirements

- [ ] New `azure-blob` provider type implementing `QueueProvider`
- [ ] Outbound: batch messages into local .jsonl WAL file, flush/upload on configurable interval (default 60s)
- [ ] Inbound: poll blob container, download .jsonl files, process each line, delete blob
- [ ] WAL crash recovery: on startup, scan for leftover WAL files and upload them
- [ ] HA-safe: unique blob names per node, idempotency prevents duplicate processing
- [ ] Missing message retry queue for reactions/deletes/updates referencing unknown posts (default 20s delay)
- [ ] File transfer deferred until WAL flush confirms, ensuring posts arrive before their attachments

## Out of Scope

- NOT adding compression to .jsonl files (Phase 2)
- NOT adding deduplication within batches (Phase 2)
- NOT persisting retry queue to KV store (in-memory is sufficient for best-effort)

## Technical Approach

### Part 1: Azure Blob Provider (`server/azure_blob_provider.go`)

**New struct:**
```go
type azureBlobProvider struct {
    containerClient *container.Client
    api             plugin.API
    cfg             AzureBlobProviderConfig
    nodeID          string
    connName        string

    // WAL (outbound only)
    walMu      sync.Mutex
    walFile    *os.File
    walPath    string
    walSeq     int64
    walDir     string
    flushTimer *time.Ticker

    // Pending file refs (outbound only, flushed alongside WAL)
    pendingFilesMu sync.Mutex
    pendingFiles   []pendingFileRef

    // Inbound polling
    cancel   context.CancelFunc
    pollDone chan struct{}
    handler  func(data []byte) error
}

// pendingFileRef captures file metadata to upload after WAL flush confirms.
type pendingFileRef struct {
    postID   string
    fileID   string
    connName string
    filename string
}
```

**Outbound (Publish + WAL):**
1. `Publish(ctx, data)`: Lock walMu, lazily open WAL file if nil, append `data + '\n'`, unlock. WAL file path: `{walDir}/{nodeID}-{unixMillis}-{seq}.jsonl`
2. Flush ticker (configurable, default 60s): Lock walMu, close current file and set to nil, unlock. Then upload closed file as blob, delete local file on success. If upload fails, leave file for startup recovery.
3. `Close()`: Flush immediately, cancel context.
4. Startup recovery: Scan the parent WAL root (`os.TempDir()/crossguard-wal/`) for ALL subdirectories (not just the current nodeID's directory). For each subdirectory, upload any `.jsonl` files and process any `.files.json` files, then delete on success. This handles the case where nodeID changes on restart (since `mmModel.NewId()` generates a new ID each `OnActivate`), which would otherwise orphan WAL files from previous runs. After recovery, remove empty old subdirectories.

**WAL directory:** `os.TempDir()/crossguard-wal/{nodeID}/` for current writes. Recovery scans the parent `crossguard-wal/` directory. Log warning at startup that WAL is in temp directory and may not survive container restarts.

**Flush rotation (addresses mutex/upload contention):**
1. Lock walMu AND pendingFilesMu (both locks held briefly)
2. Close current WAL file, save path, set walFile = nil
3. Snapshot pendingFiles into a local variable, set pendingFiles = nil
4. Unlock both locks (Publish and QueueFileRef can now proceed for the next batch)
5. Upload old WAL file as blob (no lock held)
6. On WAL upload success: upload snapshotted file refs via flushPendingFiles
7. Delete old WAL file on success only
8. On upload failure: log error, leave file for recovery scan

**Important:** pendingFiles MUST be snapshotted under the same critical section as WAL rotation (step 1-4). Otherwise, QueueFileRef calls for the NEXT batch could sneak in between WAL close and the drain, causing files to be uploaded for posts that haven't been uploaded yet.

**WAL backpressure:** If walDir contains more than 100 unflushed files (upload failures accumulating), Publish returns an error. This prevents unbounded disk growth.

**Blob naming:** `messages/{connName}/{nodeID}-{unixMillis}-{seq}.jsonl`
- nodeID ensures uniqueness across HA nodes
- Timestamp-first would be better for ordering but nodeID-first avoids collisions since timestamps could match across nodes

**Inbound (Subscribe + Poll):**
1. `Subscribe(ctx, handler)`: Store handler, start polling goroutine.
2. Poll loop (every 30s constant): List all blobs with prefix `messages/{connName}/`. For each `.jsonl` blob: attempt to acquire a KV store lock, download, split by `\n`, call `handler(line)` for each non-empty line, delete blob, release lock.
3. If any line fails, stop processing that blob, release lock (retry next poll).

**HA blob locking via KV store CAS:**

Use the Mattermost KV store's compare-and-set (`SetAtomic`) as a distributed lock to prevent multiple nodes from processing the same blob.

Key format: `blob-lock-{blobName}` (truncated/hashed if needed to fit KV key limits)

Lock value: JSON `{"node": "{nodeID}", "acquired": {unixMillis}}`

```go
const blobLockMaxAge = 5 * time.Minute

type blobLock struct {
    Node     string `json:"node"`
    Acquired int64  `json:"acquired"`
}
```

**Acquire flow:**
1. Read current value for key `blob-lock-{blobName}`
2. If empty: CAS from nil to `{nodeID, now}`. If CAS succeeds, lock acquired. If CAS fails (another node won), skip this blob.
3. If non-empty: decode the lock. Check age (`now - acquired`).
   - If age < `blobLockMaxAge` (5 min): another node is processing or recently processed it. Skip.
   - If age >= `blobLockMaxAge`: lock is stale (node likely crashed). CAS from old value to `{nodeID, now}`. If CAS succeeds, lock acquired (reprocess the blob). If CAS fails, another node beat us to reclaim. Skip.

**Release flow:**
- After successful processing + blob deletion: delete the lock key.
- After processing failure (will retry next poll): delete the lock key so another node (or this node next cycle) can retry.

**Crash recovery:** If a node acquires the lock then crashes before releasing, the lock sits in KV store with its timestamp. After `blobLockMaxAge` (5 min), any node can reclaim it via CAS. The blob is still in storage (never deleted since processing didn't complete), so it gets reprocessed.

**Why this is better than pure idempotency:**
- Avoids duplicate work across nodes (no wasted API calls for reactions, updates, deletes)
- Only one node downloads and processes each blob
- Stale lock cleanup is automatic via age check
- Uses existing KV store infrastructure (no new dependencies)

```go
func (a *azureBlobProvider) tryAcquireBlobLock(blobName string) bool {
    key := "blob-lock-" + hashBlobName(blobName)
    existing, err := a.kvGet(key)
    if err != nil {
        return false
    }

    if existing == nil {
        // No lock, try to claim
        lock := blobLock{Node: a.nodeID, Acquired: time.Now().UnixMilli()}
        return a.kvCAS(key, nil, lock)
    }

    // Lock exists, check age
    var current blobLock
    json.Unmarshal(existing, &current)
    age := time.Since(time.UnixMilli(current.Acquired))
    if age < blobLockMaxAge {
        return false // another node owns it, still fresh
    }

    // Stale lock, try to reclaim
    lock := blobLock{Node: a.nodeID, Acquired: time.Now().UnixMilli()}
    return a.kvCAS(key, existing, lock)
}

func (a *azureBlobProvider) releaseBlobLock(blobName string) {
    key := "blob-lock-" + hashBlobName(blobName)
    a.kvDelete(key)
}
```

The provider needs access to the KV store. Pass the `store.KVStore` (or a subset interface) to `newAzureBlobProvider` at construction time.

**File transfer (deferred until WAL flush):**

The problem: With batching, post messages accumulate in the WAL for up to 60s before upload. But the current code (`hooks.go:80-82`, `connections.go:250-323`) uploads file attachments immediately after relay. This means files arrive on the inbound side before the post they belong to, and the existing 3-retry/1s-delay in `handleInboundFile` cannot cover a 60s gap.

The solution: For the azure-blob provider, defer file uploads until after the WAL flush confirms the post message blob is in storage.

**Outbound file flow:**
1. When `uploadPostFiles` is called (`connections.go:250`), for azure-blob connections, instead of uploading immediately, call `provider.QueueFileRef(postID, fileID, connName, filename)` to collect file references.
2. A companion file `{same-base-name}.files.json` is written alongside the WAL `.jsonl` file, accumulating `pendingFileRef` entries as JSON.
3. On WAL flush, after the `.jsonl` blob upload succeeds:
   a. Read the companion `.files.json`
   b. For each file ref: download file from Mattermost via `p.API.GetFile(fileID)`, upload to blob storage under `files/` prefix with metadata headers (same pattern as existing `azureProvider.UploadFile`)
   c. Delete the `.files.json` after all files uploaded
4. On startup recovery: if a `.files.json` exists without a corresponding `.jsonl`, the post messages were already uploaded (WAL deleted) but files were not. Process the `.files.json` to upload remaining files.

**QueueFileRef method (new on azureBlobProvider, not on QueueProvider interface):**
```go
func (a *azureBlobProvider) QueueFileRef(postID, fileID, connName, filename string) {
    a.pendingFilesMu.Lock()
    defer a.pendingFilesMu.Unlock()
    a.pendingFiles = append(a.pendingFiles, pendingFileRef{
        postID: postID, fileID: fileID,
        connName: connName, filename: filename,
    })
}
```

**Integration with uploadPostFiles (`connections.go:250-323`):**

For azure-blob connections, the outbound file upload code path changes:
- Instead of downloading and uploading files immediately per-connection
- Type-assert the provider to `*azureBlobProvider` and call `QueueFileRef` for each file
- The actual download + upload happens at flush time

```go
// In uploadPostFiles, per connection:
if blobProvider, ok := oc.provider.(*azureBlobProvider); ok {
    // Defer file upload until WAL flush
    for _, fileID := range post.FileIds {
        fi, appErr := p.API.GetFileInfo(fileID)
        if appErr != nil { continue }
        if !isFileAllowed(fi.Name, oc.fileFilterMode, oc.fileFilterTypes) { continue }
        blobProvider.QueueFileRef(post.Id, fi.Id, oc.name, fi.Name)
    }
    continue // skip immediate upload for this connection
}
// ... existing immediate upload path for NATS/Azure Queue ...
```

**Flush file upload (inside flush method, after .jsonl blob upload succeeds):**
```go
func (a *azureBlobProvider) flushPendingFiles(ctx context.Context, getFile func(id string) ([]byte, error)) {
    a.pendingFilesMu.Lock()
    refs := a.pendingFiles
    a.pendingFiles = nil
    a.pendingFilesMu.Unlock()

    var failed []pendingFileRef
    for _, ref := range refs {
        data, err := getFile(ref.fileID)
        if err != nil {
            a.api.LogError("Deferred file upload: failed to get file",
                "file_id", ref.fileID, "post_id", ref.postID, "error", err.Error())
            continue
        }

        key := ref.postID + "/" + mmModel.NewId()
        headers := map[string]string{
            "X-Post-Id":   ref.postID,
            "X-Conn-Name": ref.connName,
            "X-Filename":  ref.filename,
        }
        if err := a.UploadFile(ctx, key, data, headers); err != nil {
            a.api.LogError("Deferred file upload: blob upload failed",
                "file_id", ref.fileID, "post_id", ref.postID, "error", err.Error())
            failed = append(failed, ref)
        }
    }
    // Re-enqueue failed refs so they are retried on the next flush cycle
    if len(failed) > 0 {
        a.pendingFilesMu.Lock()
        a.pendingFiles = append(failed, a.pendingFiles...)
        a.pendingFilesMu.Unlock()
    }
}
```

The provider needs a reference to the Plugin API for `GetFile`. This is passed as a callback during construction or flush to avoid circular dependencies.

**Inbound file handling (WatchFiles):** Reuse the same pattern as existing `azureProvider` (`azure_provider.go:208-280`). Files stored under `files/` prefix in the same container. Individual blob per file with metadata headers. The existing post mapping retry in `handleInboundFile` (3 retries, 1s delay) should now work because the post message blob is guaranteed to be uploaded before file blobs.

**MaxMessageSize:** Returns 0 (no per-message limit; messages batched into files).

### Part 2: Missing Message Retry Queue (`server/retry_queue.go`)

**This applies to ALL providers, not just azure-blob.**

**Data structure:**
```go
type retryEntry struct {
    connName   string
    rawData    []byte
    remoteID   string    // for logging/metrics
    msgType    string    // message type for direct dispatch on retry (avoids semaphore)
    enqueuedAt time.Time
    retries    int
}

type retryQueue struct {
    mu      sync.Mutex
    entries []*retryEntry
}
```

**Constants and computed values:**
```go
const (
    retryQueueMaxSize       = 1000
    retryQueueMaxRetries    = 3
    retryQueueDefaultMaxAge = 2 * time.Minute
    retryQueueTickRate      = 5 * time.Second
    retryQueueRetryDelay    = 20 * time.Second

    azureBlobPollInterval   = 30 * time.Second
)
```

**MaxAge is derived from the flush interval:** At config load time, scan all configured azure-blob connections for the largest `FlushIntervalSeconds`. Compute:
```go
maxAge = max(retryQueueDefaultMaxAge, 2 * maxFlushInterval + azureBlobPollInterval)
```
This ensures the retry queue holds items long enough for the slowest provider to deliver. For NATS-only or Azure Queue-only setups (no flush delay), max age stays at the 2-minute default. For azure-blob with a 60s flush, max age becomes `2*60 + 30 = 150s`. For a 300s flush, max age becomes `2*300 + 30 = 630s`.

The `retryQueue` struct accepts `maxAge` as a parameter at construction time (not a global constant), updated on `OnConfigurationChange`.

**Integration approach (addresses async dispatch issue):**

The `handleInboundMessage` goroutine dispatches to void handler methods. Since the goroutine runs asynchronously and the Subscribe handler already returned nil, we cannot use sentinel errors at the handler return level. Instead:

1. Modify `handleInboundUpdate`, `handleInboundDelete`, `handleInboundReaction` to return a `bool` indicating whether the message should be retried. This should be `true` both when the post mapping is empty AND when the KV store lookup returns an error (treat KV errors as transient/retriable rather than silently dropping the message).
2. In the `p.wg.Go()` goroutine body inside `handleInboundMessage`, check the return value and call `p.retryQueue.Enqueue(connName, data, remoteID, msgType)` directly.
3. The raw `data []byte` is already available in the closure scope.

Modified dispatch example:
```go
case model.MessageTypeDelete:
    if env.DeleteMessage == nil {
        p.API.LogError("Inbound delete: missing payload", "conn", connName)
        return
    }
    if missing := p.handleInboundDelete(connName, env.DeleteMessage); missing {
        p.API.LogWarn("Missing message: queuing for retry",
            "conn", connName,
            "type", env.Type,
            "remote_post_id", env.DeleteMessage.PostID,
            "queue_size", p.retryQueue.Len())
        p.retryQueue.Enqueue(connName, data, env.DeleteMessage.PostID)
    }
```

**Retry queue logging (all events logged for operational visibility):**
- **Enqueue**: `LogWarn("Missing message: queuing for retry", "conn", connName, "type", msgType, "remote_post_id", id, "queue_size", size)` - logged at the dispatch site above
- **Retry success**: `LogWarn("Missing message: retry succeeded", "conn", connName, "remote_post_id", id, "attempt", n)` - logged inside the retry goroutine after successful re-processing
- **Retry still missing**: `LogWarn("Missing message: still missing after retry", "conn", connName, "remote_post_id", id, "attempt", n, "next_retry_in", delay)` - logged when re-processing still finds no mapping
- **Dropped (max retries)**: `LogError("Missing message: dropped after max retries", "conn", connName, "remote_post_id", id, "attempts", n)` - logged when item exceeds max retries
- **Dropped (max age)**: `LogError("Missing message: dropped, exceeded max age", "conn", connName, "remote_post_id", id, "age", age)` - logged when item exceeds TTL
- **Queue full**: `LogError("Missing message: queue full, dropping message", "conn", connName, "remote_post_id", id, "queue_size", maxSize)` - logged when Enqueue rejects due to capacity

This gives operators full visibility into how often out-of-order messages occur, how many resolve on retry, and how many are permanently lost.

**PostDiagnostic calls (in addition to log entries):**
- **Dropped (max retries)**: `p.PostDiagnostic("Missing message dropped after %d retries: type=%s remote_id=%s conn=%s", ...)`
- **Dropped (max age)**: `p.PostDiagnostic("Missing message expired (age %s): type=%s remote_id=%s conn=%s", ...)`
- **Queue full**: `p.PostDiagnostic("Retry queue full (%d items), dropping message: type=%s remote_id=%s conn=%s", ...)`

These events represent permanent message loss and must be visible in the diagnostics channel, not just server logs.

**Background retry goroutine:**
- Started in `OnActivate`, stopped via context cancellation in `OnDeactivate`
- Ticks every 5s. For each entry: if age > maxAge or retries >= maxRetries, drop and log. If time since enqueue (or last retry) < retryDelay, skip. Otherwise, call a dedicated `p.retryInboundMessage(entry)` method.
- **Retry dispatch path:** `retryInboundMessage` does NOT go through `handleInboundMessage` or the semaphore. Instead, it directly calls the appropriate handler (`handleInboundUpdate`, `handleInboundDelete`, `handleInboundReaction`, or `handleInboundPost`) based on the message type stored in the `retryEntry`. This avoids the semaphore rejection problem (where retried messages would be permanently dropped if the semaphore is full) and allows passing `lastAttempt` directly to `handleInboundPost`.
- Add `msgType string` field to `retryEntry` to enable direct dispatch on retry.
- Queue survives `reconnectInbound()` (it is on the Plugin struct, not tied to connections).

**Thread root resolution via retry queue:**

Currently (`inbound.go:293-301`), when a reply's `RootID` has no local mapping, the post is created as standalone (no thread). With the retry queue, we can do better:

1. Modify `handleInboundPost` to return a `bool` for missing root (in addition to the existing missing-mapping bool for updates/deletes/reactions).
2. When `postMsg.RootID != ""` and `GetPostMapping(connName, postMsg.RootID)` returns empty, return `missing=true`.
3. The dispatcher enqueues the entire post message for retry, same as reactions/deletes.
4. On retry: look up the root mapping again. If found, create the post as a threaded reply with `RootId` set.
5. **On final attempt (retries exhausted):** Create the post as standalone (no `RootId`). This is the graceful fallback, same as current behavior but only after giving the root post a chance to arrive.

```go
// In handleInboundPost, after resolving team/channel/user:
if postMsg.RootID != "" {
    localRootID, err := p.kvstore.GetPostMapping(connName, postMsg.RootID)
    if err != nil {
        p.API.LogWarn("Inbound post: root mapping lookup failed",
            "conn", connName, "remote_root_id", postMsg.RootID, "error", err.Error())
    }
    if localRootID != "" {
        post.RootId = localRootID
    } else if !lastAttempt {
        // Root not found yet, queue for retry
        return true // missing
    }
    // If lastAttempt and still no root, fall through and create as standalone
}
```

The `lastAttempt` flag is passed to the handler so it knows whether to retry or give up. This requires a small change to the handler signature: `handleInboundPost(connName string, postMsg *model.PostMessage, lastAttempt bool) bool`. For the initial dispatch from `handleInboundMessage`, `lastAttempt` is `false`. For retries from the retry goroutine, it is `true` only on the final attempt (retries == maxRetries - 1).

Log entries:
- **Queued**: `LogWarn("Missing message: queuing for retry", "type", "crossguard_post", "reason", "missing_root", "remote_root_id", rootID)`
- **Retry found root**: `LogWarn("Missing message: retry succeeded, root found", "remote_root_id", rootID, "attempt", n)`
- **Created standalone**: `LogWarn("Missing message: root not found after retries, creating standalone", "remote_root_id", rootID, "attempts", n)`

### Part 3: Configuration Changes

**New config struct in `server/configuration.go`:**
```go
const ProviderAzureBlob = "azure-blob"

type AzureBlobProviderConfig struct {
    ConnectionString     string `json:"connection_string"`
    BlobContainerName    string `json:"blob_container_name"`
    FlushIntervalSeconds int    `json:"flush_interval_seconds,omitempty"` // default 60
}
```

**Update `ConnectionConfig`:**
```go
AzureBlob *AzureBlobProviderConfig `json:"azure_blob,omitempty"`
```

**Validation (`validateAzureBlobConnection`):**
- `connection_string` required
- `blob_container_name` required
- `flush_interval_seconds` >= 5 if set

**Provider factory (`connections.go:326`):** Add `case ProviderAzureBlob`.

**Plugin struct (`plugin.go`):** Add `nodeID string` (generated via `mmModel.NewId()` in `OnActivate`) and `retryQueue *retryQueue`.

### Part 4: Admin UI Changes

**`webapp/src/components/ConnectionSettings.tsx`:**

- Add `'azure-blob'` to `ProviderType` union
- Add `AzureBlobProviderConfig` interface
- Add `azure_blob` field to `Connection` interface
- Add `<option value='azure-blob'>Azure Blob Storage</option>` to provider dropdown
- Add form section for `azure-blob` provider: Connection String, Blob Container Name, Flush Interval (with default 60)

### Part 5: API Updates

**`server/api.go`:** Add `case ProviderAzureBlob` in test connection handler. Test by creating container client, uploading a small test blob, reading it back, deleting it.

**`server/service.go`:** Update `redactConnection` to handle `ProviderAzureBlob`.

## Decisions

| Question | Decision | Rationale |
|----------|----------|-----------|
| WAL vs direct upload? | WAL with batching | User explicitly requested batching and WAL behavior |
| Inbound HA claim mechanism? | KV store CAS lock with 5-min stale age | Prevents duplicate work across nodes; stale locks auto-recover on crash |
| File upload timing? | Deferred until WAL flush confirms | Files must not arrive before their post; companion .files.json tracks pending refs |
| Retry queue storage? | In-memory per node | Best-effort is acceptable; KV store adds complexity for marginal benefit |
| Retry queue scope? | All providers | Reactions/deletes can arrive out-of-order on any provider |
| Configurable intervals? | Only flush interval | Follow existing pattern of constants for internal timers |
| Retry queue integration? | Inside async goroutine | handleInboundMessage returns nil immediately; cannot use sentinel errors at handler level |
| Thread root retry? | Yes, retry via queue; create standalone on final attempt | Avoids re-parenting complexity (post isn't created until root found or retries exhausted) |
| Separate PRs? | No, ship together | Retry queue is needed to handle azure-blob's batch-induced ordering issues |

## Files to Modify

| File | Change |
|------|--------|
| `server/azure_blob_provider.go` | **NEW** - Core provider implementation (~450 lines, includes deferred file upload) |
| `server/azure_blob_provider_test.go` | **NEW** - Unit tests (~350 lines) |
| `server/retry_queue.go` | **NEW** - Retry queue struct and methods (~100 lines) |
| `server/retry_queue_test.go` | **NEW** - Retry queue unit tests (~150 lines) |
| `server/configuration.go` | Add `ProviderAzureBlob`, `AzureBlobProviderConfig`, validation |
| `server/connections.go` | Add `case ProviderAzureBlob` in `createProvider`; modify `uploadPostFiles` to defer files for azure-blob |
| `server/plugin.go` | Add `nodeID` and `retryQueue` fields, init in `OnActivate` |
| `server/inbound.go` | Modify handlers to return missing bool, integrate retry queue |
| `server/api.go` | Add test connection for azure-blob |
| `server/service.go` | Update redaction for new provider |
| `webapp/src/components/ConnectionSettings.tsx` | Add azure-blob provider UI |
| `server/configuration_test.go` | Add validation tests for azure-blob config |
| `server/inbound_test.go` | Add retry queue integration tests |
| `server/connections_test.go` | Add createProvider test for azure-blob |

## Tasks

1. [ ] Add `ProviderAzureBlob` constant, `AzureBlobProviderConfig` struct, and validation to `server/configuration.go`
2. [ ] Add configuration tests for azure-blob validation in `server/configuration_test.go`
3. [ ] Add `nodeID` and `retryQueue` fields to `Plugin` struct, initialize in `OnActivate`
4. [ ] Implement `server/retry_queue.go` (Enqueue, drain, purge methods)
5. [ ] Write `server/retry_queue_test.go` (timing, bounds, concurrent access)
6. [ ] Modify `handleInboundUpdate/Delete/Reaction` to return `bool` for missing mapping
7. [ ] Integrate retry queue into `handleInboundMessage` goroutine dispatch
8. [ ] Add retry goroutine startup in `OnActivate` and shutdown in `OnDeactivate`
9. [ ] Implement `server/azure_blob_provider.go` (constructor, Publish+WAL, flush with deferred file upload, Subscribe+poll, UploadFile, WatchFiles, Close, crash recovery)
10. [ ] Implement `QueueFileRef` and `flushPendingFiles` on `azureBlobProvider` for deferred file uploads
11. [ ] Modify `uploadPostFiles` in `server/connections.go` to type-assert azure-blob provider and defer file refs instead of uploading immediately
12. [ ] Write `server/azure_blob_provider_test.go` (including deferred file upload tests)
13. [ ] Wire `ProviderAzureBlob` into `createProvider` factory in `server/connections.go`
14. [ ] Add test connection endpoint in `server/api.go`
15. [ ] Update `redactConnection` in `server/service.go`
16. [ ] Add azure-blob provider option and form fields in `webapp/src/components/ConnectionSettings.tsx`
17. [ ] Run `make check-style && make test` to verify no regressions

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| WAL files lost on container restart | Log warning at startup; document volume mount requirement for container deployments |
| Upload failures accumulate WAL files | Backpressure: Publish returns error when >100 unflushed files |
| HA double-processing on inbound | KV store CAS lock per blob; only one node processes; stale locks auto-expire at 5 min |
| Batch ordering across HA nodes | Retry queue handles dependent messages arriving before their parent |
| Large .jsonl files (high volume + long flush) | Consider adding size-based flush trigger (e.g., 200MB) in addition to time-based |
| Retry queue memory growth | Bounded: max 1000 items, max 2 min age, max 3 retries |
| Partial batch failure | Stop processing blob on first line error; retry entire blob next poll |
| File upload fails after WAL flush | .files.json persists locally; recovered on next startup scan |
| File arrives before post (other providers) | Only affects NATS/Azure Queue which upload immediately; existing 3-retry/1s-delay handles this since those providers publish synchronously |

## HA Safety Analysis

| Scenario | Handling |
|----------|----------|
| Multiple nodes writing outbound | Each node has unique nodeID in blob name; no collisions |
| Multiple nodes polling inbound | KV store CAS lock ensures only one node processes each blob; losers skip |
| Node crashes mid-blob-processing | Lock sits in KV store with timestamp; after 5 min, any node reclaims via CAS and reprocesses |
| Node crash mid-flush | WAL file persists locally; uploaded on next startup |
| Node crash after upload, before WAL delete | Duplicate blob (different name due to new nodeID); idempotency handles it |
| Retry queue lost on restart | Acceptable (best-effort); items typically resolve within 60s |

## Testing Plan

**Unit tests:**
- `retry_queue_test.go`: Enqueue/drain timing, max size rejection, max age expiry, max retry removal, concurrent access
- `azure_blob_provider_test.go`: WAL write/flush/rotation, crash recovery scan, poll and process .jsonl lines, MaxMessageSize returns 0, compile-time interface conformance
- `configuration_test.go`: Azure-blob config validation (missing fields, invalid flush interval)
- `inbound_test.go`: Handler returns missing=true when mapping absent, retry queue receives entry

**Integration tests (with Azurite):**
- `make docker-azure-smoke-test` extended to test azure-blob provider
- Full round-trip: outbound Publish -> flush -> blob appears -> inbound poll -> handler called
- Out-of-order: send reaction before post, verify retry queue processes it after post arrives

**Manual verification:**
- Deploy to docker dev environment with `provider: "azure-blob"`
- Send messages, verify .jsonl files appear in Azurite blob storage
- Verify messages arrive on inbound side within flush interval + poll interval
- Send reaction before post (via delay), verify retry queue handles it

## Acceptance Criteria

- [ ] Plugin activates with `provider: "azure-blob"` configured
- [ ] Outbound messages are batched into .jsonl files and uploaded every 60s (or configured interval)
- [ ] Inbound side polls, downloads, and processes .jsonl messages
- [ ] WAL files from previous unclean shutdown are recovered on startup
- [ ] Reactions/deletes to unknown posts are retried and eventually processed
- [ ] File attachments are uploaded only after their post message blob is confirmed in storage
- [ ] File transfers work via blob storage on inbound side
- [ ] Admin UI shows Azure Blob Storage as provider option with appropriate fields
- [ ] All existing tests pass (`make check-style && make test`)

## Checklist

- [ ] **Diagnostics**: Out-of-order message retries should log to diagnostics channel for visibility
- [ ] **Slash command**: No new slash command needed (existing `/crossguard` commands work with any provider)
