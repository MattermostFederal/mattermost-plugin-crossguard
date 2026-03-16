# /crossguard init-channel + teardown commands

## Context

Cross Guard currently supports team-level initialization (`/crossguard init-team`) but has no mechanism to relay messages from specific channels over NATS, and no way to reverse initialization. This plan adds channel-level initialization, teardown commands for both channels and teams, and hooks for all channel events (post, update, delete, reaction add, reaction remove) that publish to outbound NATS connections in a goroutine with retry and exponential backoff on failure.

## Overview

1. `/crossguard init-channel` slash command marks a channel for cross-domain relay
2. `/crossguard teardown-channel` removes a channel from relay
3. `/crossguard teardown-team` removes a team (and effectively all its channels) from relay
4. REST APIs for init-channel, teardown-channel, and teardown-team (CLI-callable)
5. Hooks check both team AND channel init (cached lookups), then relay via NATS:
   - `MessageHasBeenPosted` - new post created
   - `MessageHasBeenUpdated` - post edited
   - `MessageHasBeenDeleted` - post deleted
   - `ReactionHasBeenAdded` - reaction added to post
   - `ReactionHasBeenRemoved` - reaction removed from post
6. If both initialized, spawns a goroutine to publish to all outbound NATS connections
7. On failure: retry with exponential backoff up to configured max

## Design Decisions

| Question | Decision | Rationale |
|----------|----------|-----------|
| KV store scope | Get/Set/Delete for channel init + RemoveInitializedTeamID for team teardown | Teardown needs delete operations. No channel ID list (YAGNI). |
| Teardown-team effect | Removes team init flag; hook checks team init so all channels in that team stop relaying immediately | No need to enumerate channels. Channel init flags remain (re-init team re-enables them). |
| Hook team check | MessageHasBeenPosted checks BOTH team and channel init (both cached) | Enables teardown-team to act as a kill switch for all channels in the team. |
| Permissions | Channel member + channel/team/system admin | Must be a member of the channel AND have admin role. Normal users cannot init. |
| Team init required? | Yes, check team is initialized first | Clean hierarchy: team init -> channel init -> relay |
| Retry config | Constants (not plugin settings) | Simple. Promote to settings later if needed. |
| NATS connections | Persistent pool keyed by connection name, rebuilt on config change | Avoids per-request connect overhead. Reconnect handled by nats.go auto-reconnect + config change rebuild. |
| Goroutine control | ctx/cancel/wg + semaphore (50 concurrent) | User requires goroutine. Semaphore prevents goroutine explosion. |
| Loop prevention | Skip bot/system posts + check `crossguard_relayed` post prop | Defense-in-depth. Prop marker survives bot ID mismatches across servers. |
| PostMessage model | Custom struct with RootId, ChannelName, TeamName (security boundary) | Don't leak full `model.Post` across domains. Include names for cross-domain channel/team mapping. |

## Files to Modify

| File | Change |
|------|--------|
| `server/store/store.go` | Add channel init + teardown methods to interface |
| `server/store/client.go` | Implement channel init/delete + team delete methods |
| `server/store/caching.go` | Add channel init cache, teardown cache invalidation |
| `server/store/caching_test.go` | Add channel cache + teardown tests |
| `server/model/post_message.go` | New: `PostMessage`, `DeleteMessage`, `ReactionMessage` structs + type constants |
| `server/service.go` | Add init/teardown methods for channel and team |
| `server/command.go` | Add `init-channel`, `teardown-channel`, `teardown-team` subcommands |
| `server/api.go` | Add init/teardown endpoints for channels and teams |
| `server/plugin.go` | Add `ctx`, `cancel`, `wg`, `relaySem` fields; update lifecycle |
| `server/nats.go` | Add `publishToOutbound()`, message builder functions, relay constants |
| `server/hooks.go` | New: all 5 hooks with shared `isChannelRelayEnabled` + `relayToOutbound` helpers |

## Tasks

### Task 1: Extend KV Store (store.go, client.go)

**store.go** - Add to interface:
```go
GetChannelInitialized(channelID string) (bool, error)
SetChannelInitialized(channelID string) error
DeleteChannelInitialized(channelID string) error
DeleteTeamInitialized(teamID string) error
RemoveInitializedTeamID(teamID string) error
```

**client.go**:
- Add field `channelInitPrefix: pluginID + "-channelinit-"` to `Client` struct
- Implement `GetChannelInitialized` and `SetChannelInitialized` following the team init pattern
- Implement `DeleteChannelInitialized(channelID)`: delete key `channelInitPrefix + channelID` via `kv.client.KV.Delete()`
- Implement `DeleteTeamInitialized(teamID)`: delete key `teamInitPrefix + teamID` via `kv.client.KV.Delete()`
- Implement `RemoveInitializedTeamID(teamID)`: use `casModifyStringList` to atomically remove teamID from the initialized teams list (filter it out instead of appending)

### Task 2: Add Caching for Channel Init + Teardown (caching.go)

Add cluster event constant:
```go
ClusterEventInvalidateChannelInit = "cache_inv_chaninit"
```

Add cache field to `CachingKVStore`:
```go
channelInitCache *expirable.LRU[string, bool]  // size 64, same TTL
```

Implement caching wrappers following team init cache pattern:
- `GetChannelInitialized` / `SetChannelInitialized` (same as team pattern)
- `DeleteChannelInitialized`: call inner store, then invalidate channel init cache + publish cluster event
- `DeleteTeamInitialized`: call inner store, then invalidate team init cache (`ClusterEventInvalidateTeamInit`) + publish cluster event
- `RemoveInitializedTeamID`: call inner store, then invalidate init teams list cache (`ClusterEventInvalidateInitTeams`) + publish cluster event

Update `removeFromCache`, `NewCachingKVStore`.

### Task 3: Add Message Models (model/post_message.go)

```go
const (
    MessageTypePost           = "crossguard_post"
    MessageTypeUpdate         = "crossguard_update"
    MessageTypeDelete         = "crossguard_delete"
    MessageTypeReactionAdd    = "crossguard_reaction_add"
    MessageTypeReactionRemove = "crossguard_reaction_remove"
)

// PostMessage is used for both new posts (MessageTypePost) and updates (MessageTypeUpdate).
// For updates, the fields reflect the post's current state after the edit.
type PostMessage struct {
    PostID      string `json:"post_id"`
    RootID      string `json:"root_id,omitempty"`
    ChannelID   string `json:"channel_id"`
    ChannelName string `json:"channel_name"`
    TeamID      string `json:"team_id"`
    TeamName    string `json:"team_name"`
    UserID      string `json:"user_id"`
    Username    string `json:"username"`
    Message     string `json:"message"`
    CreateAt    int64  `json:"create_at"`
}

// DeleteMessage identifies a post that was deleted.
type DeleteMessage struct {
    PostID      string `json:"post_id"`
    ChannelID   string `json:"channel_id"`
    ChannelName string `json:"channel_name"`
    TeamID      string `json:"team_id"`
    TeamName    string `json:"team_name"`
}

// ReactionMessage represents a reaction added or removed from a post.
type ReactionMessage struct {
    PostID      string `json:"post_id"`
    ChannelID   string `json:"channel_id"`
    ChannelName string `json:"channel_name"`
    TeamID      string `json:"team_id"`
    TeamName    string `json:"team_name"`
    UserID      string `json:"user_id"`
    Username    string `json:"username"`
    EmojiName   string `json:"emoji_name"`
}
```

`PostMessage` is reused for both `MessageTypePost` and `MessageTypeUpdate`. The envelope's `Type` field distinguishes them. `ReactionMessage` is reused for both `MessageTypeReactionAdd` and `MessageTypeReactionRemove`.

### Task 4: Add Service Methods (service.go)

**`initChannelForCrossGuard(user *model.User, channelID string) (*model.Channel, *apiError)`**:
1. `p.API.GetChannel(channelID)` - verify channel exists
2. `p.kvstore.GetTeamInitialized(channel.TeamId)` - verify team is initialized, return error if not
3. `p.kvstore.GetChannelInitialized(channelID)` - return early if already initialized (idempotent)
4. `p.kvstore.SetChannelInitialized(channelID)` - mark as initialized
5. Post bot announcement to the channel: "Cross Guard relay enabled for this channel by @username."

**`teardownChannelForCrossGuard(user *model.User, channelID string) (*model.Channel, *apiError)`**:
1. `p.API.GetChannel(channelID)` - verify channel exists
2. `p.kvstore.GetChannelInitialized(channelID)` - if not initialized, return early (idempotent)
3. `p.kvstore.DeleteChannelInitialized(channelID)` - remove init flag
4. Post bot announcement: "Cross Guard relay disabled for this channel by @username."

**`teardownTeamForCrossGuard(user *model.User, teamID string) (*model.Team, *apiError)`**:
1. `p.API.GetTeam(teamID)` - verify team exists
2. `p.kvstore.GetTeamInitialized(teamID)` - if not initialized, return early (idempotent)
3. `p.kvstore.DeleteTeamInitialized(teamID)` - remove init flag
4. `p.kvstore.RemoveInitializedTeamID(teamID)` - remove from global list
5. Post bot announcement to town-square: "Cross Guard disabled for this team by @username. All channel relays in this team are now inactive."

### Task 5: Add Slash Commands (command.go)

- Update `AutoCompleteHint` to `"[init-team|init-channel|teardown-team|teardown-channel|status]"`
- Add all new subcommands to `getAutocompleteData()`
- Add cases in `ExecuteCommand` switch: `init-channel`, `teardown-channel`, `teardown-team`

**Permission helper** - Add `isChannelAdminOrHigher(userID, channelID, teamID) bool`:
1. Check `p.API.GetChannelMember(channelID, userID)` to verify channel membership (return false if not a member)
2. If channel member has `SchemeAdmin` role, return true
3. Fall back to `isTeamAdminOrSystemAdmin(userID, teamID)` (team admins and system admins who are channel members can also init/teardown)

**Handlers**:
- `executeInitChannel(args)`: check `isChannelAdminOrHigher`, get user, call `initChannelForCrossGuard(user, args.ChannelId)`
- `executeTeardownChannel(args)`: check `isChannelAdminOrHigher`, get user, call `teardownChannelForCrossGuard(user, args.ChannelId)`
- `executeTeardownTeam(args)`: check `isTeamAdminOrSystemAdmin`, get user, call `teardownTeamForCrossGuard(user, args.TeamId)`

**Error messages**:
- Channel commands: "You must be a member of this channel and a channel admin, team admin, or system admin."
- Team teardown: "You don't have permissions to run this command. You must be a team admin or system admin." (same as init-team)

### Task 6: Add REST API Endpoints (api.go)

Add routes in `initAPI()`:
```go
router.HandleFunc("/api/v1/channels/{channel_id}/init", p.handleInitChannel).Methods(http.MethodPost)
router.HandleFunc("/api/v1/channels/{channel_id}/teardown", p.handleTeardownChannel).Methods(http.MethodPost)
router.HandleFunc("/api/v1/teams/{team_id}/teardown", p.handleTeardownTeam).Methods(http.MethodPost)
```

**`handleInitChannel`** - following the `handleInitTeam` pattern:
1. `getAuthenticatedUser(w, r)` - authenticate
2. Extract `channel_id` from `mux.Vars(r)`, validate with `model.IsValidId()`
3. Get the channel via `p.API.GetChannel(channelID)` to find the team ID
4. Check `isChannelAdminOrHigher(user.Id, channelID, channel.TeamId)` - return 403 if not authorized
5. Call `p.initChannelForCrossGuard(user, channelID)`
6. Return `{"status": "ok", "channel_id": "...", "channel_name": "..."}` on success

**`handleTeardownChannel`** - same auth/validation pattern:
1. Authenticate, extract/validate `channel_id`, get channel for team ID
2. Check `isChannelAdminOrHigher` - return 403 if not authorized
3. Call `p.teardownChannelForCrossGuard(user, channelID)`
4. Return `{"status": "ok", "channel_id": "...", "channel_name": "..."}`

**`handleTeardownTeam`** - same pattern as `handleInitTeam`:
1. Authenticate, extract/validate `team_id`
2. Check `isTeamAdminOrSystemAdmin` - return 403 if not authorized
3. Call `p.teardownTeamForCrossGuard(user, teamID)`
4. Return `{"status": "ok", "team_id": "...", "team_name": "..."}`

CLI usage examples:
```bash
# Init channel
curl -X POST http://low.test:8075/plugins/crossguard/api/v1/channels/{channel_id}/init \
  -H "Authorization: Bearer <token>"

# Teardown channel
curl -X POST http://low.test:8075/plugins/crossguard/api/v1/channels/{channel_id}/teardown \
  -H "Authorization: Bearer <token>"

# Teardown team
curl -X POST http://low.test:8075/plugins/crossguard/api/v1/teams/{team_id}/teardown \
  -H "Authorization: Bearer <token>"
```

### Task 7: Add Plugin Lifecycle (plugin.go)

Add to `Plugin` struct:
```go
ctx       context.Context
cancel    context.CancelFunc
wg        sync.WaitGroup
relaySem  chan struct{}      // semaphore limiting concurrent relay goroutines
outboundMu sync.RWMutex     // protects outboundConns
outboundConns []outboundConn // persistent NATS connections for relay
```

New type for the pool entry:
```go
type outboundConn struct {
    nc      *nats.Conn
    subject string
    name    string
}
```

In `OnActivate()` (after existing init):
```go
p.ctx, p.cancel = context.WithCancel(context.Background())
p.relaySem = make(chan struct{}, 50)
p.connectOutbound() // initial connection using current config
```

In `OnDeactivate()`:
```go
if p.cancel != nil {
    p.cancel()
}
p.wg.Wait()
p.closeOutbound()
return nil
```

Update `OnConfigurationChange()` (in configuration.go): after `p.setConfiguration(cfg)`, call `p.reconnectOutbound()` to close old connections and open new ones based on the updated config. Guard with a nil check on `p.API` so it does not run before `OnActivate()`.

### Task 8: Add NATS Relay Logic (nats.go)

Constants:
```go
const (
    natsRelayConnectTimeout = 5 * time.Second
    natsPublishMaxRetries   = 3
    natsPublishBaseDelay    = 500 * time.Millisecond
    natsPublishMaxDelay     = 5 * time.Second
    natsMaxReconnects       = -1  // unlimited reconnects
    natsReconnectWait       = 2 * time.Second
)
```

**Connection pool management**:

`connectOutbound()`:
- Read current config, parse outbound connections
- For each connection, call `connectNATSPersistent(conn)` which uses `connectNATS` options plus:
  - `nats.MaxReconnects(natsMaxReconnects)` - unlimited auto-reconnect
  - `nats.ReconnectWait(natsReconnectWait)` - 2s between reconnect attempts
  - `nats.DisconnectErrHandler` - log warning with connection name
  - `nats.ReconnectHandler` - log info with connection name
  - `nats.Timeout(natsRelayConnectTimeout)` - 5s connect timeout
- Store each `outboundConn{nc, conn.Subject, conn.Name}` in `p.outboundConns` under `p.outboundMu` write lock
- Log errors for connections that fail to establish but continue with others (non-fatal)

`closeOutbound()`:
- Acquire `p.outboundMu` write lock
- Drain and close each `nc` in `p.outboundConns` (drain flushes pending publishes)
- Set `p.outboundConns` to nil

`reconnectOutbound()`:
- Call `closeOutbound()` then `connectOutbound()`
- Called from `OnConfigurationChange()` when config is updated

**Message builders** (all return `([]byte, error)`, unchanged):

`buildPostEnvelope(msgType string, post *mmModel.Post, channel *mmModel.Channel, teamName string, username string) ([]byte, error)`:
- Create `model.PostMessage` from post fields + channel.TeamId + channel.Name + teamName + post.RootId + username
- Wrap in `model.NewMessage(msgType, postMsg)` (msgType is `MessageTypePost` or `MessageTypeUpdate`)
- Marshal and return
- Used by both MessageHasBeenPosted and MessageHasBeenUpdated

`buildDeleteEnvelope(post *mmModel.Post, channel *mmModel.Channel, teamName string) ([]byte, error)`:
- Create `model.DeleteMessage{PostID: post.Id, ChannelID: post.ChannelId, ChannelName: channel.Name, TeamID: channel.TeamId, TeamName: teamName}`
- Wrap in `model.NewMessage(model.MessageTypeDelete, deleteMsg)`
- Marshal and return

`buildReactionEnvelope(msgType string, reaction *mmModel.Reaction, channel *mmModel.Channel, teamName string, username string) ([]byte, error)`:
- Create `model.ReactionMessage` from reaction fields + channel.TeamId + channel.Name + teamName + username
- Wrap in `model.NewMessage(msgType, reactionMsg)` (msgType is `MessageTypeReactionAdd` or `MessageTypeReactionRemove`)
- Marshal and return

**Publisher**:

`publishToOutbound(ctx context.Context, data []byte)`:
- Acquire `p.outboundMu` read lock, snapshot `p.outboundConns`
- If none, return silently
- For each `outboundConn`:
  - Check `oc.nc.IsConnected()` or `oc.nc.IsReconnecting()` (skip if neither, log warning)
  - Publish to `oc.subject`, then flush
  - On failure: retry with exponential backoff (`baseDelay * 2^attempt`, capped at `maxDelay`)
  - Between retries: `select` on `ctx.Done()` to respect shutdown
  - Log error on final failure with connection name and error
  - Do NOT close the connection (it is persistent and shared)

### Task 9: Implement All Hooks (hooks.go)

New file `server/hooks.go` with shared helpers and 5 hook implementations.

**Shared helper: `isChannelRelayEnabled(channelID string) (*model.Channel, *model.Team, bool)`**

Consolidates the channel+team init checks used by all hooks. Returns both channel and team so callers have access to names for the envelope:
```go
func (p *Plugin) isChannelRelayEnabled(channelID string) (*model.Channel, *model.Team, bool) {
    initialized, err := p.kvstore.GetChannelInitialized(channelID)
    if err != nil {
        p.API.LogError("Failed to check channel init status", "channel_id", channelID, "error", err.Error())
        return nil, nil, false
    }
    if !initialized {
        return nil, nil, false
    }

    channel, appErr := p.API.GetChannel(channelID)
    if appErr != nil {
        p.API.LogError("Failed to get channel for relay", "channel_id", channelID, "error", appErr.Error())
        return nil, nil, false
    }

    teamInit, err := p.kvstore.GetTeamInitialized(channel.TeamId)
    if err != nil {
        p.API.LogError("Failed to check team init status", "team_id", channel.TeamId, "error", err.Error())
        return nil, nil, false
    }
    if !teamInit {
        return nil, nil, false
    }

    team, appErr := p.API.GetTeam(channel.TeamId)
    if appErr != nil {
        p.API.LogError("Failed to get team for relay", "team_id", channel.TeamId, "error", appErr.Error())
        return nil, nil, false
    }

    return channel, team, true
}
```

**Shared helper: `relayToOutbound(data []byte, logContext string)`**

Acquires semaphore and spawns goroutine:
```go
func (p *Plugin) relayToOutbound(data []byte, logContext string) {
    select {
    case p.relaySem <- struct{}{}:
    default:
        p.API.LogWarn("Relay semaphore full, dropping event", "context", logContext)
        return
    }

    p.wg.Add(1)
    go func() {
        defer p.wg.Done()
        defer func() { <-p.relaySem }()
        p.publishToOutbound(p.ctx, data)
    }()
}
```

**Hook 1: `MessageHasBeenPosted(_ *plugin.Context, post *model.Post)`**
1. Skip if `post.IsSystemMessage()` or `post.UserId == p.botUserID` or `post.GetProp("crossguard_relayed") != nil`
2. `channel, team, ok := p.isChannelRelayEnabled(post.ChannelId)` - return if not enabled
3. Look up user for username
4. `data := buildPostEnvelope(model.MessageTypePost, post, channel, team.Name, user.Username)`
5. `p.relayToOutbound(data, "post:"+post.Id)`

**Hook 2: `MessageHasBeenUpdated(_ *plugin.Context, newPost *model.Post, _ *model.Post)`**
1. Same skip checks as MessageHasBeenPosted (system, bot, relayed prop)
2. `channel, team, ok := p.isChannelRelayEnabled(newPost.ChannelId)` - return if not enabled
3. Look up user for username
4. `data := buildPostEnvelope(model.MessageTypeUpdate, newPost, channel, team.Name, user.Username)`
5. `p.relayToOutbound(data, "update:"+newPost.Id)`

**Hook 3: `MessageHasBeenDeleted(_ *plugin.Context, post *model.Post)`**
- Note: Mattermost's `MessageHasBeenDeleted` hook receives the post that was deleted. The post object still has ChannelId populated.
1. Skip if `post.UserId == p.botUserID`
2. `channel, team, ok := p.isChannelRelayEnabled(post.ChannelId)` - return if not enabled
3. `data := buildDeleteEnvelope(post, channel, team.Name)`
4. `p.relayToOutbound(data, "delete:"+post.Id)`
- No user lookup needed (DeleteMessage only has post/channel/team IDs and names)

**Hook 4: `ReactionHasBeenAdded(_ *plugin.Context, reaction *model.Reaction)`**
1. Look up the post via `p.API.GetPost(reaction.PostId)` to get the channel ID
2. Skip if post is system message or bot post
3. `channel, team, ok := p.isChannelRelayEnabled(post.ChannelId)` - return if not enabled
4. Look up reaction user for username
5. `data := buildReactionEnvelope(model.MessageTypeReactionAdd, reaction, channel, team.Name, user.Username)`
6. `p.relayToOutbound(data, "reaction_add:"+reaction.PostId)`

**Hook 5: `ReactionHasBeenRemoved(_ *plugin.Context, reaction *model.Reaction)`**
1. Same flow as ReactionHasBeenAdded
2. Look up post, check channel relay, look up user
3. `data := buildReactionEnvelope(model.MessageTypeReactionRemove, reaction, channel, team.Name, user.Username)`
4. `p.relayToOutbound(data, "reaction_remove:"+reaction.PostId)`

### Task 10: Tests

- `server/store/caching_test.go`: Add tests for channel init cache (get miss, get hit, set invalidates, delete invalidates, cluster event)
- `server/model/post_message_test.go`: Round-trip serialization of PostMessage, DeleteMessage, and ReactionMessage through envelope
- `server/hooks_test.go`: Test all hooks:
  - Shared: skip bot, skip system, skip non-initialized channel, skip non-initialized team, skip relayed prop
  - MessageHasBeenPosted: relay for initialized channel+team
  - MessageHasBeenUpdated: relay updated post
  - MessageHasBeenDeleted: relay delete with minimal payload
  - ReactionHasBeenAdded: relay reaction add
  - ReactionHasBeenRemoved: relay reaction remove

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| NATS server restarts | nats.go auto-reconnect with unlimited retries and 2s wait. Handlers log disconnect/reconnect. |
| Config change during active publishes | RWMutex: publishers hold read lock, reconnect holds write lock. Drain flushes pending before close. |
| Connection fails on startup | Non-fatal: log error, continue with other connections. Hooks skip disconnected connections. |
| Goroutine explosion under load | Semaphore (50 concurrent). Drop messages at capacity with warning log. |
| Relay loops between servers | Skip bot posts + check `crossguard_relayed` post prop. Defense-in-depth. |
| Message ordering not guaranteed | Accepted for MVP. Per-channel queue is a follow-up if needed. |
| Plugin deactivation with in-flight relays | ctx.Cancel() + wg.Wait() + closeOutbound() (drain) in OnDeactivate |

## Out of Scope

- Inbound subscription (receiving side)
- Channel status display in `/crossguard status`
- File attachment relay

## Verification

1. `make check-style` passes
2. `make test` passes
3. `make deploy` to docker environment
4. Run `/crossguard init-team` then `/crossguard init-channel` in a channel
5. Post a message, verify NATS publish with type `crossguard_post` in logs
6. Edit a message, verify NATS publish with type `crossguard_update`
7. Add a reaction, verify NATS publish with type `crossguard_reaction_add`
8. Remove a reaction, verify NATS publish with type `crossguard_reaction_remove`
9. Delete a message, verify NATS publish with type `crossguard_delete`
10. Verify bot posts are not relayed (no loop)
11. Verify posts in non-initialized channels are not relayed
12. Run `/crossguard teardown-channel`, post a message, verify no NATS publish
13. Run `/crossguard init-channel` again, verify relay resumes
14. Run `/crossguard teardown-team`, verify all channels in that team stop relaying
15. Run `/crossguard init-team` again, verify channels resume relaying (channel init flags preserved)
16. Test all REST API endpoints via curl
