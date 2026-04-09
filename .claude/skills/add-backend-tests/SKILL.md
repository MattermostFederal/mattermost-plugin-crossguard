---
name: add-backend-tests
description: Systematically find Go backend test coverage gaps and add exhaustive unit/integration tests. Use when you want to improve Go test coverage, add missing tests, or harden existing test suites.
---

# Add Backend Tests

Systematically analyze Go backend coverage, identify gaps, and add exhaustive tests that follow project patterns. You are a Go testing expert who writes thorough, maintainable tests.

## Step 1: Measure Current Coverage

Run `make coverage-backend` and capture the output. This runs `go test -coverprofile` then `go tool cover -func` to show per-function coverage.

```bash
make coverage-backend 2>&1
```

Parse the output to build a prioritized list:
- **Tier 1**: Functions at 0% coverage (completely untested)
- **Tier 2**: Functions below 60% coverage (significant gaps)
- **Tier 3**: Functions below 80% coverage (moderate gaps)
- **Tier 4**: Complex functions above 80% that have untested edge cases

Skip trivial functions (main, manifest constants, simple getters under 5 lines).

## Step 2: Understand What Needs Testing

For each function in your priority list:

1. **Read the source file** to understand the function's logic, branches, and error paths
2. **Read the existing test file** (if one exists) to see what's already covered
3. **Identify untested paths**: error returns, edge cases, branch conditions, concurrent scenarios
4. **Note dependencies**: what needs mocking (API calls, KV store, providers)

Build a concrete test plan before writing any code. For each test, know:
- The specific branch/path being tested
- The setup required (mocks, test data)
- The assertion that proves the path was exercised

## Step 3: Write Tests Using Project Patterns

### Test Infrastructure Available

The project has established test utilities in `server/test_helpers_test.go`:

**Plugin setup with router and mock KV store:**
```go
p, kvs := setupTestPluginWithRouter(api)
// kvs is a *flexibleKVStore - override any KV method via function pointers:
kvs.getTeamConnectionsFn = func(teamID string) ([]store.TeamConnection, error) {
    return nil, errors.New("store error")
}
```

**HTTP request builder for API tests:**
```go
req := makeAuthRequest(t, "POST", "/api/v1/endpoint", bodyStruct, "user-id")
w := httptest.NewRecorder()
p.ServeHTTP(w, req)
assert.Equal(t, http.StatusOK, w.Code)
```

**Response decoder:**
```go
result := decodeJSONResponse(t, w)
```

**Mock queue provider (implements QueueProvider interface):**
```go
provider := &mockQueueProvider{
    publishFn: func(ctx context.Context, data []byte) error {
        return errors.New("publish failed")
    },
    maxMsgSize: 1024,
}
```

**Embedded NATS server for integration tests:**
```go
addr := startEmbeddedNATS(t) // returns "nats://127.0.0.1:<port>"
np := connectToEmbeddedNATS(t, addr, "test-subject")
```

**Mattermost plugin API mock:**
```go
api := &plugintest.API{}
api.On("LogError", mock.Anything, mock.Anything, mock.Anything).Return()
api.On("GetTeamByName", "team-a").Return(&model.Team{Id: "team-id"}, nil)
defer api.AssertExpectations(t)
```

### Testing Patterns to Follow

**Subtests for related scenarios:**
```go
func TestFunctionName(t *testing.T) {
    t.Run("success case", func(t *testing.T) { ... })
    t.Run("error from dependency", func(t *testing.T) { ... })
    t.Run("edge case description", func(t *testing.T) { ... })
}
```

**Table-driven tests for input variations:**
```go
tests := []struct {
    name    string
    input   string
    want    string
    wantErr bool
}{
    {"valid input", "hello", "HELLO", false},
    {"empty input", "", "", true},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        got, err := Transform(tt.input)
        if tt.wantErr {
            require.Error(t, err)
        } else {
            require.NoError(t, err)
            assert.Equal(t, tt.want, got)
        }
    })
}
```

**Testing concurrent/semaphore behavior:**
```go
// Fill the semaphore to test "full" path
for i := 0; i < cap(p.relaySem); i++ {
    p.relaySem <- struct{}{}
}
defer func() {
    for i := 0; i < cap(p.relaySem); i++ {
        <-p.relaySem
    }
}()
// Now call the function - it should hit the "semaphore full" branch
```

**Testing context cancellation:**
```go
ctx, cancel := context.WithCancel(context.Background())
cancel() // cancel immediately
err := functionUnderTest(ctx)
assert.ErrorIs(t, err, context.Canceled)
```

### What Makes a Good Test

- **Tests behavior, not implementation**: assert on observable outcomes (HTTP status, return values, mock expectations), not internal state
- **One logical assertion per subtest**: test one path/branch per t.Run
- **Descriptive names**: `TestHandleInbound_SemaphoreFull_DropsMessage` not `TestHandleInbound3`
- **Isolated**: each subtest sets up its own state, no shared mutable state between subtests
- **Fast**: prefer mocks over real I/O. Use embedded NATS only for provider integration tests
- **No synthetic/mock data to fix failures**: if a test fails, fix the code or the test logic, never fabricate data

### What to Test in Each Function

For every function, aim to cover:

1. **Happy path**: normal successful execution
2. **Each error return**: every `return err` or `return nil, err` branch
3. **Nil/empty inputs**: nil pointers, empty strings, empty slices
4. **Boundary conditions**: exact limits, off-by-one, size thresholds
5. **Concurrent access**: semaphore full, context cancelled, CAS retry
6. **Permission checks**: unauthorized user, wrong role, missing header

### File Organization

Add tests to existing `*_test.go` files. The mapping:
- `server/api.go` -> `server/api_test.go`
- `server/connections.go` -> `server/connections_test.go`
- `server/inbound.go` -> `server/inbound_test.go`
- `server/nats_provider.go` -> `server/nats_test.go`
- `server/azure_provider.go` -> `server/azure_provider_test.go`
- `server/command.go` -> `server/command_test.go`
- `server/service.go` -> `server/service_test.go`
- `server/plugin.go` -> `server/plugin_test.go`
- `server/prompt.go` -> `server/prompt_test.go`
- `server/sync_user.go` -> `server/sync_user_test.go`
- `server/hooks.go` -> `server/hooks_test.go`
- `server/configuration.go` -> `server/configuration_test.go`
- `server/store/caching.go` -> `server/store/caching_test.go`
- `server/store/client.go` -> `server/store/client_test.go`
- `server/model/message.go` -> `server/model/message_test.go`
- `server/model/post_message.go` -> `server/model/post_message_test.go`
- `server/model/test_message.go` -> `server/model/test_message_test.go`

## Step 4: Implement in Phases

Work through tiers in order. After each phase, validate before moving on.

**Phase 1 - Quick wins (0% coverage, simple functions):**
Functions with straightforward logic that just need basic test coverage. Often these are small utility functions, error type methods, or simple delegating wrappers.

**Phase 2 - Integration tests (0% coverage, require embedded NATS):**
Provider functions that need a real NATS server. Use `startEmbeddedNATS(t)` and `connectToEmbeddedNATS(t, addr, subject)`.

**Phase 3 - Branch coverage (low coverage functions):**
Functions that have tests but miss important branches. Read existing tests carefully to avoid duplication, then add subtests for uncovered paths.

**Phase 4 - Edge cases and complex logic:**
Message splitting (UTF-8 boundaries), file handling (retry exhaustion, filter policies), health check logic (recheck windows), concurrent scenarios (semaphore full, CAS retry).

**Phase 5 - Interface extraction for testability (if needed):**
If a dependency uses a concrete SDK type that cannot be mocked (e.g., Azure blob client), extract an interface to enable unit testing. Follow the existing `azureQueuer` pattern in `azure_provider.go`.

## Step 5: Validate After Each Phase

After writing each batch of tests:

```bash
# Tests compile and pass
make test

# No lint issues (fixes import formatting)
make check-style

# Coverage improved
make coverage-backend 2>&1
```

Compare coverage numbers against the baseline from Step 1. If a function you targeted is still at 0%, your test isn't exercising the right code path. Re-read the source and fix.

## Step 6: Final Verification

After all phases complete:

```bash
# Full test suite passes
make test

# Style checks pass
make check-style

# Print final coverage summary
make coverage-backend 2>&1
```

Report the before/after coverage delta per package and overall.

## Common Pitfalls

- **Testing the mock, not the code**: ensure your mock setup actually forces the code path you intend. If a mock returns nil where the real code would return data, you may be testing a different branch.
- **Forgetting `defer api.AssertExpectations(t)`**: without this, unmet mock expectations silently pass.
- **Not reading existing tests first**: you'll write duplicates or miss established patterns.
- **Over-mocking**: if a function is pure logic with no dependencies, test it directly without mocks.
- **Ignoring goroutine cleanup**: any test that spawns goroutines (via Subscribe, WatchFiles, etc.) must cancel the context and wait for completion to avoid test pollution.
- **Flaky time-dependent tests**: use short, generous timeouts in tests. Prefer channels and synchronization over `time.Sleep`.
