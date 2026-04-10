---
name: add-frontend-tests
description: Systematically find frontend test coverage gaps and add exhaustive Playwright unit and component tests. Use when you want to improve React/TypeScript test coverage, add missing tests, or harden existing test suites.
---

# Add Frontend Tests

Systematically analyze frontend coverage, identify gaps, and add exhaustive tests that follow project patterns. You are a frontend testing expert who writes thorough, maintainable Playwright tests.

**This skill runs in two phases: Plan first, then Execute.**

---

## Phase A: Plan (Read-Only)

Enter plan mode, analyze coverage, build a todo list, then exit plan mode for user approval.

### Step 1: Enter Plan Mode

Call `EnterPlanMode` to ensure no edits are made during analysis.

### Step 2: Measure Current Coverage

Run `make coverage-frontend` and capture the output. This runs C8 coverage for both unit tests (Playwright Test) and component tests (Playwright CT).

```bash
make coverage-frontend 2>&1
```

This executes two commands:
- `npm run test:coverage` - unit tests (`.spec.ts`) with C8, reports to `webapp/coverage/`
- `npm run test:pw-ct-coverage` - component tests (`.pw.tsx`) with C8, reports to `webapp/coverage-ct/`

**If overall coverage is 90% or above, stop here.** Report the coverage number, congratulate the user, and exit plan mode. No additional tests are needed. The codebase is well-tested.

If coverage is below 90%, parse the C8 text output to build a prioritized list:
- **Tier 1**: Files/functions at 0% coverage (completely untested)
- **Tier 2**: Files/functions below 60% coverage (significant gaps)
- **Tier 3**: Files/functions below 80% coverage (moderate gaps)
- **Tier 4**: Complex components above 80% with untested edge cases (error states, loading states, empty states)

Skip test infrastructure files (`*Story.tsx`, `PluginTestHarness.tsx`, `TestPlaceholder.tsx`).

### Step 3: Understand What Needs Testing

For each file in your priority list:

1. **Read the source file** to understand the component's logic, props, state, effects, and event handlers
2. **Read the existing test file** (if one exists) to see what's already covered
3. **Identify untested paths**: error states, loading states, empty/null props, user interactions, API failures, edge cases
4. **Note dependencies**: what needs mocking (API routes, global state, browser APIs)

### Step 4: Build Todo List

Use `TaskCreate` to create one task per component or logical group of files needing tests. Each task should include:

- **subject**: `Test <ComponentName> in <file>` (imperative form)
- **description**: Current coverage %, what specifically needs testing (list the untested states, interactions, error paths), and the tier (1-4)

Group related files into a single task when they share setup (e.g., a component and its Story wrapper). Order tasks by tier (Tier 1 first).

Example tasks:
- `Test CrossguardUserPopover error and empty states in CrossguardUserPopover.pw.tsx` (Tier 1, 0% coverage, needs route mocks for API failure + empty user list)
- `Test AdminPanel form validation edge cases in AdminPanel.pw.tsx` (Tier 3, 62% coverage, missing invalid input handling and submit error states)

### Step 5: Exit Plan Mode

Call `ExitPlanMode` to present the plan and todo list to the user for approval.

---

## Phase B: Execute

After the user approves the plan, work through the todo list writing tests.

### Step 6: Write Tests Using Project Patterns

Mark each task as `in_progress` (via `TaskUpdate`) before starting it, and `completed` when tests pass.

### Two Test Types

This project uses two distinct test approaches:

**Unit Tests (`.spec.ts`)** - Run in Node.js via Playwright Test:
- For pure logic, utility functions, state management
- File pattern: `src/<module>.spec.ts`
- Config: `playwright.config.ts`
- Example: `connection_state.spec.ts` tests the connection state module

**Component Tests (`.pw.tsx`)** - Run in browser via Playwright Component Testing:
- For React components with real DOM rendering
- File pattern: `src/components/<Component>.pw.tsx`
- Config: `playwright-ct.config.ts`
- Example: `ConnectionSettings.pw.tsx` tests the ConnectionSettings component

### Test Infrastructure Available

**Story wrapper components for exposing internal state:**
```tsx
// ConnectionSettingsStory.tsx wraps ConnectionSettings
// Captures onChange and setSaveNeeded calls via window.__testCalls
const getCalls = async (page: Page) => {
    return page.evaluate(() => (window as any).__testCalls);
};
```

**Route mocking for API calls:**
```typescript
await page.route('**/plugins/crossguard/api/**', (route) => {
    route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(responseData),
    });
});
```

**Error route mocking:**
```typescript
await page.route('**/plugins/crossguard/api/**', (route) => {
    route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({error: 'Internal server error'}),
    });
});

// Network failure simulation
await page.route('**/plugins/crossguard/api/**', (route) => {
    route.abort('connectionrefused');
});
```

**Modal helpers:**
```typescript
// Open a channel modal via custom DOM event
const openModal = async (page: Page, channelID: string) => {
    await page.evaluate((id) => {
        window.dispatchEvent(new CustomEvent('crossguard-channel-modal', {
            detail: {channelId: id},
        }));
    }, channelID);
};

// Set CSRF cookie for API calls
const setCsrfCookie = async (page: Page) => {
    await page.evaluate(() => {
        Object.defineProperty(document, 'cookie', {
            get: () => 'MMCSRF=test-csrf-token',
        });
    });
};

// Convenience: route + respond with success
const routeStatusOk = async (page: Page, body: object) => {
    await page.route('**/plugins/crossguard/api/**', (route) => {
        route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify(body),
        });
    });
};
```

**Connection state mocking (for unit tests):**
```typescript
// Track and set connections with cleanup
const trackedIds: string[] = [];
function trackSet(channelId: string, connections: string) {
    trackedIds.push(channelId);
    setChannelConnections(channelId, connections);
}
test.afterEach(() => {
    for (const id of trackedIds) {
        setChannelConnections(id, '');
    }
    trackedIds.length = 0;
});

// Mock fetch for API calls
const originalFetch = globalThis.fetch;
globalThis.fetch = (async () => ({
    ok: true,
    json: async () => mockData,
})) as typeof globalThis.fetch;
// Restore in afterEach:
globalThis.fetch = originalFetch;
```

**Browser context evaluation (for plugin class tests):**
```typescript
const result = await page.evaluate(async () => {
    const plugin = new (window as any).__PluginClass();
    const registry = (window as any).__mockRegistry;
    plugin.initialize(registry, store);
    return {
        registerCount: registry.calls.length,
        // ...
    };
});
```

### Testing Patterns to Follow

**Hierarchical test organization:**
```typescript
test.describe('ComponentName', () => {
    test.describe('rendering', () => {
        test('displays title', async ({mount}) => { ... });
        test('shows loading state', async ({mount}) => { ... });
    });
    test.describe('interactions', () => {
        test('handles click', async ({mount, page}) => { ... });
        test('submits form', async ({mount, page}) => { ... });
    });
    test.describe('error handling', () => {
        test('shows error on API failure', async ({mount, page}) => { ... });
        test('handles network error', async ({mount, page}) => { ... });
    });
});
```

**Component mount and assert:**
```typescript
test('renders with props', async ({mount}) => {
    const component = await mount(
        <ConnectionSettingsStory
            id="test"
            value='[{"name":"conn1","provider":"nats"}]'
        />
    );
    await expect(component.getByText('conn1')).toBeVisible();
});
```

**Reactive update testing:**
```typescript
test('updates when props change', async ({mount, page}) => {
    const component = await mount(<CrossguardChannelIndicatorStory connections="" />);
    await expect(page.getByTestId('indicator')).toBeHidden();
    
    await component.update(<CrossguardChannelIndicatorStory connections='[{"name":"c1"}]' />);
    await expect(page.getByTestId('indicator')).toBeVisible();
});
```

**Capturing callbacks via window object:**
```typescript
test('calls onChange when value changes', async ({mount, page}) => {
    await mount(<ConnectionSettingsStory id="test" value="[]" />);
    // ... interact with component ...
    const calls = await page.evaluate(() => (window as any).__testCalls);
    expect(calls.onChange).toHaveLength(1);
    expect(calls.onChange[0]).toContain('new-value');
});
```

### What Makes a Good Frontend Test

- **Tests user-visible behavior**: assert on what the user sees (text, visibility, enabled/disabled), not internal state
- **One behavior per test**: each test verifies one specific interaction or state
- **Descriptive names**: `'shows error message when API returns 500'` not `'test error 1'`
- **Isolated**: each test mounts its own component, no shared state between tests
- **No flaky selectors**: prefer `getByRole`, `getByText`, `getByTestId` over CSS selectors
- **Proper cleanup**: restore mocked globals in `test.afterEach()`
- **No synthetic data to fix failures**: if a test fails, fix the code or the test logic

### What to Test in Each Component

For every component, aim to cover:

1. **Initial render**: correct elements visible with default/provided props
2. **Empty/null states**: empty arrays, null values, undefined props, empty strings
3. **Loading states**: spinners, placeholders, skeleton UI
4. **Error states**: API failures, network errors, invalid data
5. **User interactions**: clicks, form inputs, modal open/close, keyboard events
6. **Reactive updates**: prop changes, state changes, subscription callbacks
7. **Edge cases**: malformed JSON, very long strings, special characters, rapid interactions

For utility modules:

1. **Happy path**: normal successful execution
2. **Error handling**: thrown errors, rejected promises, invalid inputs
3. **Concurrent operations**: multiple calls, race conditions, cleanup during pending operations
4. **State management**: subscription lifecycle, cleanup, memory leaks

### File Organization

Add tests to existing test files or create new ones following the pattern:

**Unit tests:**
- `src/connection_state.ts` -> `src/connection_state.spec.ts`
- `src/manifest.ts` -> `src/manifest.spec.ts`

**Component tests:**
- `src/components/AdminPanel.tsx` -> `src/components/AdminPanel.pw.tsx`
- `src/components/ConnectionSettings.tsx` -> `src/components/ConnectionSettings.pw.tsx`
- `src/components/CrossguardChannelIndicator.tsx` -> `src/components/CrossguardChannelIndicator.pw.tsx`
- `src/components/CrossguardChannelModal.tsx` -> `src/components/CrossguardChannelModal.pw.tsx`
- `src/components/CrossguardTeamModal.tsx` -> `src/components/CrossguardTeamModal.pw.tsx`
- `src/components/CrossguardUserPopover.tsx` -> `src/components/CrossguardUserPopover.pw.tsx`

**Edge case tests (separate files for large components):**
- `src/components/ConnectionSettings_edge_cases.pw.tsx`
- `src/components/CrossguardChannelModal_edge_cases.pw.tsx`
- `src/components/CrossguardTeamModal_edge_cases.pw.tsx`
- `src/index_edge_cases.pw.tsx`

**Plugin class tests:**
- `src/index.tsx` -> `src/index.pw.tsx`

## Step 7: Implement in Phases

Work through tiers in order. After each phase, validate before moving on.

**Phase 1 - Quick wins (0% coverage, simple components):**
Components or utilities with no tests at all. Start with the simplest ones to build momentum.

**Phase 2 - Component tests (low coverage components):**
Components that have some tests but miss important states (error, loading, empty). Add edge case test files if the main test file is already large.

**Phase 3 - Interaction and integration tests:**
Test complex user flows: modal open/close/submit, form validation, multi-step interactions, API call chains.

**Phase 4 - Edge cases and error resilience:**
Malformed JSON inputs, network failures, rapid user interactions, concurrent state updates, cleanup during unmount.

**Phase 5 - Story wrapper creation (if needed):**
If a component needs internal state exposed for testing, create a `*Story.tsx` wrapper following the existing pattern in `ConnectionSettingsStory.tsx`. The story component captures callbacks via `window.__testCalls`.

## Step 8: Validate After Each Phase

After writing each batch of tests:

```bash
# Unit tests pass
cd webapp && npm run test

# Component tests pass
cd webapp && npm run test:pw-ct

# No lint issues
make check-style

# Coverage improved
make coverage-frontend 2>&1
```

Compare coverage numbers against the baseline from Step 2. If a file you targeted still shows low coverage, your tests aren't exercising the right code paths. Re-read the source and fix.

Mark completed tasks via `TaskUpdate` with `status: "completed"` as each batch passes.

## Step 9: Final Verification

After all phases complete:

```bash
# Full test suite passes
make test

# Style checks pass
make check-style

# Print final coverage summary
make coverage-frontend 2>&1
```

Report the before/after coverage delta per file and overall.

## Common Pitfalls

- **Testing the wrapper, not the component**: ensure your mounts and assertions exercise the actual component logic, not just the story wrapper.
- **Not restoring mocked globals**: if you mock `globalThis.fetch` or `console.warn`, always restore in `test.afterEach()`. Leaked mocks cause cascading test failures.
- **Not reading existing tests first**: you'll write duplicates or miss established helper functions.
- **Flaky async assertions**: use `await expect(locator).toBeVisible()` (auto-retrying) instead of checking once. Playwright's auto-waiting handles timing.
- **Forgetting route mocks**: component tests that make API calls will fail or hang without `page.route()` setup. Mock all expected API calls.
- **CSS selector fragility**: prefer semantic locators (`getByRole`, `getByText`, `getByTestId`) over CSS class selectors which break on style changes.
- **Missing cleanup in afterEach**: tracked state (connections, subscriptions, mocked globals) must be cleaned up to prevent test pollution.
- **Testing implementation details**: don't assert on internal state or React hooks. Test what the user sees and what callbacks fire.
