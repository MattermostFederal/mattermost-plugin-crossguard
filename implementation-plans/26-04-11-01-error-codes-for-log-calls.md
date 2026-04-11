# Crossguard Error Codes for All Log Calls

## Context

Crossguard currently has **215 `p.API.Log*` call sites** across 13 server files with **no error code system**. When operators see a log line in production, the only way to trace it back to source is grepping the free-text message, which is fragile (messages get reworded, substrings collide, translated or formatted text breaks lookups).

Goal: give every `p.API.LogDebug`, `LogInfo`, `LogWarn`, `LogError` call a **distinct numeric error code** sourced from a central registry, emitted as an `error_code` key in the structured log. Operators can then grep logs for a stable integer and jump directly to the source line.

## Decisions (from clarification)

| Question | Decision |
|---|---|
| Code format | Numeric `int` constants |
| Organization | Central registry file (`server/errcode/`) |
| Call-site style | Add `"error_code", errcode.X` as first K/V pair in every Log call |
| Scope | All four levels: Debug, Info, Warn, Error (215 codes) |

## Numbering Scheme

Codes are plain `int` constants, starting at **10000** and allocated in **1000-blocks per file**. Each file owns a full block of 1000 values, leaving ample headroom for new log calls without renumbering. Blocks are assigned in the order files are processed.

| Range | File | Current calls |
|---|---|---|
| 10000–10999 | `hooks.go` | 12 |
| 11000–11999 | `api.go` | 17 |
| 12000–12999 | `configuration.go` | 2 |
| 13000–13999 | `command.go` | 1 |
| 14000–14999 | `service.go` | 34 |
| 15000–15999 | `inbound.go` | 46 |
| 16000–16999 | `connections.go` | 14 |
| 17000–17999 | `sync_user.go` | 4 |
| 18000–18999 | `azure_blob_provider.go` | 49 |
| 19000–19999 | `azure_provider.go` | 10 |
| 20000–20999 | `nats_provider.go` | 4 |
| 21000–21999 | `prompt.go` | 17 |
| 22000–22999 | `retry_dispatch.go` | 5 |

Codes are allocated sequentially **in the order log statements appear in each file**. Within a block, Debug/Info/Warn/Error are not segregated (level is orthogonal to identity).

## Registry Structure

New package: `server/errcode/`

### `server/errcode/codes.go`

```go
// Package errcode defines distinct numeric identifiers for every
// p.API.Log* call in the Crossguard plugin. Each constant maps to exactly
// one call site so operators can grep production logs for a stable integer.
//
// Ranges are allocated by source file; see the plan in implementation-plans/
// for the allocation table. When adding a new Log call, append the next
// unused code in that file's block.
package errcode

// hooks.go (10000–10999)
const (
    HooksChannelConnCheckFailed = 10000
    HooksRelaySemaphoreFull     = 10001
    // ...
)

// api.go (11000–11999)
const (
    APIInvalidRequest = 11000
    // ...
)

// ... one block per file ...

// AllCodes lists every code declared in this package. Used by the
// uniqueness test. Keep in sync when adding new constants.
var AllCodes = []int{
    HooksChannelConnCheckFailed,
    HooksRelaySemaphoreFull,
    APIInvalidRequest,
    // ...
}
```

Naming convention: `<FilePrefix><CamelCaseSummary>`. The identifier describes the *event*, not the log level, so the same name works whether the call is `LogWarn` or `LogError`.

### `server/errcode/codes_test.go`

One test: **`TestCodesUnique`** — iterate `AllCodes`, build a `map[int]bool`, and fail if any duplicate is encountered. This is the contract that matters: two call sites must never share a code.

## Call-Site Transformation

Every `p.API.Log*` call gains `"error_code", errcode.X` as the **first** key-value pair after the message string. Example from `hooks.go:16`:

Before:
```go
p.API.LogError("Failed to check channel connections", "channel_id", channelID, "error", err.Error())
```

After:
```go
p.API.LogError("Failed to check channel connections",
    "error_code", errcode.HooksChannelConnCheckFailed,
    "channel_id", channelID, "error", err.Error())
```

Placing `error_code` first makes it visually prominent at call sites and predictable in log output. No message text changes — only the prepended K/V pair.

## Files to Modify

| File | Log calls | Action |
|---|---|---|
| `server/errcode/codes.go` | — | **NEW** — registry |
| `server/errcode/codes_test.go` | — | **NEW** — uniqueness test |
| `server/hooks.go` | 12 | Add error_code to every Log call |
| `server/api.go` | 17 | same |
| `server/configuration.go` | 2 | same |
| `server/command.go` | 1 | same |
| `server/service.go` | 34 | same |
| `server/inbound.go` | 46 | same |
| `server/connections.go` | 14 | same |
| `server/sync_user.go` | 4 | same |
| `server/azure_blob_provider.go` | 49 | same |
| `server/azure_provider.go` | 10 | same |
| `server/nats_provider.go` | 4 | same |
| `server/prompt.go` | 17 | same |
| `server/retry_dispatch.go` | 5 | same |
| **Total call-site edits** | **215** | |

Test files (`*_test.go`) that call `p.API.Log*` are **out of scope** — tests don't need registered codes.

## Implementation Tasks

1. [ ] Create `server/errcode/` directory with empty `codes.go` skeleton and package comment.
2. [ ] Walk `server/hooks.go` top-to-bottom; for each `p.API.Log*` call, allocate the next code in 1000-block, add the constant to `codes.go`, add its description, and update the call site. Run `make check-style` after the file is complete.
3. [ ] Repeat for each file in the table above, in order: `api.go`, `configuration.go`, `command.go`, `service.go`, `inbound.go`, `connections.go`, `sync_user.go`, `azure_blob_provider.go`, `azure_provider.go`, `nats_provider.go`, `prompt.go`, `retry_dispatch.go`.
4. [ ] Write `server/errcode/codes_test.go` with `TestCodesUnique`.
5. [ ] Run `make check-style` and `make test` — must pass cleanly.
6. [ ] Spot-check a deployed plugin log line (optional): trigger one warn path in dev docker and verify `error_code` appears as a structured field.

Because this is 215 mechanical edits, the executing agent should process **one file at a time**, not attempt all files in parallel, to keep the diff reviewable and keep numbering monotonic.

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| Two call sites accidentally get the same code during editing | `TestCodesUnique` fails the build; also, sequential per-file allocation makes collisions structurally unlikely |
| A new Log call is added later without a code | Add a `TODO: register in errcode` note in `server/CLAUDE.md`; future linter/review catches it. Not enforced by this plan |
| Massive diff noise in PR review | Split by file into logical commits (one commit per module block) so reviewers can scan each block independently |
| Renaming `errcode.X` later breaks log grep history | Constants are identifiers only; the **numeric value** is the stable contract. Once assigned, never change the integer |

## Out of Scope

- Test-file log calls (`*_test.go`).
- Any lint rule or CI check that enforces "every new Log call must reference errcode".
- A generated markdown catalog of codes.
- Renaming or restructuring existing log messages.
- Migrating log calls that go through non-`p.API` loggers (e.g., `fmt.Println`, direct `log.Printf`) — none found in scope.

## Verification

1. **Unit**: `cd server && go test ./errcode/...` — uniqueness and completeness tests pass.
2. **Build**: `make check-style && make test` — whole project still green.
3. **Grep audit**: `grep -rn "p.API.Log" server/ --include="*.go" | grep -v _test.go | grep -v error_code | wc -l` should return **0**. This proves every non-test Log call has an `error_code` field.
4. **Count audit**: `len(errcode.AllCodes)` equals **215** (matching the pre-change `p.API.Log*` count).
5. **Runtime smoke**: `make deploy && make docker-smoke-test`, then `make docker-logs` — confirm an emitted line includes `error_code=<int>`.

## Post-Approval Save

After approval, this plan will be saved to `implementation-plans/26-04-11-01-error-codes-for-log-calls.md` before implementation begins, per the create-plan skill convention.
