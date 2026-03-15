# Team-Connection Linking for init-team, teardown-team, and status

## Context

Currently, `init-team` sets a boolean flag per team. All relay-enabled channels broadcast to ALL outbound connections, and ALL inbound connections feed messages to any initialized team/channel. There is no association between a team and specific NATS connections.

The goal is to let admins link specific NATS connections to teams, so relay traffic is scoped per-connection rather than global. This enables multi-tenant setups where different teams relay through different NATS subjects/servers.

## Problem Statement

When multiple NATS connections (inbound + outbound) are configured, there is no way to control which connections apply to which team. Every initialized team relays through every connection, which is incorrect for multi-connection deployments.

## Current State

- `teaminit-{teamID}` stores `bool` in KV
- `initialized-teams` stores `[]string` of team IDs
- `channelinit-{channelID}` stores `bool`
- `publishToOutbound()` sends to ALL outbound connections
- `resolveTeamAndChannel()` accepts from ANY inbound connection

## Design Principles

| Pattern | Our Approach | Avoid |
|---------|-------------|-------|
| Connection linking | Store connection names per team in KV | Separate linking table or complex schema |
| Auto-selection | Auto-link when only 1 connection exists | Always requiring explicit selection |
| Ambiguity resolution | List available connections in error message | Interactive posts with action callbacks (over-engineered) |
| Channel connections | Inherit from team | Per-channel connection config |
| Migration | None needed, clean break | Complex migration logic |

## Requirements

- [ ] `init-team [connection-name]` links a connection to a team (connection-name uses `inbound-{name}` or `outbound-{name}` format)
- [ ] If only 1 total connection (inbound+outbound), auto-link without param
- [ ] If >1 connections and no param, list available connections in ephemeral error and prompt re-run
- [ ] Running `init-team` on an already-initialized team with a new connection name adds that connection
- [ ] `teardown-team [connection-name]` unlinks a connection
- [ ] If only 1 connection linked, auto-teardown without param
- [ ] If >1 linked and no param, list linked connections in ephemeral error and prompt re-run
- [ ] If 0 connections configured, error: "No NATS connections configured"
- [ ] `status` shows which connections are linked to the team
- [ ] REST APIs accept optional `connection_name` query parameter
- [ ] Outbound relay only publishes to team-linked outbound connections
- [ ] Inbound relay only accepts messages for teams with the connection linked
- [ ] Replace existing boolean team init with connection list (no migration needed)

## Out of Scope

- Per-channel connection linking (channels inherit from team)
- Webapp/admin console changes for connection linking
- init-channel connection awareness
- Interactive posts with SlackAttachment actions (deferred, simple error message is sufficient)

## Technical Approach

### KV Store Schema Change

Reuse existing `teaminit-{teamID}` key prefix but change stored type from `bool` to `[]string` (connection names). A team is "initialized" if it has at least one connection name stored.

This eliminates dual-state. No new key prefix needed.

### Connection Direction

Connection names are direction-aware. Stored connection names use the format `inbound-{name}` or `outbound-{name}` to include direction as part of the identifier. This makes direction explicit in the KV store, in user-facing commands, and in status output.

For example, if the admin configures an outbound connection named `fed-a` and an inbound connection named `fed-a`, the stored names would be `outbound-fed-a` and `inbound-fed-a`. This avoids ambiguity when the same base name is used for both directions.

The `getAllConnectionNames()` helper builds the direction-prefixed list from config:
- For each outbound connection: `"outbound-" + conn.Name`
- For each inbound connection: `"inbound-" + conn.Name`

At relay time:
- **Outbound**: Filter `outboundConns` pool to only those whose `"outbound-" + name` is in the team's connection list
- **Inbound**: Check if `"inbound-" + connName` is in the target team's connection list

This means a team with only an inbound connection linked will receive messages but not send. This is valid and expected for asymmetric setups.

### Adding Connections to Already-Initialized Teams

`init-team` changes behavior: instead of returning "already initialized" when the team has connections, it checks if the specified connection is already linked. If not, it adds it. Only returns "already linked" if that specific connection is already in the list.

### Relay Filtering

- `isChannelRelayEnabled()` returns `(channel, team, []string)` where `[]string` is the team's connection list. Empty means relay disabled.
- `publishToOutbound()` accepts `connNames []string` filter, only publishes to matching outbound connections
- `resolveTeamAndChannel()` accepts `connName` and verifies it is in the team's connection list

### Callers of old GetTeamInitialized that need updating

1. `service.go:58` - `initTeamForCrossGuard` - replaced by `GetTeamConnections`
2. `service.go:95` - `getTeamStatus` - replaced by `GetTeamConnections`
3. `service.go:161` - `initChannelForCrossGuard` - replaced by `IsTeamInitialized`
4. `hooks.go:26` - `isChannelRelayEnabled` - replaced by `GetTeamConnections` (returns conn list)
5. `inbound.go:127` - `resolveTeamAndChannel` - replaced by `GetTeamConnections` + validate connName
6. `service.go:238` - `teardownTeamForCrossGuard` - replaced by `GetTeamConnections`

## Decisions

| Question | Decision | Rationale |
|----------|----------|-----------|
| Store schema | Reuse `teaminit-` prefix, change type from bool to []string | No dual-state, simpler |
| Migration | None, clean break | No migration complexity |
| Ambiguity UX | Error message listing connections, user re-runs | Avoids interactive post infrastructure (~150 lines saved) |
| Connection direction | Direction-prefixed: `inbound-{name}`, `outbound-{name}` | Explicit direction avoids ambiguity when same base name used for both directions |
| Same connection, multiple teams | Allowed | Per-team storage model supports this, valid for shared NATS buses |
| Orphaned connection names | Warn in status, relay silently skips | Config changes shouldn't break stored state |
| Adding to initialized team | `init-team` adds new connection, not "already init" error | Fixes critical flaw: must be able to link multiple connections |
| API param naming | `connection_name` (not `connection`) | Consistent with `team_id`, `channel_id` snake_case convention |
| Per-team status field | `linked_connections: []string` | Avoids collision with global `connections: []RedactedNATSConnection` |

## Files to Modify

| File | Change |
|------|--------|
| `server/store/store.go` | Replace `Get/Set/DeleteTeamInitialized` with `GetTeamConnections`, `SetTeamConnections`, `DeleteTeamConnections`, `IsTeamInitialized`, `AddTeamConnection`, `RemoveTeamConnection` |
| `server/store/client.go` | Implement new methods on same `teaminit-` prefix with `[]string` type. Use `casModifyStringList` for Add/Remove. |
| `server/store/caching.go` | Change `teamInitCache` type from `LRU[string, bool]` to `LRU[string, []string]`, update wrapper methods |
| `server/store/caching_test.go` | Update tests for new interface |
| `server/service.go` | Update `initTeamForCrossGuard(user, teamID, connName)` to add connection (not set bool). Update `teardownTeamForCrossGuard(user, teamID, connName)` to remove connection. Update status responses with `linked_connections`. Add `getAllConnectionNames()` helper. |
| `server/command.go` | Parse optional connection-name param. List connections in error for ambiguous case. Update `init-team` to not return "already init" when adding new connection. Update status display. |
| `server/api.go` | Add `connection_name` query param to init/teardown handlers. Update error responses for multi-connection case. |
| `server/hooks.go` | `isChannelRelayEnabled` returns `[]string`. `relayToOutbound` accepts conn filter. |
| `server/nats.go` | `publishToOutbound` accepts `connNames []string` filter |
| `server/inbound.go` | `resolveTeamAndChannel` accepts and validates `connName` against team's connection list |

## Tasks

1. [ ] **Store interface**: Replace `Get/Set/DeleteTeamInitialized` with connection-aware methods in `store.go`
2. [ ] **Store client**: Implement `GetTeamConnections`, `SetTeamConnections`, `DeleteTeamConnections`, `IsTeamInitialized`, `AddTeamConnection`, `RemoveTeamConnection` in `client.go`.
3. [ ] **Caching store**: Update `caching.go` cache type from `bool` to `[]string`, update all wrapper methods
4. [ ] **Service layer**: Update `initTeamForCrossGuard` and `teardownTeamForCrossGuard` to accept `connName`. Change init to add connections (not just set bool). Update status responses with `linked_connections`. Add `getAllConnectionNames()` helper that returns direction-prefixed names (`outbound-{name}`, `inbound-{name}`).
5. [ ] **Relay filtering (outbound)**: Update `isChannelRelayEnabled` to return `[]string`. Update `relayToOutbound` and `publishToOutbound` to filter by connection names.
6. [ ] **Relay filtering (inbound)**: Update `resolveTeamAndChannel` to accept and validate `connName`.
7. [ ] **Command handlers**: Parse optional connection-name param. Error with available names for ambiguous case. Handle zero-connections case. Update autocomplete hints. Update status display.
8. [ ] **API endpoints**: Update `handleInitTeam` and `handleTeardownTeam` to accept optional `connection_name` query param. Define 400 response with `connections` list for ambiguous case.
9. [ ] **Tests**: Update existing tests, add tests for connection linking, relay filtering.

## Error Response Branches

| Scenario | `connection_name` param | Response |
|----------|------------------------|----------|
| 0 connections configured | omitted | 400: `{"error": "no NATS connections configured"}` |
| 0 connections configured | provided | 400: `{"error": "no NATS connections configured"}` |
| 1 connection configured | omitted | Auto-select, 200 OK |
| 1 connection configured | valid | Use specified, 200 OK |
| 1 connection configured | invalid | 400: `{"error": "connection not found: outbound-foo"}` |
| N connections configured | omitted | 400: `{"error": "multiple connections available, specify connection_name", "connections": ["outbound-a","inbound-a"]}` |
| N connections configured | valid | Use specified, 200 OK |
| N connections configured | invalid | 400: `{"error": "connection not found: outbound-foo", "connections": ["outbound-a","inbound-a"]}` |

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Orphaned connection names in KV (config changed, old names remain) | Status command warns about unknown connections. Relay silently skips. |
| Concurrent link/unlink race | Reuse existing `casModifyStringList` pattern with retry. |
| Cluster cache staleness (node links, other node serves stale) | Existing cluster event invalidation handles this. Same pattern as current teamInit cache. |
| Channel state persists after last connection unlinked | Documented: channel init flag remains, but relay is inactive because team has no connections. Re-linking a connection resumes relay. |

## Testing Plan

**Unit**: Store methods (GetTeamConnections, AddTeamConnection, RemoveTeamConnection), getAllConnectionNames helper
**Integration**: init-team with 0/1/N connections, adding 2nd connection to already-init team, teardown with 1 vs multiple linked, relay filtering (outbound only to linked, inbound rejected if not linked)
**Manual E2E**: Run with docker dual-server setup, configure 2 connections, init teams with different connections, verify messages only relay through linked connections

## Verification

1. `make check-style` and `make test` pass
2. `make deploy` to docker dual-server environment
3. `/crossguard init-team` with 0 connections: error message
4. `/crossguard init-team` with 1 connection: auto-link
5. Configure 2 connections, `/crossguard init-team`: error listing available connections
6. `/crossguard init-team outbound-conn-a`: links outbound-conn-a
7. `/crossguard init-team inbound-conn-a`: adds inbound-conn-a (not "already init")
8. `/crossguard status`: shows linked connections
9. Post message, verify it only goes through linked outbound connections
10. `/crossguard teardown-team` with 2 linked: error listing linked connections
11. `/crossguard teardown-team outbound-conn-a`: unlinks outbound-conn-a
12. `/crossguard teardown-team`: auto-unlinks last connection (inbound-conn-a)

## Acceptance Criteria

- [ ] `init-team` with 0 connections shows clear error
- [ ] `init-team` with 1 total connection auto-links without parameters
- [ ] `init-team` with >1 connections and no param lists available connections
- [ ] `init-team connection-name` links the specified connection
- [ ] `init-team` on already-initialized team adds new connection (not "already init")
- [ ] `teardown-team` with 1 linked connection auto-unlinks
- [ ] `teardown-team` with >1 linked lists linked connections
- [ ] `status` shows linked connection names per team (field: `linked_connections`)
- [ ] Messages only relay through team-linked outbound connections
- [ ] Inbound messages rejected if connection not linked to target team
- [ ] APIs accept optional `connection_name` query parameter

## Checklist

- [ ] **Diagnostics**: Init/teardown connection linking should post to diagnostics channel
- [ ] **Slash command**: Existing commands updated, no new subcommands needed
