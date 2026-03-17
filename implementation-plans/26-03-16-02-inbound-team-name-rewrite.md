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

### Inbound Processing Change

In `resolveTeamAndChannel()`, after finding the team connections, check for a `RemoteTeamName` on the matching inbound connection:

```go
conns, _ := p.kvstore.GetTeamConnections(teamID)
for _, tc := range conns {
    if tc.Direction == "inbound" && tc.Connection == connName && tc.RemoteTeamName != "" {
        // This inbound connection has a team name rewrite configured
        if tc.RemoteTeamName == teamName {
            // The remote team name matches, use this local team
            team, appErr = p.API.GetTeam(teamID)
            // ...
        }
    }
}
```

Actually, the rewrite needs to happen earlier. `resolveTeamAndChannel` receives the remote `teamName` and needs to find the local team. The flow becomes:

1. First, try `GetTeamByName(teamName)` as before (handles matching names)
2. If not found, scan all initialized teams for an inbound connection with `RemoteTeamName == teamName` and `Connection == connName`
3. If found, use that local team

```go
func (p *Plugin) findTeamByRewrite(connName, remoteTeamName string) (*model.Team, error) {
    teamIDs, err := p.kvstore.GetInitializedTeamIDs()
    if err != nil {
        return nil, err
    }
    for _, teamID := range teamIDs {
        conns, err := p.kvstore.GetTeamConnections(teamID)
        if err != nil {
            continue
        }
        for _, tc := range conns {
            if tc.Direction == "inbound" && tc.Connection == connName && tc.RemoteTeamName == remoteTeamName {
                team, appErr := p.API.GetTeam(teamID)
                if appErr != nil {
                    return nil, fmt.Errorf("rewrite target team %s not found: %w", teamID, appErr)
                }
                return team, nil
            }
        }
    }
    return nil, nil
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

The set-rewrite handler finds the matching inbound `TeamConnection` in the team's connections list, sets its `RemoteTeamName`, and calls `SetTeamConnections()`. The delete handler clears `RemoteTeamName`.

### Frontend Changes

`parseConnection()` is no longer needed. The API now returns structured `ConnectionStatus` with `direction` and `name` as separate fields.

In `CrossguardTeamModal.tsx` and `CrossguardChannelModal.tsx`:
- Remove `parseConnection()` function
- Use `conn.direction` and `conn.name` directly from the API response
- For inbound connections, show `remote_team_name` and allow editing via the rewrite endpoints

### NATS Layer Changes

`server/nats.go:316` currently does `slices.Contains(connNames, "outbound:"+outboundName)`. This changes to iterate `[]TeamConnection` and match on `Direction == "outbound" && Connection == outboundName`.

## Decisions

| Question | Decision | Rationale |
|----------|----------|-----------|
| Storage format? | `[]TeamConnection` struct in existing KV keys | Embeds rewrite data with connection, no separate keys needed |
| Backwards compat? | None | User-specified, clean break simplifies code |
| Matching for removal? | Direction + Connection only | RemoteTeamName is metadata, not identity |
| Rewrite lookup? | Scan initialized teams | Small list (1-5 teams typically), avoids extra index |
| Separate rewrite KV keys? | No, removed from design | Struct approach eliminates need for separate storage |

## Files to Modify

| File | Change |
|------|--------|
| `server/store/store.go` | Add `TeamConnection` struct with `Matches()` method, change all `[]string` params to `[]TeamConnection` in KVStore interface |
| `server/store/client.go` | Update `GetTeamConnections`/`SetTeamConnections`/`AddTeamConnection`/`RemoveTeamConnection` (and channel equivalents) to use `[]TeamConnection` |
| `server/store/caching.go` | Update cache types from `[]string` to `[]TeamConnection`, update all wrapper methods |
| `server/store/caching_test.go` | Update all test data from strings to `TeamConnection` structs |
| `server/service.go` | Update `ConnectionStatus`, `TeamStatusResponse`, `TeamStatusEntry`, `getAllConnectionNames()`, `getTeamStatus()`, `getChannelStatus()`, `initTeamForCrossGuard()`, `teardownTeamForCrossGuard()`, `initChannelForCrossGuard()`, `teardownChannelForCrossGuard()`, `resolveConnectionName()` |
| `server/service_test.go` | Update test data to use `TeamConnection` structs |
| `server/inbound.go` | Add `findTeamByRewrite()`, modify `resolveTeamAndChannel()` to check rewrites on name-not-found |
| `server/inbound_test.go` | Update `testKVStore` to return `[]TeamConnection`, add rewrite resolution tests |
| `server/nats.go` | Update connection matching from string contains to struct field comparison |
| `server/nats_test.go` | Update test data to use `TeamConnection` structs |
| `server/api.go` | Add `handleSetTeamRewrite` (POST) and `handleDeleteTeamRewrite` (DELETE) endpoints |
| `server/command.go` | Update `executeInitTeam()` to build `TeamConnection` struct, add optional remote-team-name arg |
| `server/prompt.go` | Update connection name building from string concat to `TeamConnection` struct |
| `webapp/src/components/CrossguardTeamModal.tsx` | Remove `parseConnection()`, use structured API response, add rewrite edit controls |
| `webapp/src/components/CrossguardChannelModal.tsx` | Remove `parseConnection()`, use structured API response |

## Tasks

1. [ ] **Store struct + interface**: Add `TeamConnection` struct with `Matches()` to `store.go`, change all `[]string` connection params to `[]TeamConnection` in `KVStore` interface
2. [ ] **Store implementation**: Update `client.go` to serialize/deserialize `[]TeamConnection` JSON, update Add/Remove to use `Matches()`
3. [ ] **Caching layer**: Update `caching.go` cache types and wrapper methods for `[]TeamConnection`
4. [ ] **Service layer**: Update all service functions, `ConnectionStatus`, `TeamStatusResponse`, `TeamStatusEntry`, `getAllConnectionNames()`, status builders, init/teardown functions
5. [ ] **Inbound processing**: Add `findTeamByRewrite()`, modify `resolveTeamAndChannel()` to fall back to rewrite scan when team name not found
6. [ ] **NATS layer**: Update `nats.go` connection matching from string to struct field comparison
7. [ ] **Slash command**: Update `command.go` to build `TeamConnection` structs, add optional remote-team-name argument
8. [ ] **Prompt handling**: Update `prompt.go` to build `TeamConnection` structs instead of string concat
9. [ ] **API endpoints**: Add `POST/DELETE /api/v1/teams/{team_id}/rewrite` handlers that modify `RemoteTeamName` on existing inbound connections
10. [ ] **Tests**: Update all test files (`caching_test.go`, `service_test.go`, `inbound_test.go`, `nats_test.go`) for new struct types, add rewrite resolution tests
11. [ ] **Frontend**: Update `CrossguardTeamModal.tsx` and `CrossguardChannelModal.tsx` to use structured API response, remove `parseConnection()`, add rewrite edit UI for inbound connections

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Breaking existing KV data | No migration, clean break per user request. Admins re-init teams after upgrade. |
| Rewrite scan performance | Initialized teams list is small (1-5 typically). If it grows, add a reverse index later. |
| Rewrite target team deleted | `GetTeam(teamID)` returns error, logged as "rewrite target team not found" |
| Channel names also differ | Out of scope. Channel rewrites can follow same pattern on `TeamConnection` later. |

## Testing Plan

**Unit**: KV store round-trip for Get/Set/Add/Remove `TeamConnection`, `Matches()` logic, CachingKVStore cache hit/miss/invalidation
**Integration**: `resolveTeamAndChannel` with `RemoteTeamName` configured resolves to correct local team; slash command sets rewrite and messages route correctly
**E2E**: `make docker-smoke-test` with two servers using different team names, verify messages relay after rewrite configured

## Acceptance Criteria

- [ ] `TeamConnection` struct with `Direction`, `Connection`, `RemoteTeamName` replaces all `[]string` connection storage
- [ ] Inbound messages with mismatched team names route correctly when `RemoteTeamName` is configured
- [ ] `/crossguard team-init <connection> <remote-team-name>` sets `RemoteTeamName` on the inbound connection
- [ ] `POST/DELETE /api/v1/teams/{team_id}/rewrite` endpoints modify `RemoteTeamName` on existing connections
- [ ] Team and channel status APIs return structured connection data with direction, name, and remote_team_name
- [ ] Frontend displays structured connection data without string parsing
- [ ] Teardown of connections works correctly with struct-based storage
- [ ] Works correctly in multi-node cluster deployments (cache invalidation)

## Checklist

- [ ] **Diagnostics**: Rewrite set/remove should be logged for audit trail
- [ ] **Slash command**: `init-team` extended with optional remote-team-name arg, autocomplete hint updated
- [ ] **No migration**: Document in release notes that existing team/channel connections must be re-initialized after upgrade
