import manifest from 'manifest';
import React from 'react';
import {useSelector} from 'react-redux';

type View = 'actions' | 'select-connection';
type PendingAction = 'team' | 'channel' | null;

interface Status {
    loading: boolean;
    success?: boolean;
    message?: string;
}

function getCSRFToken(): string {
    const match = document.cookie.match(/MMCSRF=([^;]+)/);
    return match ? match[1] : '';
}

const colors = {
    primary: '#1C58D9',
    primaryHover: '#1851C4',
    danger: '#D24B4E',
    success: '#3DB887',
    border: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.16)',
    bg: 'var(--center-channel-bg, #fff)',
    text: 'var(--center-channel-color, #3D3C40)',
    textMuted: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.64)',
    textSubtle: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.48)',
};

const STATUS_DISPLAY_MS = 5000;

const CrossguardModal: React.FC = () => {
    const [channelID, setChannelID] = React.useState<string | null>(null);
    const [view, setView] = React.useState<View>('actions');
    const [pendingAction, setPendingAction] = React.useState<PendingAction>(null);
    const [connections, setConnections] = React.useState<string[]>([]);
    const [selectedConnection, setSelectedConnection] = React.useState('');
    const [status, setStatus] = React.useState<Status>({loading: false});

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const currentTeamId = useSelector((state: any) => state.entities?.teams?.currentTeamId || '');

    React.useEffect(() => {
        const handler = (e: Event) => {
            const detail = (e as CustomEvent).detail;
            if (detail?.channelID) {
                setChannelID(detail.channelID);
                setView('actions');
                setPendingAction(null);
                setConnections([]);
                setSelectedConnection('');
                setStatus({loading: false});
            }
        };
        document.addEventListener('crossguard:open-modal', handler);
        return () => document.removeEventListener('crossguard:open-modal', handler);
    }, []);

    React.useEffect(() => {
        if (!channelID) {
            return undefined;
        }
        const handler = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                setChannelID(null);
            }
        };
        document.addEventListener('keydown', handler);
        return () => document.removeEventListener('keydown', handler);
    }, [channelID]);

    const callAPI = React.useCallback(async (url: string): Promise<{ok: boolean; data: Record<string, unknown>}> => {
        const response = await fetch(url, {
            method: 'POST',
            credentials: 'same-origin',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': getCSRFToken(),
                'X-Requested-With': 'XMLHttpRequest',
            },
        });
        const data = await response.json();
        return {ok: response.ok, data};
    }, []);

    const handleAction = React.useCallback(async (action: 'team' | 'channel', connectionName?: string) => {
        setStatus({loading: true});

        let url: string;
        if (action === 'team') {
            url = `/plugins/${manifest.id}/api/v1/teams/${currentTeamId}/init`;
        } else {
            url = `/plugins/${manifest.id}/api/v1/channels/${channelID}/init`;
        }

        if (connectionName) {
            url += `?connection_name=${encodeURIComponent(connectionName)}`;
        }

        try {
            const {ok, data} = await callAPI(url);

            if (ok) {
                const label = action === 'team' ? 'Team' : 'Channel';
                setStatus({loading: false, success: true, message: `${label} initialized for connection "${data.connection_name}".`});
                setView('actions');
                setPendingAction(null);
                setTimeout(() => setStatus({loading: false}), STATUS_DISPLAY_MS);
                return;
            }

            if (data.connections && Array.isArray(data.connections)) {
                setConnections(data.connections as string[]);
                setSelectedConnection((data.connections as string[])[0] || '');
                setPendingAction(action);
                setView('select-connection');
                setStatus({loading: false});
                return;
            }

            setStatus({loading: false, success: false, message: (data.error as string) || 'Request failed.'});
            setTimeout(() => setStatus({loading: false}), STATUS_DISPLAY_MS);
        } catch (err) {
            const message = err instanceof Error ? err.message : 'Network error';
            setStatus({loading: false, success: false, message});
            setTimeout(() => setStatus({loading: false}), STATUS_DISPLAY_MS);
        }
    }, [callAPI, channelID, currentTeamId]);

    const handleConfirmConnection = React.useCallback(() => {
        if (pendingAction && selectedConnection) {
            handleAction(pendingAction, selectedConnection);
        }
    }, [handleAction, pendingAction, selectedConnection]);

    const handleBackdropClick = React.useCallback((e: React.MouseEvent) => {
        if (e.target === e.currentTarget) {
            setChannelID(null);
        }
    }, []);

    if (!channelID) {
        return null;
    }

    const modalStyles: Record<string, React.CSSProperties> = {
        backdrop: {
            position: 'fixed',
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            background: 'rgba(0, 0, 0, 0.5)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 10000,
        },
        modal: {
            background: colors.bg,
            borderRadius: '8px',
            width: '480px',
            maxWidth: '90vw',
            boxShadow: '0 12px 32px rgba(0, 0, 0, 0.2)',
            color: colors.text,
        },
        header: {
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: '20px 24px 16px',
            borderBottom: `1px solid ${colors.border}`,
        },
        title: {
            fontSize: '18px',
            fontWeight: 600,
            margin: 0,
        },
        closeBtn: {
            background: 'none',
            border: 'none',
            fontSize: '20px',
            cursor: 'pointer',
            color: colors.textMuted,
            padding: '4px 8px',
            lineHeight: 1,
        },
        body: {
            padding: '24px',
        },
        btnPrimary: {
            display: 'inline-flex',
            alignItems: 'center',
            gap: '6px',
            padding: '10px 20px',
            cursor: 'pointer',
            border: 'none',
            borderRadius: '4px',
            background: colors.primary,
            color: '#fff',
            fontSize: '14px',
            fontWeight: 600,
            width: '100%',
            justifyContent: 'center',
        },
        btnSecondary: {
            display: 'inline-flex',
            alignItems: 'center',
            gap: '6px',
            padding: '10px 20px',
            cursor: 'pointer',
            border: `1px solid ${colors.border}`,
            borderRadius: '4px',
            background: colors.bg,
            color: colors.text,
            fontSize: '14px',
            fontWeight: 600,
            width: '100%',
            justifyContent: 'center',
        },
        statusBanner: {
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            padding: '8px 12px',
            borderRadius: '4px',
            fontSize: '13px',
            fontWeight: 500,
            marginTop: '16px',
        },
        statusSuccess: {
            background: 'rgba(61, 184, 135, 0.12)',
            color: '#1B8A5C',
        },
        statusError: {
            background: 'rgba(210, 75, 78, 0.12)',
            color: colors.danger,
        },
        select: {
            width: '100%',
            padding: '10px 12px',
            border: `1px solid ${colors.border}`,
            borderRadius: '4px',
            fontSize: '14px',
            color: colors.text,
            background: colors.bg,
            marginBottom: '16px',
        },
        label: {
            display: 'block',
            marginBottom: '8px',
            fontWeight: 600,
            fontSize: '13px',
            color: colors.text,
        },
        helpText: {
            fontSize: '12px',
            color: colors.textSubtle,
            marginBottom: '16px',
            lineHeight: '18px',
        },
    };

    const renderActions = () => (
        <div>
            <p style={{...modalStyles.helpText, marginBottom: '20px', marginTop: 0}}>
                {'Initialize the current team or channel for cross-domain message relay.'}
            </p>
            <div style={{display: 'flex', flexDirection: 'column', gap: '12px'}}>
                <button
                    style={modalStyles.btnPrimary}
                    onClick={() => handleAction('team')}
                    disabled={status.loading}
                >
                    {status.loading && pendingAction === 'team' ? 'Initializing...' : 'Init Team'}
                </button>
                <button
                    style={modalStyles.btnPrimary}
                    onClick={() => handleAction('channel')}
                    disabled={status.loading}
                >
                    {status.loading && pendingAction === 'channel' ? 'Initializing...' : 'Init Channel'}
                </button>
            </div>
            {renderStatus()}
        </div>
    );

    const renderConnectionSelector = () => (
        <div>
            <p style={{...modalStyles.helpText, marginTop: 0}}>
                {'Multiple connections are configured. Select which connection to use.'}
            </p>
            <label style={modalStyles.label}>{'Connection'}</label>
            <select
                style={modalStyles.select}
                value={selectedConnection}
                onChange={(e) => setSelectedConnection(e.target.value)}
            >
                {connections.map((name) => (
                    <option
                        key={name}
                        value={name}
                    >
                        {name}
                    </option>
                ))}
            </select>
            <div style={{display: 'flex', gap: '8px'}}>
                <button
                    style={modalStyles.btnPrimary}
                    onClick={handleConfirmConnection}
                    disabled={status.loading || !selectedConnection}
                >
                    {status.loading ? 'Initializing...' : 'Confirm'}
                </button>
                <button
                    style={modalStyles.btnSecondary}
                    onClick={() => {
                        setView('actions');
                        setPendingAction(null);
                    }}
                >
                    {'Back'}
                </button>
            </div>
            {renderStatus()}
        </div>
    );

    const renderStatus = () => {
        if (status.loading || status.success === undefined) {
            return null;
        }
        const bannerStyle = {
            ...modalStyles.statusBanner,
            ...(status.success ? modalStyles.statusSuccess : modalStyles.statusError),
        };
        return (
            <div style={bannerStyle}>
                {status.message}
            </div>
        );
    };

    return (
        <div
            style={modalStyles.backdrop}
            onClick={handleBackdropClick}
        >
            <div style={modalStyles.modal}>
                <div style={modalStyles.header}>
                    <h2 style={modalStyles.title}>{'Crossguard'}</h2>
                    <button
                        style={modalStyles.closeBtn}
                        onClick={() => setChannelID(null)}
                    >
                        {'\u00D7'}
                    </button>
                </div>
                <div style={modalStyles.body}>
                    {view === 'actions' && renderActions()}
                    {view === 'select-connection' && renderConnectionSelector()}
                </div>
            </div>
        </div>
    );
};

export default CrossguardModal;
