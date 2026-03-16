# Inbound NATS Relay Worker

## Context

The crossguard plugin currently only relays **outbound** (local posts -> NATS). When a team/channel is initialized and a user posts, the message is published to configured outbound NATS connections. There is no inbound path: messages arriving on NATS from other servers are ignored.

This plan adds an **inbound NATS subscription worker** that listens on configured inbound connections, receives messages, resolves teams/channels by name, creates synthetic users representing remote senders, and creates local posts/reactions. This completes the bidirectional relay.

## Design Principles

| Pattern | Our Approach | Avoid | Reference |
|---------|-------------|--------|-----------|
| Inbound connections | Mirror outbound pattern exactly | Polling jobs, cluster.Schedule | `nats.go:99-122` (connectOutbound) |
| Name-based mapping | Look up team/channel by name, not ID | ID-based matching | User requirement |
| Synthetic users | Create local users with munged usernames | Bot user posts, or no attribution | MM shared channels pattern |
| Loop prevention (posts/updates) | `crossguard_relayed` prop on all inbound-created posts | User-based filtering | `hooks.go:61` (existing pattern) |
| Loop prevention (reactions) | Check if reacting user is a sync user (`Position == "crossguard-sync"`) | Checking post prop (blocks local reactions on relayed posts) | Reviewed during plan review |
| Loop prevention (deletes) | Short-lived KV flag set by inbound delete handler before calling DeletePost | Checking post prop (blocks local deletes of relayed posts, and `MessageHasBeenDeleted` provides post author not deleter) | Reviewed during plan review |
| Post correlation | KV store mapping remote PostID -> local PostID | Same-ID approach (plugin API generates new IDs, cannot reuse remote IDs like MM core shared channels does) | Added to store interface |
| Simplicity | No extra caching layers; use MM API directly | KV-based user lookup cache | YAGNI |

## Requirements

- [ ] Subscribe to inbound NATS connections on plugin activation
- [ ] Process all message types: post, update, delete, reaction_add, reaction_remove
- [ ] Resolve team by name, channel by name (not IDs)
- [ ] Verify team and channel are initialized before processing
- [ ] Create synthetic users for remote senders (username munging with connection name)
- [ ] Set `crossguard_relayed` on all created posts to prevent re-relay
- [ ] Map remote PostID to local PostID for update/delete/reaction correlation
- [ ] Handle threaded replies (RootID mapping)
- [ ] Graceful shutdown (drain subscriptions on deactivate)
- [ ] Reconnect on config change

## Out of Scope

- Profile image syncing for synthetic users
- File attachment relay (PostMessage has no FileIds field)
- Custom status or presence syncing
- Channel membership syncing beyond what is needed for posting
- JetStream durable subscriptions (standard NATS subscribe is sufficient for v1)
- @mention translation across servers

## Technical Approach

### New Files

#### 1. `server/inbound.go` - Core inbound subscription logic

```go
type inboundConn struct {
    nc   *nats.Conn
    sub  *nats.Subscription
    name string
}
```

**Functions:**
- `connectInbound()` - Parse inbound config, connect via `connectNATSPersistent(..., "Inbound")`, subscribe to each subject with `handleInboundMessage` callback. Store in `p.inboundConns`.
- `closeInbound()` - Unsubscribe, drain, close all inbound connections.
- `reconnectInbound()` - Close then connect (mirrors `reconnectOutbound`).
- `handleInboundMessage(connName string) nats.MsgHandler` - Returns closure that spawns a goroutine (with `p.wg.Add/Done`) that:
  1. Acquires semaphore (`p.relaySem`), drops if full
  2. Checks `p.ctx.Done()` to avoid processing during shutdown
  3. Unmarshals envelope via `model.UnmarshalMessage`
  4. Switches on `envelope.Type` to dispatch to type-specific handlers
  5. Releases semaphore
- `resolveTeamAndChannel(teamName, channelName string) (*mmModel.Team, *mmModel.Channel, error)` - `GetTeamByName` to get team, then `GetChannelByName(team.Id, channelName, false)` to get channel. Verify both initialized via `p.kvstore.GetTeamInitialized()` and `p.kvstore.GetChannelInitialized()`. Returns error if lookup fails or either is not initialized.
- `handleInboundPost(connName string, envelope *model.Message)` - Decode PostMessage, resolve channel, ensureSyncUser, create post with `crossguard_relayed` prop, store post mapping via kvstore.
- `handleInboundUpdate(connName string, envelope *model.Message)` - Decode PostMessage, look up local PostID from mapping. If not found, log warning and drop. Otherwise get post, update Message, update post.
- `handleInboundDelete(envelope *model.Message)` - Decode DeleteMessage, look up local PostID. If not found, log warning and drop. Otherwise set short-lived KV flag `crossguard-deleting-{localPostID}`, call `API.DeletePost`, then delete the flag and clean up post mapping. The flag prevents the `MessageHasBeenDeleted` hook from re-relaying this delete outbound. Note: DeleteMessage has no username field, so the plugin API performs the delete directly (no synthetic user needed).
- `handleInboundReaction(connName string, envelope *model.Message, add bool)` - Decode ReactionMessage, look up local PostID, resolve team/channel from message, ensureSyncUser, add/remove reaction using the sync user's ID.

#### 2. `server/sync_user.go` - Synthetic user management

**Username format:** `{username}.{connName}` (e.g., `usera.high`)

- Connection names are validated as `^[a-z0-9]+(-[a-z0-9]+)*$` (configuration.go:12), so dots create an unambiguous boundary.
- Mattermost usernames allow dots, so the format is valid.

**`ensureSyncUser(username, connName, teamID, channelID string) (string, error)`:**
1. Compute munged username: `username + "." + connName`. Validate `len(mungedUsername) <= 64` (MM username limit). If too long, truncate username to fit and log a warning.
2. Call `p.API.GetUserByUsername(mungedUsername)` (no KV cache; MM caches user lookups internally)
3. If found, ensure team/channel membership, return user.Id
4. If not found, create user:
   - Username: munged
   - Email: `mmModel.NewId() + "@crossguard.local"` (random, unique, not a real email)
   - Password: `mmModel.NewId() + "!Aa1"` (random, meets complexity, user never logs in)
   - Roles: `system_user`
   - Nickname: original username
   - FirstName: original username
   - LastName: `(via {connName})`
   - Position: `crossguard-sync` (marker for identification)
   - **RemoteId: `model.NewPointer("crossguard-" + connName)`** (triggers native SharedUserIndicator badge in MM UI)
5. If CreateUser fails with "username already taken" (race condition from concurrent messages), retry GetUserByUsername and verify Position is `crossguard-sync`. If the existing user is a real user (not sync), log error and drop.
6. Ensure team membership: `p.API.CreateTeamMember(teamID, user.Id)` (idempotent, ignore "already member" errors)
7. Ensure channel membership: `p.API.AddChannelMember(channelID, user.Id)` (idempotent, ignore "already member" errors)

**Native UI badge:** Setting `RemoteId` on the user causes the Mattermost webapp's `SharedUserIndicator` component to automatically render a shared-user icon next to the username in posts, mentions, member lists, and profile popovers. The `init-channel` command already sets `channel.Shared = true`, so the full shared channel visual treatment applies with zero webapp code. This mirrors exactly how MM core shared channels displays remote users.

Team and channel membership calls (CreateTeamMember/AddChannelMember with idempotent error handling) are inlined in `ensureSyncUser` steps 6-7 above, not extracted into a separate helper.

### Modified Files

#### 3. `server/store/store.go` - Add post mapping methods to KVStore interface

```go
type KVStore interface {
    // ... existing methods ...
    SetPostMapping(connName, remotePostID, localPostID string) error
    GetPostMapping(connName, remotePostID string) (string, error)
    DeletePostMapping(connName, remotePostID string) error
}
```

#### 4. `server/store/client.go` - Implement post mapping methods

KV key pattern: `pm-{connName}-{remotePostID}` -> `localPostID` (string value). The `pm-` prefix is short to stay within the 50-char KV key limit (e.g., `pm-high-` = 7 chars + 26-char post ID = 33 chars, well within limit). Namespacing by connection name prevents collisions when multiple remote servers are connected.

Simple Get/Set/Delete on the KV store. These go through the existing store layer so they benefit from the plugin's KV infrastructure.

#### 5. `server/store/caching.go` - Likely no changes needed

Post mappings are write-once-read-few (only read on update/delete/reaction for a specific post). No caching needed. Since `CachingKVStore` embeds the `KVStore` interface, Go's embedding auto-delegates post mapping methods to the inner store. Verify during implementation; skip `caching.go` changes if delegation works automatically.

#### 6. `server/plugin.go`

Add to Plugin struct:
```go
inboundMu    sync.RWMutex
inboundConns []inboundConn
```

In `OnActivate()` (after line 84 `p.connectOutbound()`):
```go
p.connectInbound()
```

In `OnDeactivate()`, insert `closeInbound()` before `cancel()`:
```go
p.closeInbound()  // 1. Stop accepting new inbound messages
// existing: p.cancel()  // 2. Signal context cancellation to in-flight goroutines
// existing: p.wg.Wait() // 3. Wait for all goroutines (inbound + outbound) to finish
// existing: p.closeOutbound() // 4. Close outbound connections last
```

Full deactivate order: closeInbound -> cancel -> wg.Wait -> closeOutbound. Inbound closes first to stop new messages. Outbound stays open until all in-flight goroutines complete (so inbound goroutines that trigger outbound relay can still publish).

#### 7. `server/configuration.go`

In `OnConfigurationChange()` (line 199-201), add `reconnectInbound`:
```go
if p.relaySem != nil {
    p.reconnectOutbound()
    p.reconnectInbound()
}
```

#### 8. `server/nats.go`

Add `direction` parameter to `connectNATSPersistent` so log messages distinguish inbound from outbound:
```go
func connectNATSPersistent(conn NATSConnection, p *Plugin, direction string) (*nats.Conn, error)
```

Update the two log handlers to use `direction` instead of hardcoded "Outbound":
- `p.API.LogWarn(direction+" NATS disconnected", ...)`
- `p.API.LogInfo(direction+" NATS reconnected", ...)`

Update existing call site in `connectOutbound()` to pass `"Outbound"`.

#### 9. `server/hooks.go`

Add loop prevention guards to prevent re-relaying inbound operations. Each hook type uses a different strategy because `crossguard_relayed` on the post identifies the **post origin**, not the **actor origin**.

**`MessageHasBeenDeleted` (line 111):** Check for short-lived KV flag set by the inbound delete handler:
```go
if post.UserId == p.botUserID {
    return
}
// Check if this delete was triggered by the inbound handler
flagKey := "crossguard-deleting-" + post.Id
if val, _ := p.kvstore.Get(flagKey); val != nil {
    return
}
```
The `MessageHasBeenDeleted` hook provides `post.UserId` which is the post **author**, not the deleter, so we cannot use user-based filtering here. Instead, the inbound delete handler sets a KV flag before calling `API.DeletePost`, and this hook checks for it. Local admin deletes of relayed posts will correctly relay outbound (no flag present).

**`ReactionHasBeenAdded` (line 136):** Check if the reacting user is a sync user:
```go
if post.IsSystemMessage() || post.UserId == p.botUserID {
    return
}
user, appErr := p.API.GetUser(reaction.UserId)
if appErr != nil {
    return
}
if user.Position == "crossguard-sync" {
    return
}
```

**`ReactionHasBeenRemoved` (line 167):** Same sync user check as `ReactionHasBeenAdded`:
```go
if post.IsSystemMessage() || post.UserId == p.botUserID {
    return
}
user, appErr := p.API.GetUser(reaction.UserId)
if appErr != nil {
    return
}
if user.Position == "crossguard-sync" {
    return
}
```

**Why different strategies per hook type:**
- **Posts/Updates**: `crossguard_relayed` prop on the post works correctly (already in place). Inbound creates post with prop, hook sees prop and skips.
- **Reactions**: Checking the post prop would block LOCAL users from reacting to relayed posts. Instead, check if the **reacting user** is a sync user. Local user reactions on relayed posts correctly relay outbound.
- **Deletes**: `MessageHasBeenDeleted` does not provide the deleter's identity. A short-lived KV flag signals "this delete was triggered by inbound." Local admin deletes of relayed posts correctly relay outbound.

**Note:** The reaction hooks already call `p.API.GetUser(reaction.UserId)` later in the function to get the username for the envelope. The sync user check can reuse that same user object by moving the GetUser call earlier. No additional API call needed.

## Message Processing Flow

```
NATS msg arrives on inbound subscription
  -> handleInboundMessage (spawn goroutine, acquire semaphore)
    -> check p.ctx.Done() (skip if shutting down)
    -> unmarshal envelope (model.UnmarshalMessage)
    -> switch on envelope.Type:

      crossguard_post:
        -> decode PostMessage
        -> resolveTeamAndChannel(teamName, channelName)
           -> GetTeamByName, then GetChannelByName(team.Id, channelName, false)
           -> verify team initialized, channel initialized
        -> ensureSyncUser(username, connName, teamID, channelID)
           -> validate munged username <= 64 chars
           -> GetUserByUsername(munged) or CreateUser
           -> ensure team + channel membership (inline)
        -> if RootID != "", look up local root via kvstore.GetPostMapping(connName, rootID)
           -> if not found, create as top-level post (graceful degradation)
        -> API.CreatePost with Props{"crossguard_relayed": true}
        -> kvstore.SetPostMapping(connName, remotePostID, localPostID)

      crossguard_update:
        -> decode PostMessage
        -> kvstore.GetPostMapping(connName, remotePostID) -> localPostID
        -> if not found, log warning and drop
        -> API.GetPost(localPostID), update Message field
        -> API.UpdatePost (post retains crossguard_relayed prop)

      crossguard_delete:
        -> decode DeleteMessage
        -> kvstore.GetPostMapping(connName, remotePostID) -> localPostID
        -> if not found, log warning and drop
        -> set KV flag "crossguard-deleting-{localPostID}" (loop prevention)
        -> API.DeletePost(localPostID)
        -> delete KV flag "crossguard-deleting-{localPostID}"
        -> kvstore.DeletePostMapping(connName, remotePostID)

      crossguard_reaction_add / crossguard_reaction_remove:
        -> decode ReactionMessage
        -> kvstore.GetPostMapping(connName, remotePostID) -> localPostID
        -> if not found, log warning and drop
        -> resolveTeamAndChannel(teamName, channelName)
        -> ensureSyncUser(username, connName, teamID, channelID)
        -> API.AddReaction / API.RemoveReaction with sync user ID
        -> (loop prevention: reaction hook checks user.Position == "crossguard-sync")

      crossguard_test:
        -> log debug and ignore (connectivity test)
```

## Files to Modify

| File | Change |
|------|--------|
| `server/inbound.go` | **NEW** - Inbound subscription, message dispatch, all handlers |
| `server/sync_user.go` | **NEW** - Synthetic user creation with race-safe retry |
| `server/store/store.go` | Add `SetPostMapping`, `GetPostMapping`, `DeletePostMapping` to KVStore interface |
| `server/store/client.go` | Implement post mapping methods with `pm-{connName}-` key prefix |
| `server/store/caching.go` | Likely no changes (Go embedding auto-delegates); verify during implementation |
| `server/plugin.go` | Add inboundConn fields, wire connect/close in lifecycle |
| `server/configuration.go` | Add `reconnectInbound()` in OnConfigurationChange |
| `server/nats.go` | Add `direction` param to `connectNATSPersistent` |
| `server/hooks.go` | Add loop prevention: KV flag for deletes, sync user check for reactions |

## Tasks

1. [ ] Add loop prevention to hooks.go (KV flag for delete, sync user check for reactions)
2. [ ] Add post mapping methods to store interface and implementation (store.go, client.go; verify caching.go needs no changes)
3. [ ] Create `server/sync_user.go` with ensureSyncUser
4. [ ] Create `server/inbound.go` with all inbound subscription logic
5. [ ] Modify `server/plugin.go` (struct fields, OnActivate, OnDeactivate ordering)
6. [ ] Modify `server/nats.go` (add direction parameter)
7. [ ] Modify `server/configuration.go` (add reconnectInbound)
8. [ ] Write tests
9. [ ] Run `make check-style` and `make test`

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Username collision with real user | Dot-connName pattern is unlikely to collide. If CreateUser fails, retry GetUserByUsername and check Position is `crossguard-sync`. If real user, log error and drop. |
| Post mapping KV growth | Mappings are small strings (~50 bytes each). Could add periodic cleanup or TTL later if needed. |
| Thread root mapping miss | If root mapping not found, create reply as top-level post (graceful degradation). |
| Brief message loss during config reload | Acceptable, matches outbound pattern. NATS reconnect is fast. |
| Concurrent sync user creation (race) | CreateUser with retry-on-conflict pattern handles this. |
| Delete arrives before create (ordering) | Extremely unlikely on single NATS subject. If it happens, delete is dropped (no mapping), create proceeds. Post persists but this is an acceptable edge case. |
| Shared semaphore contention | Inbound and outbound share the 50-slot semaphore. Acceptable for v1; can split later if needed. |
| Munged username exceeds 64-char limit | Validate length before CreateUser. Remote usernames can be up to 64 chars; with `.connName` appended this could exceed the limit. Truncate and log warning. |

## Testing Plan

**Unit tests:**
- `server/inbound_test.go` - resolveTeamAndChannel (not found, not initialized, happy path), handleInboundPost (full flow with post mapping verification), handleInboundUpdate (mapping found/not found), handleInboundDelete, handleInboundReaction
- `server/sync_user_test.go` - Username munging, create path, existing user path, race condition retry, team/channel membership
- `server/store/client_test.go` - Add post mapping round-trip tests (store/get/delete)

**Integration (manual with docker):**
- Configure Server A with outbound, Server B with inbound on same NATS subject
- Init team and channel on both servers (same names)
- Post on Server A, verify it appears on Server B with synthetic user
- Edit post on A, verify update on B
- Delete post on A, verify deletion on B
- Add/remove reaction on A, verify on B
- Verify no relay loops (posts don't bounce back)

## Acceptance Criteria

- [ ] Posts from Server A appear on Server B in the matching team/channel
- [ ] Synthetic users are created with munged usernames (e.g., `usera.high`)
- [ ] Post edits and deletes propagate correctly
- [ ] Reactions propagate correctly
- [ ] No relay loops (posts don't bounce back and forth)
- [ ] Plugin starts/stops cleanly with inbound connections
- [ ] Config changes reconnect inbound subscriptions
- [ ] `make check-style` and `make test` pass
