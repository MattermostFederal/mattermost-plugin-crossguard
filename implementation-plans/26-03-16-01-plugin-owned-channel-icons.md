# Replace Shared Channel Infrastructure with Plugin-Owned Icons

## Context

Currently, CrossGuard marks channels as shared (`channel.Shared = true`) and sets `RemoteId` on sync users to get the `data-testid="SharedChannelIcon"` (the `icon-circle-multiple-outline` icon) to appear in the sidebar and channel header. This piggybacks on Mattermost's built-in Connected Workspaces/Shared Channels infrastructure, which causes broken API requests:

- `GET /api/v4/sharedchannels/{channelId}/remotes` (fails because no actual shared channel remotes exist)
- `GET /api/v4/sharedchannels/remote_info/crossguard-{connName}` (fails for the same reason)

These requests are triggered by Mattermost's `SidebarChannelLink` and `ChannelHeaderTitle` components when they see `channel.shared === true` or `user.remote_id` is set.

## Problem Statement

Setting `channel.Shared` and `user.RemoteId` to get visual indicators causes Mattermost core to make API calls to the Shared Channels endpoints, which fail because CrossGuard does not use that infrastructure. We need our own icon system that avoids these broken requests entirely.

## Design Principles

| Pattern | Our Approach | Avoid |
|---------|-------------|-------|
| Channel indicator | Store connection names in `channel.Props` | Using `channel.Shared` (triggers broken API calls) |
| User indicator | Plugin popover via `registerPopoverUserAttributesComponent` | Using `user.RemoteId` (triggers broken API calls) |
| Data sync | Leverage Mattermost's built-in channel/user prop sync | Custom KV index + API + Redux + WebSocket events |
| Sidebar icon | `registerSidebarChannelLinkLabelComponent` reads `channel.props` directly | Fetching state from plugin API per-channel |
| Channel header icon | `registerChannelHeaderButtonAction` with always-registered React component that renders icon or empty based on Redux channel state | Dynamic register/unregister on channel switch |

## Requirements

- [ ] `data-testid="SharedChannelIcon"` appears next to connected channels in the sidebar
- [ ] Tooltip on channel icon shows connection name(s)
- [ ] Icon is passive (no click action)
- [ ] Channel header shows CrossGuard icon with connection name tooltip for connected channels (blank for non-connected)
- [ ] Clicking a sync user's name shows "Relayed from: {username} (via {connName})" in the profile popover
- [ ] No requests to `/api/v4/sharedchannels/*` endpoints
- [ ] Linking/unlinking updates the icon in real-time (via Mattermost's channel update WebSocket)

## Out of Scope

- Inline username icon in post view (no plugin extension point exists; popover is the alternative)
- Custom post type for relayed messages (future enhancement)

## Technical Approach

### Channel Indicator via `channel.Props`

Store connection names directly on the channel as `channel.Props["crossguard_connections"]` (comma-separated string). The Mattermost plugin API `registerSidebarChannelLinkLabelComponent` passes the full `channel` object to the registered component, so it can read `channel.props.crossguard_connections` directly with no API calls needed.

When a channel is linked/unlinked, `UpdateChannel` is called with the updated props. Mattermost core broadcasts a channel update WebSocket event, so the sidebar component re-renders automatically.

### Channel Header Icon via `registerChannelHeaderButtonAction`

Register a channel header button once on plugin init. The `icon` parameter is a React component that reads the current channel from the Redux store (`state.entities.channels.currentChannelId` and `state.entities.channels.channels`). If the current channel has `props.crossguard_connections`, render `icon-circle-multiple-outline`; otherwise render an empty fragment. The button is always registered but visually invisible on non-connected channels.

- `icon`: React component that conditionally renders based on current channel's props
- `action`: No-op (passive indicator)
- `dropdownText`: "CrossGuard"
- `tooltipText`: Connection name(s) or empty

### User Indicator via `registerPopoverUserAttributesComponent`

Store the remote username in `user.Props["CrossguardRemoteUsername"]` (renamed from `RemoteUsername` to avoid collision with Mattermost Shared Channels). Register a `PopoverUserAttributes` component that reads this prop and displays connection info in the user's profile popover.

## Files to Modify

| File | Change |
|------|--------|
| `server/service.go` | Replace `channel.Shared` set/unset with `channel.Props["crossguard_connections"]` in `initChannelForCrossGuard` and `teardownChannelForCrossGuard` |
| `server/sync_user.go` | Remove `RemoteId` from sync user creation. Rename `Props["RemoteUsername"]` to `Props["CrossguardRemoteUsername"]` |
| `server/sync_user_test.go` | Remove `RemoteId` assertion, update Props assertion |
| `webapp/src/components/CrossguardChannelIndicator.tsx` | **New.** Sidebar label component that reads `channel.props.crossguard_connections` and renders icon with tooltip |
| `webapp/src/components/CrossguardHeaderIcon.tsx` | **New.** Channel header icon component that reads current channel from Redux, renders icon or empty |
| `webapp/src/components/CrossguardUserPopover.tsx` | **New.** Popover user attributes component that reads `user.props.CrossguardRemoteUsername` |
| `webapp/src/index.tsx` | Register `CrossguardChannelIndicator`, `CrossguardHeaderIcon`, and `CrossguardUserPopover` |

## Tasks

1. [ ] **Server: channel.Props instead of channel.Shared** - In `service.go`, replace `channel.Shared = true/false` + `UpdateChannel` with `channel.Props["crossguard_connections"]` updates in both `initChannelForCrossGuard` (lines 331-336) and `teardownChannelForCrossGuard` (lines 389-393). Guard against nil Props map: `if channel.Props == nil { channel.Props = make(model.StringMap) }` before setting the key
2. [ ] **Server: remove RemoteId** - In `sync_user.go`, remove `RemoteId` field (line 58) and `remoteID` variable (line 48). Change `Props["RemoteUsername"]` to `Props["CrossguardRemoteUsername"]`
3. [ ] **Server: update tests** - In `sync_user_test.go`, remove `RemoteId` assertion (lines 89-90), update Props assertion
4. [ ] **Webapp: CrossguardChannelIndicator** - New component reading `channel.props.crossguard_connections`, rendering `icon-circle-multiple-outline` with `data-testid="SharedChannelIcon"` and tooltip
5. [ ] **Webapp: CrossguardHeaderIcon** - New component that reads current channel from Redux store, renders `icon-circle-multiple-outline` if connected or empty fragment if not
6. [ ] **Webapp: CrossguardUserPopover** - New component reading `user.props.CrossguardRemoteUsername`, rendering "Relayed from: {username} (via {connName})" in profile popover
7. [ ] **Webapp: register components** - In `index.tsx`, call `registerSidebarChannelLinkLabelComponent`, `registerChannelHeaderButtonAction`, and `registerPopoverUserAttributesComponent`

## Decisions

| Question | Decision | Rationale |
|----------|----------|-----------|
| How to store channel connection state for the webapp | `channel.Props["crossguard_connections"]` | Mattermost syncs channel props to webapp automatically, no custom API/Redux/WS needed |
| How to indicate sync users | `PopoverUserAttributes` reading `user.Props["CrossguardRemoteUsername"]` | Only plugin extension point for user info; inline icon requires `RemoteId` which breaks |
| Props key naming | `CrossguardRemoteUsername` (not `RemoteUsername`) | Avoids collision with Mattermost Shared Channels infrastructure |
| Channel header icon | Always-registered button with conditionally-rendering icon component | Simpler than dynamic register/unregister on channel switch |

## Verification

1. `make check-style` and `make test` pass
2. `make deploy` to Docker dev environment
3. Link a channel via CrossGuard modal on Server A
4. Verify: `data-testid="SharedChannelIcon"` appears in sidebar next to connected channel with tooltip showing connection name
5. Verify: Channel header shows CrossGuard icon when viewing a connected channel
6. Verify: Channel header icon is not visible when viewing a non-connected channel
7. Verify: No requests to `/api/v4/sharedchannels/*` in browser network tab
8. Verify: Unlinking removes the icons (channel update triggers re-render)
9. Send a message from Server B, click on the sync user's name in Server A
10. Verify: Profile popover shows "Relayed from: {username} (via {connName})"
11. Verify: No `SharedUserIndicator` icon next to sync usernames (RemoteId removed)
12. `make docker-smoke-test` passes

## Acceptance Criteria

- [ ] Connected channels show `data-testid="SharedChannelIcon"` in the sidebar
- [ ] Icon tooltip shows connection name(s)
- [ ] Channel header shows CrossGuard icon on connected channels, blank on others
- [ ] No requests to `/api/v4/sharedchannels/*` in browser network tab
- [ ] Sync user profile popover shows remote username and connection name
- [ ] Linking/unlinking a channel updates the icon without page refresh
- [ ] All tests pass
