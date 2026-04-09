import {test, expect} from '@playwright/test';

import {
    setChannelConnections,
    getChannelConnections,
    subscribe,
    fetchChannelConnections,
} from './connection_state';

const trackedIds: string[] = [];

function trackSet(channelId: string, connections: string) {
    trackedIds.push(channelId);
    setChannelConnections(channelId, connections);
}

let originalFetch: typeof globalThis.fetch;
let originalWarn: typeof console.warn;
let warnCalls: unknown[][];

test.beforeEach(() => {
    originalFetch = globalThis.fetch;
    originalWarn = console.warn; // eslint-disable-line no-console
    warnCalls = [];
    console.warn = (...args: unknown[]) => { // eslint-disable-line no-console
        warnCalls.push(args);
    };
});

test.afterEach(() => {
    globalThis.fetch = originalFetch;
    console.warn = originalWarn; // eslint-disable-line no-console
    for (const id of trackedIds) {
        setChannelConnections(id, '');
    }
    trackedIds.length = 0;
});

function mockFetch(response: {ok: boolean; status?: number; body?: Record<string, string>}) {
    globalThis.fetch = (async () => ({
        ok: response.ok,
        status: response.status || (response.ok ? 200 : 500),
        json: async () => response.body || {},
    })) as unknown as typeof globalThis.fetch;
}

function mockFetchThrow(error: Error) {
    globalThis.fetch = (async () => {
        throw error;
    }) as typeof globalThis.fetch;
}

// ----------------------------------------------------------------
// Cross-function integration
// ----------------------------------------------------------------
test.describe('cross-function integration', () => {
    test('fetchChannelConnections updates state readable by getChannelConnections', async () => {
        mockFetch({ok: true, body: {'ch-int-1': 'ConnAlpha', 'ch-int-2': 'ConnBeta'}});
        trackedIds.push('ch-int-1', 'ch-int-2');

        await fetchChannelConnections(['ch-int-1', 'ch-int-2']);

        expect(getChannelConnections('ch-int-1')).toBe('ConnAlpha');
        expect(getChannelConnections('ch-int-2')).toBe('ConnBeta');
    });

    test('fetchChannelConnections triggers subscriber notification for each channel', async () => {
        const notifiedChannels: string[] = [];
        const unsub = subscribe(() => {
            // Snapshot current state on each notification to track progression
            for (const id of ['ch-notify-a', 'ch-notify-b', 'ch-notify-c']) {
                const val = getChannelConnections(id);
                if (val && !notifiedChannels.includes(id)) {
                    notifiedChannels.push(id);
                }
            }
        });

        mockFetch({
            ok: true,
            body: {
                'ch-notify-a': 'A',
                'ch-notify-b': 'B',
                'ch-notify-c': 'C',
            },
        });
        trackedIds.push('ch-notify-a', 'ch-notify-b', 'ch-notify-c');

        await fetchChannelConnections(['ch-notify-a', 'ch-notify-b', 'ch-notify-c']);

        // All three channels should have triggered notifications
        expect(notifiedChannels).toContain('ch-notify-a');
        expect(notifiedChannels).toContain('ch-notify-b');
        expect(notifiedChannels).toContain('ch-notify-c');
        unsub();
    });

    test('fetchChannelConnections clears state for channels not in response', async () => {
        // Pre-populate both channels
        trackSet('ch-partial-1', 'ExistingA');
        trackSet('ch-partial-2', 'ExistingB');
        expect(getChannelConnections('ch-partial-1')).toBe('ExistingA');
        expect(getChannelConnections('ch-partial-2')).toBe('ExistingB');

        // Response only includes ch-partial-1; ch-partial-2 is omitted
        mockFetch({ok: true, body: {'ch-partial-1': 'Updated'}});
        await fetchChannelConnections(['ch-partial-1', 'ch-partial-2']);

        expect(getChannelConnections('ch-partial-1')).toBe('Updated');
        expect(getChannelConnections('ch-partial-2')).toBe('');
    });

    test('multiple subscribers all notified after fetchChannelConnections', async () => {
        let countA = 0;
        let countB = 0;
        let countC = 0;
        const unsubA = subscribe(() => {
            countA++;
        });
        const unsubB = subscribe(() => {
            countB++;
        });
        const unsubC = subscribe(() => {
            countC++;
        });

        mockFetch({ok: true, body: {'ch-multi-sub': 'Conn'}});
        trackedIds.push('ch-multi-sub');

        await fetchChannelConnections(['ch-multi-sub']);

        // Each subscriber should be called once per setChannelConnections call
        expect(countA).toBe(1);
        expect(countB).toBe(1);
        expect(countC).toBe(1);
        unsubA();
        unsubB();
        unsubC();
    });

    test('subscriber unsubscribes mid-fetch, only active subscribers notified post-fetch', async () => {
        let earlyCount = 0;
        let lateCount = 0;

        // This subscriber will unsubscribe itself on first notification
        const earlyUnsub: () => void = subscribe(() => {
            earlyCount++;
            earlyUnsub();
        });

        const lateUnsub = subscribe(() => {
            lateCount++;
        });

        // Response has two channels, so setChannelConnections fires twice
        mockFetch({ok: true, body: {'ch-unsub-mid-1': 'A', 'ch-unsub-mid-2': 'B'}});
        trackedIds.push('ch-unsub-mid-1', 'ch-unsub-mid-2');

        await fetchChannelConnections(['ch-unsub-mid-1', 'ch-unsub-mid-2']);

        // Early subscriber unsubscribed after first notification, so called once
        expect(earlyCount).toBe(1);

        // Late subscriber stays active for both notifications
        expect(lateCount).toBe(2);
        lateUnsub();
    });
});

// ----------------------------------------------------------------
// State consistency under concurrent operations
// ----------------------------------------------------------------
test.describe('state consistency under concurrent operations', () => {
    test('setChannelConnections during active fetchChannelConnections: manual write visible immediately', async () => {
        let resolveDelay: (() => void) | null = null;
        globalThis.fetch = (async () => {
            await new Promise<void>((r) => {
                resolveDelay = r;
            });
            return {ok: true, json: async () => ({'ch-during-fetch': 'FromFetch'})};
        }) as unknown as typeof globalThis.fetch;

        trackedIds.push('ch-during-fetch');
        const fetchPromise = fetchChannelConnections(['ch-during-fetch']);

        // While fetch is in-flight, manually set a value
        trackSet('ch-during-fetch', 'ManualWrite');
        expect(getChannelConnections('ch-during-fetch')).toBe('ManualWrite');

        // Now resolve the fetch
        resolveDelay!();
        await fetchPromise;

        // Fetch overwrites the manual value after it resolves
        expect(getChannelConnections('ch-during-fetch')).toBe('FromFetch');
    });

    test('fetchChannelConnections followed by immediate setChannelConnections: last write wins', async () => {
        mockFetch({ok: true, body: {'ch-last-write': 'Fetched'}});
        trackedIds.push('ch-last-write');

        await fetchChannelConnections(['ch-last-write']);
        expect(getChannelConnections('ch-last-write')).toBe('Fetched');

        // Immediate manual set after fetch completes
        trackSet('ch-last-write', 'ManualOverride');
        expect(getChannelConnections('ch-last-write')).toBe('ManualOverride');
    });

    test('two concurrent fetchChannelConnections for disjoint channels both succeed', async () => {
        let callCount = 0;
        globalThis.fetch = (async (url: string | URL | Request) => {
            callCount++;
            const urlStr = String(url);
            if (urlStr.includes('ch-disjoint-a')) {
                return {ok: true, json: async () => ({'ch-disjoint-a': 'Alpha'})};
            }
            return {ok: true, json: async () => ({'ch-disjoint-b': 'Beta'})};
        }) as unknown as typeof globalThis.fetch;

        trackedIds.push('ch-disjoint-a', 'ch-disjoint-b');

        await Promise.all([
            fetchChannelConnections(['ch-disjoint-a']),
            fetchChannelConnections(['ch-disjoint-b']),
        ]);

        expect(callCount).toBe(2);
        expect(getChannelConnections('ch-disjoint-a')).toBe('Alpha');
        expect(getChannelConnections('ch-disjoint-b')).toBe('Beta');
    });
});

// ----------------------------------------------------------------
// Error recovery integration
// ----------------------------------------------------------------
test.describe('error recovery integration', () => {
    test('failed fetch followed by successful fetch: state reflects success', async () => {
        // Pre-populate state
        trackSet('ch-recover-1', 'Original');
        trackSet('ch-recover-2', 'Original');

        // First fetch fails with network error
        mockFetchThrow(new Error('connection refused'));
        await fetchChannelConnections(['ch-recover-1', 'ch-recover-2']);

        // State preserved after failure
        expect(getChannelConnections('ch-recover-1')).toBe('Original');
        expect(getChannelConnections('ch-recover-2')).toBe('Original');
        expect(warnCalls.length).toBe(1);

        // Second fetch succeeds
        mockFetch({ok: true, body: {'ch-recover-1': 'Updated1', 'ch-recover-2': 'Updated2'}});
        await fetchChannelConnections(['ch-recover-1', 'ch-recover-2']);

        // State now reflects the successful fetch
        expect(getChannelConnections('ch-recover-1')).toBe('Updated1');
        expect(getChannelConnections('ch-recover-2')).toBe('Updated2');
    });

    test('multiple failed fetches preserve existing state and listener set', async () => {
        // Set initial state and attach a listener
        trackSet('ch-multi-fail', 'Stable');
        let listenerCount = 0;
        const unsub = subscribe(() => {
            listenerCount++;
        });

        // First failure (network error)
        mockFetchThrow(new Error('timeout'));
        await fetchChannelConnections(['ch-multi-fail']);
        expect(getChannelConnections('ch-multi-fail')).toBe('Stable');
        expect(warnCalls.length).toBe(1);

        // Second failure (non-ok response)
        mockFetch({ok: false, status: 503});
        await fetchChannelConnections(['ch-multi-fail']);
        expect(getChannelConnections('ch-multi-fail')).toBe('Stable');
        expect(warnCalls.length).toBe(2);

        // Third failure (json parse error)
        globalThis.fetch = (async () => ({
            ok: true,
            status: 200,
            json: async () => {
                throw new Error('invalid json');
            },
        })) as unknown as typeof globalThis.fetch;
        await fetchChannelConnections(['ch-multi-fail']);
        expect(getChannelConnections('ch-multi-fail')).toBe('Stable');
        expect(warnCalls.length).toBe(3);

        // Listener is still active after all failures
        const countBefore = listenerCount;
        trackSet('ch-multi-fail-verify', 'Proof');
        expect(listenerCount).toBe(countBefore + 1);

        unsub();
    });
});
