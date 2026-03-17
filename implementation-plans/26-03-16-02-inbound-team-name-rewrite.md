# Inbound Team Name Rewrite

## Context

When the Cross Guard plugin receives inbound messages via NATS, `resolveTeamAndChannel()` (`server/inbound.go:122`) calls `p.API.GetTeamByName(teamName)` using the team name from the remote server's message envelope. If the remote team name differs from the local team name, inbound messages fail with "team not found." Admins need a way to map remote team names to local teams on a per-connection basis.

## Problem Statement

Remote servers may use different team names than the local server. There is no way to tell the plugin "when connection X sends messages for team Y, route them to local team Z."

## Current State

- `resolveTeamAndChannel()` does a direct `GetTeamByName` with no rewrite capability
- Team connections are stored per-team as `[]string` in KV store (e.g., `["inbound:high", "outbound:high"]`)
- Channel connections follow the same `[]string` pattern
- The frontend `parseConnection()` splits on `:` to extract direction and name
- The `CachingKVStore` (`server/store/caching.go`) provides LRU caching with TTL and cluster invalidation

### Current Gaps
- No mechanism to map remote team names to local teams
- Connection data stored as flat strings with no room for metadata

## Design Principles

| Pattern | Our Approach | Avoid | Reference |
|---------|-------------|-------|-----------|
| Storage | Replace `[]string` with `[]TeamConnection` struct | Separate rewrite KV keys, string parsing hacks | `store/store.go` interface |
| Backwards compat | None, clean break | Migration code, dual-format support | User request: "lets not worry about backwards compatibility" |
| Caching | Existing `CachingKVStore` LRU pattern unchanged | Custom sync.RWMutex map on Plugin struct | `caching.go:25-42` |
| Struct design | Go-serializable struct with 3 fields | String concatenation with delimiters | `store/store.go:9-12` ConnectionPrompt pattern |
| Rewrite lookup | Reverse index KV key for O(1) lookup on hot path | Scanning all teams per inbound message | `store/client.go` existing KV patterns |

## Reference Patterns

- `store/store.go:9-12` - `ConnectionPrompt` struct pattern (Go struct in KV store)
- `store/client.go:338-369` - CAS retry pattern for list modification
- `server/service.go:506-528` - `getAllConnectionNames()` builds `direction:name` strings
- `server/service.go:241-245` - `ConnectionStatus` struct
- `server/inbound.go:122-153` - `resolveTeamAndChannel()` where rewrite is applied
- `webapp/src/components/CrossguardTeamModal.tsx:29-38` - `parseConnection()` for direction detection

## Out of Scope

- Channel name rewriting (only team names for now)
- Backwards compatibility or migration from old `[]string` format
- Separate `/crossguard set-rewrite` subcommand (replaced by `rewrite-team`)

## Technical Approach

### New Struct: `TeamConnection`

Replace the `[]string` connection storage with a typed struct:

```go
// TeamConnection represents a single connection linked to a team or channel.
type TeamConnection struct {
    Direction      string `json:"direction"`        // "inbound" or "outbound"
    Connection     string `json:"connection"`        // raw connection name (e.g., "high", "low-to-high")
    RemoteTeamName string `json:"remote_team_name,omitempty"` // remote team name for inbound rewrites
}
```

This struct lives in `server/store/store.go` alongside `ConnectionPrompt`.

### KVStore Interface Changes

All `[]string` methods become `[]TeamConnection`:

```go
type KVStore interface {
    GetTeamConnections(teamID string) ([]TeamConnection, error)
    SetTeamConnections(teamID string, conns []TeamConnection) error
    DeleteTeamConnections(teamID string) error
    IsTeamInitialized(teamID string) (bool, error)
    AddTeamConnection(teamID string, conn TeamConnection) error
    RemoveTeamConnection(teamID string, conn TeamConnection) error
    // ... other methods unchanged ...
    GetChannelConnections(channelID string) ([]TeamConnection, error)
    SetChannelConnections(channelID string, conns []TeamConnection) error
    DeleteChannelConnections(channelID string) error
    IsChannelInitialized(channelID string) (bool, error)
    AddChannelConnection(channelID string, conn TeamConnection) error
    RemoveChannelConnection(channelID string, conn TeamConnection) error
    // ... rest unchanged ...
}
```

### Matching Logic

`RemoveTeamConnection` and `RemoveChannelConnection` match on `Direction` + `Connection` only (ignore `RemoteTeamName` for removal):

```go
func (tc TeamConnection) Matches(other TeamConnection) bool {
    return tc.Direction == other.Direction && tc.Connection == other.Connection
}
```

### Reverse Index Cleanup on Removal

`RemoveTeamConnection` must read the existing connection list before removal to find the `RemoteTeamName` on the matching entry. If the matched connection has a non-empty `RemoteTeamName`, delete the corresponding reverse index key before removing the connection from the list. This ensures teardown does not leave orphaned reverse index entries.

### Write Ordering for Rewrite Operations

When setting a rewrite, write the reverse index first, then update the `TeamConnection` list. If the index write succeeds but the list write fails, the stale index points to a valid team (safe, message still routes). The reverse case (list updated but index missing) would silently drop messages with no indication of misconfiguration.

When clearing a rewrite or removing a connection, update the `TeamConnection` list first, then delete the reverse index. A stale index after partial failure is safe (resolves to a team where the connection is no longer linked, so the connection-linked check rejects the message).

### Rewrite Reverse Index

A reverse index provides O(1) lookup on the inbound hot path instead of scanning all initialized teams per message.

**KV key format:**

```
Key:   {pluginID}-rwi-{connName}::{remoteTeamName}   (e.g., "crossguard-rwi-low-to-high::test-a")
Value: string (local team ID)
```

The `::` delimiter is used because neither Mattermost team slugs (`[a-z0-9-]`) nor connection names contain colons, avoiding ambiguity when connection names contain hyphens (e.g., `low-to-high`).

The reverse index is maintained as a side effect of setting/clearing `RemoteTeamName` on a `TeamConnection`:
- When `RemoteTeamName` is set on a connection for a team, write the reverse index key
- When `RemoteTeamName` is cleared or the connection is removed, delete the reverse index key

**Uniqueness constraint:** Before writing a reverse index entry, check if one already exists for a different team. If so, reject the operation with an error: "Remote team name 'X' on connection 'Y' is already mapped to team [other-team-name]. Clear that rewrite first." This prevents two teams from silently competing for the same (connName, remoteTeamName) mapping.

**KVStore interface additions:**

```go
GetTeamRewriteIndex(connName, remoteTeamName string) (string, error)   // returns local teamID or ""
SetTeamRewriteIndex(connName, remoteTeamName, localTeamID string) error
DeleteTeamRewriteIndex(connName, remoteTeamName string) error
```

These are simple single-value KV get/set/delete operations (no lists, no CAS). `SetTeamRewriteIndex` must first check for an existing entry pointing to a different team and return an error if found. The `CachingKVStore` wraps them with an LRU cache keyed on `connName + "::" + remoteTeamName` and cluster invalidation.

### Inbound Processing Change

`resolveTeamAndChannel` receives the remote `teamName` and needs to find the local team. The flow becomes:

1. First, try `GetTeamByName(teamName)` as before (handles matching names)
2. If not found, do a single KV lookup via the reverse index: `GetTeamRewriteIndex(connName, teamName)`
3. If found, use `GetTeam(localTeamID)` to resolve the local team

```go
func (p *Plugin) findTeamByRewrite(connName, remoteTeamName string) (*model.Team, error) {
    localTeamID, err := p.kvstore.GetTeamRewriteIndex(connName, remoteTeamName)
    if err != nil {
        return nil, err
    }
    if localTeamID == "" {
        return nil, nil
    }
    team, appErr := p.API.GetTeam(localTeamID)
    if appErr != nil {
        return nil, fmt.Errorf("rewrite target team %s not found: %w", localTeamID, appErr)
    }
    return team, nil
}
```

### Service Layer Changes

`getAllConnectionNames()` now returns `[]TeamConnection` instead of `[]string`:

```go
func (p *Plugin) getAllConnectionNames() []TeamConnection {
    cfg := p.getConfiguration()
    outbound, _ := cfg.GetOutboundConnections()
    inbound, _ := cfg.GetInboundConnections()

    var conns []TeamConnection
    for _, conn := range outbound {
        conns = append(conns, TeamConnection{Direction: "outbound", Connection: conn.Name})
    }
    for _, conn := range inbound {
        conns = append(conns, TeamConnection{Direction: "inbound", Connection: conn.Name})
    }
    return conns
}
```

`ConnectionStatus` gains `RemoteTeamName`:

```go
type ConnectionStatus struct {
    Name           string `json:"name"`
    Direction      string `json:"direction"`
    Linked         bool   `json:"linked"`
    Orphaned       bool   `json:"orphaned,omitempty"`
    RemoteTeamName string `json:"remote_team_name,omitempty"`
}
```

`getTeamStatus()` and `getChannelStatus()` build statuses from `[]TeamConnection` instead of `[]string`. The `LinkedConnections` field on `TeamStatusResponse` and `TeamStatusEntry` changes from `[]string` to `[]TeamConnection`.

### API Changes

The existing `POST /api/v1/teams/{team_id}/init` and similar endpoints pass `TeamConnection` structs instead of string connection names. The rewrite is set by including `remote_team_name` in the init request body or via a dedicated set-rewrite endpoint:

```
POST   /api/v1/teams/{team_id}/rewrite
  Body: {"connection": "low-to-high", "remote_team_name": "remote-team-name"}
  Response: {"status": "ok", "team_id": "...", "connection": "...", "remote_team_name": "..."}

DELETE /api/v1/teams/{team_id}/rewrite?connection=low-to-high
  Response: {"status": "ok", "team_id": "...", "connection": "..."}
```

Both handlers require system admin or team admin permissions (same as init endpoints). They validate that the specified connection is an inbound connection linked to this team, returning 400 if not found or not inbound. The set-rewrite handler checks the reverse index uniqueness constraint before writing. The set-rewrite handler finds the matching inbound `TeamConnection` in the team's connections list, sets its `RemoteTeamName`, and calls `SetTeamConnections()`. The delete handler clears `RemoteTeamName`.

### Slash Command: `/crossguard rewrite-team`

```
/crossguard rewrite-team [connection-name] [remote-team-name]
```

Sets the `RemoteTeamName` on an inbound connection linked to the current team. This tells the plugin "when inbound messages arrive from `connection-name` with team name `remote-team-name`, route them to this local team."

**Behavior:**
- Requires team admin or system admin permissions (same as `init-team`)
- Validates that `connection-name` is an inbound connection linked to this team
- If `connection-name` is omitted and only one inbound connection is linked, auto-selects it
- If `remote-team-name` is omitted, clears the existing rewrite (removes `RemoteTeamName`)
- Updates the `TeamConnection` in the team's connection list and maintains the reverse index
- Posts an audit message to the team's Town Square channel

**Implementation in `command.go`:**

```go
const actionRewriteTeam = "rewrite-team"

func (p *Plugin) executeRewriteTeam(args *model.CommandArgs) *model.CommandResponse {
    if !p.isTeamAdminOrSystemAdmin(args.UserId, args.TeamId) {
        return respondEphemeral("You must be a team admin or system admin.")
    }

    parts := strings.Fields(args.Command)
    // /crossguard rewrite-team [connection-name] [remote-team-name]

    conns, err := p.kvstore.GetTeamConnections(args.TeamId)
    if err != nil {
        return respondEphemeral("Failed to check team connections.")
    }

    // Filter to inbound-only connections linked to this team
    var inboundConns []TeamConnection
    for _, tc := range conns {
        if tc.Direction == "inbound" {
            inboundConns = append(inboundConns, tc)
        }
    }
    if len(inboundConns) == 0 {
        return respondEphemeral("No inbound connections are linked to this team.")
    }

    // Resolve connection name
    connName := ""
    if len(parts) >= 3 {
        connName = parts[2]
    }
    // ... resolve against inboundConns, auto-select if only one ...

    remoteTeamName := ""
    if len(parts) >= 4 {
        remoteTeamName = parts[3]
    }

    // Call service layer to set/clear rewrite + update reverse index
    // ...
}
```

**Autocomplete:**

```go
rewriteTeam := model.NewAutocompleteData("rewrite-team", "[connection-name] [remote-team-name]",
    "Set or clear a remote team name rewrite for an inbound connection on this team")
cmd.AddCommand(rewriteTeam)
```

**Usage examples:**
- `/crossguard rewrite-team low-to-high test-a` -- route inbound messages from `low-to-high` with team name `test-a` to this local team
- `/crossguard rewrite-team low-to-high` -- clear the rewrite for `low-to-high` on this team
- `/crossguard rewrite-team` -- auto-selects if only one inbound connection, then clears its rewrite

### Frontend Changes

`parseConnection()` is no longer needed. The API now returns structured `ConnectionStatus` with `direction` and `name` as separate fields.

In `CrossguardTeamModal.tsx`:
- Remove `parseConnection()` function
- Use `conn.direction` and `conn.name` directly from the API response
- For inbound connections, show `remote_team_name` and allow editing via the rewrite endpoints

In `CrossguardChannelModal.tsx`:
- Remove `parseConnection()` function
- Use `conn.direction` and `conn.name` directly from the API response
- No rewrite editing here (remote_team_name is a team-level concern only)

### NATS Layer Changes

`server/nats.go:316` currently does `slices.Contains(connNames, "outbound:"+outboundName)`. This changes to iterate `[]TeamConnection` and match on `Direction == "outbound" && Connection == outboundName`.

## Decisions

| Question | Decision | Rationale |
|----------|----------|-----------|
| Storage format? | `[]TeamConnection` struct in existing KV keys | Embeds rewrite data with connection, no separate keys needed |
| Backwards compat? | None | User-specified, clean break simplifies code |
| Matching for removal? | Direction + Connection only | RemoteTeamName is metadata, not identity |
| Rewrite lookup? | Reverse index KV key for O(1) lookup | Hot path (every inbound message), must not scan |
| Reverse index maintenance? | Side effect of set/clear RemoteTeamName | Separate admin action, keeps write path simple |
| Separate rewrite KV keys? | Only reverse index keys (`rwi-` prefix) | Struct holds the source of truth, index is derived |

## Files to Modify

| File | Change |
|------|--------|
| `server/store/store.go` | Add `TeamConnection` struct with `Matches()` method, change all `[]string` params to `[]TeamConnection` in KVStore interface, add `GetTeamRewriteIndex`/`SetTeamRewriteIndex`/`DeleteTeamRewriteIndex` |
| `server/store/client.go` | Update `GetTeamConnections`/`SetTeamConnections`/`AddTeamConnection`/`RemoveTeamConnection` (and channel equivalents) to use `[]TeamConnection`, rename `casModifyStringList` to `casModifyConnectionList` with `[]TeamConnection` types, implement reverse index get/set/delete with `rewriteIndexPrefix` |
| `server/store/caching.go` | Update cache types from `[]string` to `[]TeamConnection`, update all wrapper methods, add `rewriteIndexCache` LRU with cluster invalidation for reverse index |
| `server/store/caching_test.go` | Update all test data from strings to `TeamConnection` structs |
| `server/service.go` | Update `ConnectionStatus`, `TeamStatusResponse`, `TeamStatusEntry`, `getAllConnectionNames()`, `getTeamStatus()`, `getChannelStatus()`, `initTeamForCrossGuard()`, `teardownTeamForCrossGuard()`, `initChannelForCrossGuard()`, `teardownChannelForCrossGuard()`, `resolveConnectionName()` |
| `server/service_test.go` | Update test data to use `TeamConnection` structs |
| `server/inbound.go` | Add `findTeamByRewrite()` using reverse index lookup, modify `resolveTeamAndChannel()` to fall back to rewrite on name-not-found |
| `server/inbound_test.go` | Update `testKVStore` to return `[]TeamConnection`, add rewrite resolution tests |
| `server/nats.go` | Update connection matching from string contains to struct field comparison |
| `server/nats_test.go` | Update test data to use `TeamConnection` structs |
| `server/api.go` | Add `handleSetTeamRewrite` (POST) and `handleDeleteTeamRewrite` (DELETE) endpoints |
| `server/command.go` | Update `executeInitTeam()` to build `TeamConnection` struct, add `executeRewriteTeam()` subcommand with autocomplete, update `ExecuteCommand` switch and usage hints |
| `server/prompt.go` | Update connection name building from string concat to `TeamConnection` struct |
| `webapp/src/components/CrossguardTeamModal.tsx` | Remove `parseConnection()`, use structured API response, add rewrite edit controls |
| `webapp/src/components/CrossguardChannelModal.tsx` | Remove `parseConnection()`, use structured API response |

## Tasks

1. [ ] **Store struct + interface**: Add `TeamConnection` struct with `Matches()` to `store.go`, change all `[]string` connection params to `[]TeamConnection` in `KVStore` interface, add `GetTeamRewriteIndex`/`SetTeamRewriteIndex`/`DeleteTeamRewriteIndex`
2. [ ] **Store implementation**: Update `client.go` to serialize/deserialize `[]TeamConnection` JSON, rename `casModifyStringList` to `casModifyConnectionList` with `[]TeamConnection` types, update Add/Remove to use `Matches()`, implement reverse index get/set/delete with `rewriteIndexPrefix` and `::` delimiter, add uniqueness check in `SetTeamRewriteIndex`. `RemoveTeamConnection` must read the existing connection's `RemoteTeamName` before removal and delete the corresponding reverse index key.
3. [ ] **Caching layer**: Update `caching.go` cache types and wrapper methods for `[]TeamConnection`, add `rewriteIndexCache` LRU with cluster invalidation
4. [ ] **Service layer**: Update all service functions, `ConnectionStatus`, `TeamStatusResponse`, `TeamStatusEntry`, `getAllConnectionNames()`, status builders, init/teardown functions. Maintain reverse index as side effect when setting/clearing `RemoteTeamName` and when tearing down connections.
5. [ ] **Inbound processing**: Add `findTeamByRewrite()` using reverse index O(1) lookup, modify `resolveTeamAndChannel()` to fall back to rewrite when team name not found
6. [ ] **NATS layer**: Update `nats.go` connection matching from string to struct field comparison
7. [ ] **Slash command**: Update `command.go` to build `TeamConnection` structs, add `rewrite-team` subcommand (`executeRewriteTeam`), update `ExecuteCommand` switch, autocomplete data, and usage hints
8. [ ] **Prompt handling**: Update `prompt.go` to build `TeamConnection` structs instead of string concat
9. [ ] **API endpoints**: Add `POST/DELETE /api/v1/teams/{team_id}/rewrite` handlers that modify `RemoteTeamName` on existing inbound connections. Validate admin permissions, inbound-only, connection linked to team, and reverse index uniqueness constraint.
10. [ ] **Tests**: Update all test files (`caching_test.go`, `service_test.go`, `inbound_test.go`, `nats_test.go`) for new struct types, add rewrite resolution tests
11. [ ] **Frontend (team modal)**: Update `CrossguardTeamModal.tsx` to use structured API response, remove `parseConnection()`, add rewrite edit UI for inbound connections
12. [ ] **Frontend (channel modal)**: Update `CrossguardChannelModal.tsx` to use structured API response, remove `parseConnection()` (no rewrite UI, team-level only)

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Breaking existing KV data | No migration, clean break per user request. Admins re-init teams after upgrade. |
| Reverse index drift | Index is derived from `TeamConnection.RemoteTeamName`. Write ordering (index-first on set, list-first on clear) ensures partial failures leave safe stale state. `RemoveTeamConnection` reads existing `RemoteTeamName` before removal to clean up the index. |
| Duplicate rewrite conflict | `SetTeamRewriteIndex` checks for existing entries pointing to a different team and returns an error, preventing silent overwrites. |
| Reverse index key collision | `::` delimiter used between connName and remoteTeamName. Neither team slugs (`[a-z0-9-]`) nor connection names contain colons. |
| Rewrite target team deleted | `GetTeam(teamID)` returns error, logged as "rewrite target team not found" |
| Channel names also differ | Out of scope. Channel rewrites can follow same pattern on `TeamConnection` later. |

## Testing Plan

**Unit**: KV store round-trip for Get/Set/Add/Remove `TeamConnection`, `Matches()` logic, reverse index get/set/delete, CachingKVStore cache hit/miss/invalidation for both connection lists and rewrite index
**Integration**: `resolveTeamAndChannel` with `RemoteTeamName` configured resolves to correct local team; slash command sets rewrite and messages route correctly
**E2E**: `make docker-smoke-test` with two servers using different team names, verify messages relay after rewrite configured

## Acceptance Criteria

- [ ] `TeamConnection` struct with `Direction`, `Connection`, `RemoteTeamName` replaces all `[]string` connection storage
- [ ] Inbound messages with mismatched team names route correctly when `RemoteTeamName` is configured
- [ ] `/crossguard rewrite-team <connection> <remote-team-name>` sets `RemoteTeamName` on the inbound connection
- [ ] `/crossguard rewrite-team <connection>` (no remote name) clears the rewrite
- [ ] `POST/DELETE /api/v1/teams/{team_id}/rewrite` endpoints modify `RemoteTeamName` on existing connections
- [ ] Team and channel status APIs return structured connection data with direction, name, and remote_team_name
- [ ] Frontend displays structured connection data without string parsing
- [ ] Reverse index is maintained automatically when setting/clearing `RemoteTeamName` and on teardown
- [ ] Inbound rewrite lookup is O(1) via reverse index (no team scanning)
- [ ] Setting a rewrite fails with a clear error if another team already claims the same (connName, remoteTeamName) mapping
- [ ] Teardown of connections works correctly with struct-based storage and cleans up reverse index
- [ ] Works correctly in multi-node cluster deployments (cache invalidation)

## Checklist

- [ ] **Diagnostics**: Rewrite set/remove should be logged for audit trail
- [ ] **Slash command**: `rewrite-team` subcommand added with autocomplete, validates inbound-only, posts audit message
- [ ] **No migration**: Document in release notes that existing team/channel connections must be re-initialized after upgrade
