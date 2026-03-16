import manifest from 'manifest';

const connectionState: Record<string, string> = {};

const listeners: Set<() => void> = new Set();

function notifyListeners() {
    for (const fn of listeners) {
        fn();
    }
}

export function setChannelConnections(channelId: string, connections: string) {
    if (connections) {
        connectionState[channelId] = connections;
    } else {
        delete connectionState[channelId];
    }
    notifyListeners();
}

export function getChannelConnections(channelId: string): string {
    return connectionState[channelId] || '';
}

export function subscribe(fn: () => void): () => void {
    listeners.add(fn);
    return () => {
        listeners.delete(fn);
    };
}

export async function fetchChannelConnections(channelIds: string[]): Promise<void> {
    if (channelIds.length === 0) {
        return;
    }

    try {
        const resp = await fetch(
            `/plugins/${manifest.id}/api/v1/channels/connections?ids=${channelIds.join(',')}`,
            {
                credentials: 'same-origin',
                headers: {'X-Requested-With': 'XMLHttpRequest'},
            },
        );
        if (!resp.ok) {
            // eslint-disable-next-line no-console
            console.warn('fetchChannelConnections: non-ok response', resp.status);
            return;
        }
        const data: Record<string, string> = await resp.json();
        for (const id of channelIds) {
            setChannelConnections(id, data[id] || '');
        }
    } catch (e) {
        // eslint-disable-next-line no-console
        console.warn('fetchChannelConnections failed', e);
    }
}
