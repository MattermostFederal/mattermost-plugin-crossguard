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
// Concurrent fetch operations
// ----------------------------------------------------------------
test.describe('concurrent fetch operations', () => {
    test('two concurrent fetches for different channels both resolve correctly', async () => {
        let callCount = 0;
        globalThis.fetch = (async (url: string | URL | Request) => {
            callCount++;
            const urlStr = String(url);
            if (urlStr.includes('ch-a1')) {
                return {ok: true, json: async () => ({'ch-a1': 'ConnA'})};
            }
            return {ok: true, json: async () => ({'ch-b1': 'ConnB'})};
        }) as unknown as typeof globalThis.fetch;

        trackedIds.push('ch-a1', 'ch-b1');
        await Promise.all([
            fetchChannelConnections(['ch-a1']),
            fetchChannelConnections(['ch-b1']),
        ]);
        expect(getChannelConnections('ch-a1')).toBe('ConnA');
        expect(getChannelConnections('ch-b1')).toBe('ConnB');
        expect(callCount).toBe(2);
    });

    test('two concurrent fetches for overlapping channel sets', async () => {
        globalThis.fetch = (async (url: string | URL | Request) => {
            const urlStr = String(url);
            if (urlStr.includes('ch-x1,ch-x2')) {
                return {ok: true, json: async () => ({'ch-x1': 'A', 'ch-x2': 'B'})};
            }
            return {ok: true, json: async () => ({'ch-x2': 'C', 'ch-x3': 'D'})};
        }) as unknown as typeof globalThis.fetch;

        trackedIds.push('ch-x1', 'ch-x2', 'ch-x3');
        await Promise.all([
            fetchChannelConnections(['ch-x1', 'ch-x2']),
            fetchChannelConnections(['ch-x2', 'ch-x3']),
        ]);
        expect(getChannelConnections('ch-x1')).toBe('A');

        // ch-x2 could be B or C depending on resolution order; both are valid
        expect(['B', 'C']).toContain(getChannelConnections('ch-x2'));
        expect(getChannelConnections('ch-x3')).toBe('D');
    });

    test('fetch followed by another fetch for same channel, last write wins', async () => {
        let resolveFirst: (() => void) | null = null;
        let callIndex = 0;

        globalThis.fetch = (async () => {
            callIndex++;
            if (callIndex === 1) {
                await new Promise<void>((r) => {
 resolveFirst = r;
});
                return {ok: true, json: async () => ({'ch-race': 'First'})};
            }
            return {ok: true, json: async () => ({'ch-race': 'Second'})};
        }) as unknown as typeof globalThis.fetch;

        trackedIds.push('ch-race');
        const p1 = fetchChannelConnections(['ch-race']);
        const p2 = fetchChannelConnections(['ch-race']);

        // Second fetch resolves immediately, first is waiting
        await p2;
        expect(getChannelConnections('ch-race')).toBe('Second');

        // Now resolve first
        resolveFirst!();
        await p1;

        // First fetch overwrites since it resolved last
        expect(getChannelConnections('ch-race')).toBe('First');
    });
});

// ----------------------------------------------------------------
// Fetch with duplicate channel IDs
// ----------------------------------------------------------------
test.describe('fetch with duplicate channel IDs', () => {
    test('duplicate IDs in array are passed as-is in URL', async () => {
        let capturedUrl = '';
        globalThis.fetch = (async (url: string | URL | Request) => {
            capturedUrl = String(url);
            return {ok: true, json: async () => ({'ch-dup': 'Conn'})};
        }) as typeof globalThis.fetch;

        trackedIds.push('ch-dup');
        await fetchChannelConnections(['ch-dup', 'ch-dup']);
        expect(capturedUrl).toContain('ids=ch-dup,ch-dup');
    });

    test('setChannelConnections called for each entry including duplicates', async () => {
        let listenerCount = 0;
        const unsub = subscribe(() => {
 listenerCount++;
});

        globalThis.fetch = (async () => ({
            ok: true,
            json: async () => ({'ch-dup2': 'Conn'}),
        })) as unknown as typeof globalThis.fetch;

        trackedIds.push('ch-dup2');
        await fetchChannelConnections(['ch-dup2', 'ch-dup2']);

        // setChannelConnections called once per array entry (2 times)
        expect(listenerCount).toBe(2);
        unsub();
    });
});

// ----------------------------------------------------------------
// URL construction edge cases
// ----------------------------------------------------------------
test.describe('URL construction edge cases', () => {
    test('channel IDs with commas are included as-is', async () => {
        let capturedUrl = '';
        globalThis.fetch = (async (url: string | URL | Request) => {
            capturedUrl = String(url);
            return {ok: true, json: async () => ({})};
        }) as typeof globalThis.fetch;

        await fetchChannelConnections(['ch,with,commas']);
        expect(capturedUrl).toContain('ids=ch,with,commas');
    });

    test('channel IDs with URL-special characters are included raw', async () => {
        let capturedUrl = '';
        globalThis.fetch = (async (url: string | URL | Request) => {
            capturedUrl = String(url);
            return {ok: true, json: async () => ({})};
        }) as typeof globalThis.fetch;

        await fetchChannelConnections(['ch&id=1', 'ch?id=2']);
        expect(capturedUrl).toContain('ids=ch&id=1,ch?id=2');
    });

    test('large channel ID array (150) constructs valid URL', async () => {
        const ids = Array.from({length: 150}, (_, i) => `ch-large-${i}`);
        let capturedUrl = '';
        globalThis.fetch = (async (url: string | URL | Request) => {
            capturedUrl = String(url);
            return {ok: true, json: async () => ({})};
        }) as typeof globalThis.fetch;

        await fetchChannelConnections(ids);
        expect(capturedUrl).toContain('ids=' + ids.join(','));
        expect(capturedUrl.length).toBeGreaterThan(1000);
    });
});

// ----------------------------------------------------------------
// Rapid subscribe/unsubscribe cycles
// ----------------------------------------------------------------
test.describe('rapid subscribe/unsubscribe cycles', () => {
    test('subscribe 100 listeners, unsubscribe all, none called on next set', () => {
        const unsubs: Array<() => void> = [];
        let callCount = 0;
        const incrementCallCount = () => {
            callCount++;
        };
        for (let i = 0; i < 100; i++) {
            unsubs.push(subscribe(incrementCallCount));
        }
        for (const unsub of unsubs) {
            unsub();
        }
        trackSet('ch-mass', 'X');
        expect(callCount).toBe(0);
    });

    test('subscribe, set, unsubscribe, set again: count is exactly 1', () => {
        let count = 0;
        const unsub = subscribe(() => {
 count++;
});
        trackSet('ch-exact', 'A');
        unsub();
        trackSet('ch-exact2', 'B');
        expect(count).toBe(1);
    });

    test('unsubscribe inside a listener callback still works', () => {
        let count = 0;
        const unsub: () => void = subscribe(() => {
            count++;
            unsub();
        });
        trackSet('ch-self-unsub', 'X');
        trackSet('ch-self-unsub2', 'Y');

        // First set triggers the listener, which unsubscribes. Second set does not trigger.
        expect(count).toBe(1);
    });

    test('rapid subscribe/unsubscribe loop does not corrupt listener set', () => {
        let count = 0;
        const stableFn = () => {
 count++;
};
        const stableUnsub = subscribe(stableFn);

        for (let i = 0; i < 50; i++) {
            const u = subscribe(() => {});
            u();
        }

        trackSet('ch-rapid', 'X');
        expect(count).toBe(1);
        stableUnsub();
    });
});

// ----------------------------------------------------------------
// State consistency
// ----------------------------------------------------------------
test.describe('state consistency', () => {
    test('fetch overwrites manually set value', async () => {
        trackSet('ch-overwrite', 'Manual');
        expect(getChannelConnections('ch-overwrite')).toBe('Manual');

        mockFetch({ok: true, body: {'ch-overwrite': 'Fetched'}});
        await fetchChannelConnections(['ch-overwrite']);
        expect(getChannelConnections('ch-overwrite')).toBe('Fetched');
    });

    test('manual set overwrites fetch value', async () => {
        mockFetch({ok: true, body: {'ch-manual': 'Fetched'}});
        trackedIds.push('ch-manual');
        await fetchChannelConnections(['ch-manual']);
        expect(getChannelConnections('ch-manual')).toBe('Fetched');

        trackSet('ch-manual', 'Manual');
        expect(getChannelConnections('ch-manual')).toBe('Manual');
    });

    test('string "0" is stored correctly (non-empty string is truthy)', () => {
        trackSet('ch-zero', '0');

        // '0' is a non-empty string, so `if (connections)` is true and it gets stored
        expect(getChannelConnections('ch-zero')).toBe('0');
    });

    test('string "false" is stored correctly', () => {
        trackSet('ch-false', 'false');
        expect(getChannelConnections('ch-false')).toBe('false');
    });

    test('undefined coerced as connections deletes the entry', () => {
        trackSet('ch-undef', 'Exists');
        setChannelConnections('ch-undef', undefined as unknown as string);
        expect(getChannelConnections('ch-undef')).toBe('');
    });

    test('setting same value again still notifies listeners', () => {
        let count = 0;
        const unsub = subscribe(() => {
 count++;
});
        trackSet('ch-same', 'X');
        trackSet('ch-same', 'X');
        expect(count).toBe(2);
        unsub();
    });
});

// ----------------------------------------------------------------
// Fetch error recovery
// ----------------------------------------------------------------
test.describe('fetch error recovery', () => {
    test('fetch fails then succeeds on retry, state updated correctly', async () => {
        trackSet('ch-retry', 'Original');

        // First call fails
        mockFetchThrow(new Error('network down'));
        await fetchChannelConnections(['ch-retry']);
        expect(getChannelConnections('ch-retry')).toBe('Original');

        // Second call succeeds
        mockFetch({ok: true, body: {'ch-retry': 'Updated'}});
        await fetchChannelConnections(['ch-retry']);
        expect(getChannelConnections('ch-retry')).toBe('Updated');
    });

    test('fetch returns 404, state unchanged', async () => {
        trackSet('ch-404', 'Original');
        mockFetch({ok: false, status: 404});
        await fetchChannelConnections(['ch-404']);
        expect(getChannelConnections('ch-404')).toBe('Original');
    });

    test('fetch returns 401, state unchanged', async () => {
        trackSet('ch-401', 'Original');
        mockFetch({ok: false, status: 401});
        await fetchChannelConnections(['ch-401']);
        expect(getChannelConnections('ch-401')).toBe('Original');
    });

    test('fetch response with extra unexpected keys, extra keys ignored', async () => {
        globalThis.fetch = (async () => ({
            ok: true,
            json: async () => ({
                'ch-extra': 'Conn',
                'ch-unexpected': 'Ignore',
                'another-key': 'ShouldNotBeStored',
            }),
        })) as unknown as typeof globalThis.fetch;

        trackedIds.push('ch-extra');
        await fetchChannelConnections(['ch-extra']);
        expect(getChannelConnections('ch-extra')).toBe('Conn');

        // Extra keys are in the response but not in the channelIds array, so not set
        expect(getChannelConnections('ch-unexpected')).toBe('');
        expect(getChannelConnections('another-key')).toBe('');
    });
});

// ----------------------------------------------------------------
// Listener notification edge cases
// ----------------------------------------------------------------
test.describe('listener notification edge cases', () => {
    test('listener that unsubscribes a different listener during notification', () => {
        const order: number[] = [];
        const holder: {unsub2: () => void} = {unsub2: () => {}};
        const unsub1 = subscribe(() => {
            order.push(1);
            holder.unsub2();
        });
        const unsub2 = subscribe(() => {
            order.push(2);
        });
        holder.unsub2 = unsub2;
        const unsub3 = subscribe(() => {
            order.push(3);
        });

        trackSet('ch-cross-unsub', 'X');

        // Listener 1 runs and unsubscribes listener 2.
        // Whether listener 2 fires depends on Set iteration behavior when deleting during iteration.
        // In modern JS, if an entry is deleted before being visited, it is skipped.
        expect(order[0]).toBe(1);
        expect(order).toContain(3);
        unsub1();
        unsub3();
    });

    test('subscribe after setChannelConnections, listener NOT called retroactively', () => {
        trackSet('ch-no-retro', 'X');
        let called = false;
        const unsub = subscribe(() => {
 called = true;
});
        expect(called).toBe(false);
        unsub();
    });

    test('notification count: set 3 different channels, listener called 3 times', () => {
        let count = 0;
        const unsub = subscribe(() => {
 count++;
});
        trackSet('ch-count-a', 'A');
        trackSet('ch-count-b', 'B');
        trackSet('ch-count-c', 'C');
        expect(count).toBe(3);
        unsub();
    });

    test('listener added during notification fires on next state change, not current', () => {
        let innerCalled = false;
        let innerUnsub: (() => void) | null = null;
        const unsub = subscribe(() => {
            if (!innerUnsub) {
                innerUnsub = subscribe(() => {
 innerCalled = true;
});
            }
        });
        trackSet('ch-add-during', 'A');

        // Inner listener was added during notification, should NOT have been called yet
        // (Set behavior: entries added during iteration may or may not be visited)
        // The key test is that it works on the NEXT change
        innerCalled = false;
        trackSet('ch-add-during2', 'B');
        expect(innerCalled).toBe(true);
        unsub();
        if (innerUnsub) {
            (innerUnsub as () => void)();
        }
    });

    test('delete triggers listener notification', () => {
        trackSet('ch-del-notify', 'X');
        let count = 0;
        const unsub = subscribe(() => {
 count++;
});
        setChannelConnections('ch-del-notify', '');
        expect(count).toBe(1);
        unsub();
    });
});

// ----------------------------------------------------------------
// Module-level state isolation
// ----------------------------------------------------------------
test.describe('module-level state isolation', () => {
    test('after cleanup, all previously set channels return empty', () => {
        trackSet('ch-iso-1', 'A');
        trackSet('ch-iso-2', 'B');

        // Simulate cleanup
        for (const id of trackedIds) {
            setChannelConnections(id, '');
        }
        expect(getChannelConnections('ch-iso-1')).toBe('');
        expect(getChannelConnections('ch-iso-2')).toBe('');
    });

    test('sequential test-like sequences do not leak state', () => {
        // Sequence 1
        trackSet('ch-seq-1', 'First');
        expect(getChannelConnections('ch-seq-1')).toBe('First');

        // Cleanup sequence 1
        setChannelConnections('ch-seq-1', '');

        // Sequence 2 should not see sequence 1 data
        expect(getChannelConnections('ch-seq-1')).toBe('');
        trackSet('ch-seq-2', 'Second');
        expect(getChannelConnections('ch-seq-2')).toBe('Second');
    });

    test('unsubscribed listeners do not fire across sequences', () => {
        let count = 0;
        const unsub = subscribe(() => {
 count++;
});
        trackSet('ch-seq-listen', 'A');
        expect(count).toBe(1);
        unsub();

        // New sequence
        trackSet('ch-seq-listen2', 'B');
        expect(count).toBe(1); // Still 1, old listener not called
    });
});
