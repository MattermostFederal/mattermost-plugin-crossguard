import {test, expect} from '@playwright/test';

import {
    setChannelConnections,
    getChannelConnections,
    subscribe,
    fetchChannelConnections,
} from './connection_state';

// Track channel IDs set during each test for cleanup
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

test.describe('setChannelConnections / getChannelConnections', () => {
    test('stores and retrieves a connection string for a channel', () => {
        trackSet('ch-1', 'ConnA');
        expect(getChannelConnections('ch-1')).toBe('ConnA');
    });

    test('returns empty string for unknown channel ID', () => {
        expect(getChannelConnections('nonexistent')).toBe('');
    });

    test('deletes entry when connections is empty string', () => {
        trackSet('ch-2', 'SomeConn');
        expect(getChannelConnections('ch-2')).toBe('SomeConn');
        setChannelConnections('ch-2', '');
        expect(getChannelConnections('ch-2')).toBe('');
    });

    test('overwrites previous value for same channel ID', () => {
        trackSet('ch-3', 'First');
        expect(getChannelConnections('ch-3')).toBe('First');
        trackSet('ch-3', 'Second');
        expect(getChannelConnections('ch-3')).toBe('Second');
    });

    test('handles multiple channel IDs independently', () => {
        trackSet('ch-a', 'ConnA');
        trackSet('ch-b', 'ConnB');
        expect(getChannelConnections('ch-a')).toBe('ConnA');
        expect(getChannelConnections('ch-b')).toBe('ConnB');
    });

    test('handles channel ID with special characters', () => {
        trackSet('ch/special:chars!@#', 'Conn');
        expect(getChannelConnections('ch/special:chars!@#')).toBe('Conn');
    });
});

test.describe('subscribe / notification', () => {
    test('calls listener when setChannelConnections is invoked', () => {
        let called = false;
        const unsub = subscribe(() => {
            called = true;
        });
        trackSet('ch-sub-1', 'X');
        expect(called).toBe(true);
        unsub();
    });

    test('calls multiple listeners on state change', () => {
        let count = 0;
        const unsub1 = subscribe(() => {
            count++;
        });
        const unsub2 = subscribe(() => {
            count++;
        });
        trackSet('ch-sub-2', 'X');
        expect(count).toBe(2);
        unsub1();
        unsub2();
    });

    test('does not call listener after unsubscribe', () => {
        let count = 0;
        const unsub = subscribe(() => {
            count++;
        });
        trackSet('ch-sub-3', 'X');
        expect(count).toBe(1);
        unsub();
        trackSet('ch-sub-3b', 'Y');
        expect(count).toBe(1);
    });

    test('returns working unsubscribe function', () => {
        const unsub = subscribe(() => {});
        expect(typeof unsub).toBe('function');
        unsub();
    });

    test('double-unsubscribe does not throw', () => {
        const unsub = subscribe(() => {});
        unsub();
        expect(() => unsub()).not.toThrow();
    });

    test('listeners fire synchronously in insertion order', () => {
        const order: number[] = [];
        const unsub1 = subscribe(() => order.push(1));
        const unsub2 = subscribe(() => order.push(2));
        const unsub3 = subscribe(() => order.push(3));
        trackSet('ch-sub-order', 'X');
        expect(order).toEqual([1, 2, 3]);
        unsub1();
        unsub2();
        unsub3();
    });

    test('exception in one listener propagates and skips remaining', () => {
        const order: number[] = [];
        const unsub1 = subscribe(() => order.push(1));
        const unsub2 = subscribe(() => {
            throw new Error('boom');
        });
        const unsub3 = subscribe(() => order.push(3));
        expect(() => trackSet('ch-sub-err', 'X')).toThrow('boom');

        // First listener ran, second threw, third was skipped
        expect(order).toEqual([1]);
        unsub1();
        unsub2();
        unsub3();
    });

    test('notifies on delete (empty string) as well', () => {
        trackSet('ch-sub-del', 'Conn');
        let called = false;
        const unsub = subscribe(() => {
            called = true;
        });
        setChannelConnections('ch-sub-del', '');
        expect(called).toBe(true);
        unsub();
    });
});

test.describe('fetchChannelConnections', () => {
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

    test('returns immediately without calling fetch when channelIds is empty', async () => {
        let fetchCalled = false;
        globalThis.fetch = (async () => {
            fetchCalled = true;
            return {ok: true, json: async () => ({})};
        }) as unknown as typeof globalThis.fetch;

        await fetchChannelConnections([]);
        expect(fetchCalled).toBe(false);
    });

    test('calls fetch with correct URL containing comma-joined IDs', async () => {
        let capturedUrl = '';
        globalThis.fetch = (async (url: string | URL | Request) => {
            capturedUrl = String(url);
            return {ok: true, json: async () => ({})};
        }) as typeof globalThis.fetch;

        await fetchChannelConnections(['id1', 'id2']);
        expect(capturedUrl).toBe('/plugins/crossguard/api/v1/channels/connections?ids=id1,id2');
    });

    test('includes credentials same-origin and X-Requested-With header', async () => {
        let capturedInit: RequestInit | undefined;
        globalThis.fetch = (async (_url: string | URL | Request, init?: RequestInit) => {
            capturedInit = init;
            return {ok: true, json: async () => ({})};
        }) as typeof globalThis.fetch;

        await fetchChannelConnections(['id1']);
        expect(capturedInit?.credentials).toBe('same-origin');
        expect((capturedInit?.headers as Record<string, string>)['X-Requested-With']).toBe('XMLHttpRequest');
    });

    test('updates state for each channel ID from response', async () => {
        mockFetch({ok: true, body: {'ch-f1': 'ConnA', 'ch-f2': 'ConnB'}});
        trackedIds.push('ch-f1', 'ch-f2');
        await fetchChannelConnections(['ch-f1', 'ch-f2']);
        expect(getChannelConnections('ch-f1')).toBe('ConnA');
        expect(getChannelConnections('ch-f2')).toBe('ConnB');
    });

    test('clears state when response has empty string for a channel', async () => {
        trackSet('ch-f3', 'ExistingConn');
        mockFetch({ok: true, body: {'ch-f3': ''}});
        await fetchChannelConnections(['ch-f3']);
        expect(getChannelConnections('ch-f3')).toBe('');
    });

    test('clears state when response omits a channel entirely', async () => {
        trackSet('ch-f4', 'ExistingConn');
        mockFetch({ok: true, body: {}});
        await fetchChannelConnections(['ch-f4']);
        expect(getChannelConnections('ch-f4')).toBe('');
    });

    test('notifies listeners after updating state from response', async () => {
        let notified = false;
        const unsub = subscribe(() => {
            notified = true;
        });
        mockFetch({ok: true, body: {'ch-f5': 'Conn'}});
        trackedIds.push('ch-f5');
        await fetchChannelConnections(['ch-f5']);
        expect(notified).toBe(true);
        unsub();
    });

    test('does not update state on non-ok response', async () => {
        trackSet('ch-f6', 'Original');
        mockFetch({ok: false, status: 500, body: {}});
        await fetchChannelConnections(['ch-f6']);
        expect(getChannelConnections('ch-f6')).toBe('Original');
    });

    test('logs console.warn on non-ok response', async () => {
        mockFetch({ok: false, status: 500, body: {}});
        await fetchChannelConnections(['ch-f7']);
        expect(warnCalls.length).toBe(1);
        expect(warnCalls[0][0]).toBe('fetchChannelConnections: non-ok response');
    });

    test('does not update state when fetch throws', async () => {
        trackSet('ch-f8', 'Original');
        mockFetchThrow(new Error('network down'));
        await fetchChannelConnections(['ch-f8']);
        expect(getChannelConnections('ch-f8')).toBe('Original');
    });

    test('logs console.warn when fetch throws', async () => {
        mockFetchThrow(new Error('network down'));
        await fetchChannelConnections(['ch-f9']);
        expect(warnCalls.length).toBe(1);
        expect(warnCalls[0][0]).toBe('fetchChannelConnections failed');
    });

    test('handles single channel ID', async () => {
        mockFetch({ok: true, body: {'ch-single': 'Solo'}});
        trackedIds.push('ch-single');
        await fetchChannelConnections(['ch-single']);
        expect(getChannelConnections('ch-single')).toBe('Solo');
    });
});
