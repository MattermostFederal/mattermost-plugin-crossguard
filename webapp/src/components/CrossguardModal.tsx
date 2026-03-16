import manifest from 'manifest';
import React from 'react';

interface ConnectionStatus {
    name: string;
    linked: boolean;
}

interface ChannelStatusResponse {
    channel_id: string;
    channel_name: string;
    team_name: string;
    team_connections: ConnectionStatus[];
}

interface Status {
    loading: boolean;
    success?: boolean;
    message?: string;
}

function getCSRFToken(): string {
    const match = document.cookie.match(/MMCSRF=([^;]+)/);
    return match ? match[1] : '';
}

function formatConnectionLabel(name: string, teamName: string): string {
    const colonIdx = name.indexOf(':');
    if (colonIdx === -1) {
        return name;
    }
    const direction = name.substring(0, colonIdx);
    const connName = name.substring(colonIdx + 1);
    if (direction === 'inbound') {
        return `NATS.io (${connName})  \u2192  Mattermost (${teamName})`;
    }
    return `Mattermost (${teamName})  \u2192  NATS.io (${connName})`;
}

const colors = {
    primary: '#1C58D9',
    danger: '#D24B4E',
    border: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.16)',
    bg: 'var(--center-channel-bg, #fff)',
    text: 'var(--center-channel-color, #3D3C40)',
    textMuted: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.64)',
    textSubtle: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.48)',
};

const STATUS_DISPLAY_MS = 5000;

const CrossguardModal: React.FC = () => {
    const [channelID, setChannelID] = React.useState<string | null>(null);
    const [teamName, setTeamName] = React.useState('');
    const [teamConnections, setTeamConnections] = React.useState<ConnectionStatus[]>([]);
    const [fetching, setFetching] = React.useState(false);
    const [status, setStatus] = React.useState<Status>({loading: false});
    const [actionInProgress, setActionInProgress] = React.useState<string | null>(null);
    const statusTimerRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);

    React.useEffect(() => {
        return () => {
            if (statusTimerRef.current) {
                clearTimeout(statusTimerRef.current);
            }
        };
    }, []);

    React.useEffect(() => {
        const handler = (e: Event) => {
            const detail = (e as CustomEvent).detail;
            if (detail?.channelID) {
                setChannelID(detail.channelID);
                setTeamName('');
                setTeamConnections([]);
                setStatus({loading: false});
                setActionInProgress(null);
            }
        };
        document.addEventListener('crossguard:open-modal', handler);
        return () => document.removeEventListener('crossguard:open-modal', handler);
    }, []);

    const fetchStatus = React.useCallback(async (chID: string) => {
        setFetching(true);
        try {
            const response = await fetch(`/plugins/${manifest.id}/api/v1/channels/${chID}/status`, {
                credentials: 'same-origin',
                headers: {'X-Requested-With': 'XMLHttpRequest'},
            });
            if (!response.ok) {
                const data = await response.json();
                setStatus({loading: false, success: false, message: data.error || 'Failed to load channel status.'});
                return;
            }
            const data: ChannelStatusResponse = await response.json();
            setTeamName(data.team_name);
            setTeamConnections(data.team_connections || []);
        } catch {
            setStatus({loading: false, success: false, message: 'Network error loading channel status.'});
        } finally {
            setFetching(false);
        }
    }, []);

    React.useEffect(() => {
        if (channelID) {
            fetchStatus(channelID);
        }
    }, [channelID, fetchStatus]);

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

    const handleToggle = React.useCallback(async (connName: string, linked: boolean) => {
        if (!channelID) {
            return;
        }
        const action = linked ? 'teardown' : 'init';
        const verb = linked ? 'unlinked' : 'linked';
        const failVerb = linked ? 'unlink' : 'link';
        setActionInProgress(connName);
        setStatus({loading: true});
        if (statusTimerRef.current) {
            clearTimeout(statusTimerRef.current);
        }
        try {
            const url = `/plugins/${manifest.id}/api/v1/channels/${channelID}/${action}?connection_name=${encodeURIComponent(connName)}`;
            const {ok, data} = await callAPI(url);
            if (ok) {
                setStatus({loading: false, success: true, message: `Connection "${connName}" ${verb}.`});
            } else {
                setStatus({loading: false, success: false, message: (data.error as string) || `Failed to ${failVerb} connection.`});
            }
            await fetchStatus(channelID);
            statusTimerRef.current = setTimeout(() => setStatus({loading: false}), STATUS_DISPLAY_MS);
        } catch {
            setStatus({loading: false, success: false, message: 'Network error.'});
            statusTimerRef.current = setTimeout(() => setStatus({loading: false}), STATUS_DISPLAY_MS);
        } finally {
            setActionInProgress(null);
        }
    }, [callAPI, channelID, fetchStatus]);

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
            width: '560px',
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
        table: {
            width: '100%',
            borderCollapse: 'collapse' as const,
        },
        th: {
            textAlign: 'left' as const,
            padding: '8px 12px',
            fontSize: '12px',
            fontWeight: 600,
            color: colors.textMuted,
            borderBottom: `1px solid ${colors.border}`,
            textTransform: 'uppercase' as const,
            letterSpacing: '0.5px',
        },
        td: {
            padding: '10px 12px',
            fontSize: '14px',
            borderBottom: `1px solid ${colors.border}`,
            verticalAlign: 'middle' as const,
        },
        btnLink: {
            padding: '6px 16px',
            cursor: 'pointer',
            border: 'none',
            borderRadius: '4px',
            background: colors.primary,
            color: '#fff',
            fontSize: '13px',
            fontWeight: 600,
        },
        btnUnlink: {
            padding: '6px 16px',
            cursor: 'pointer',
            border: 'none',
            borderRadius: '4px',
            background: colors.danger,
            color: '#fff',
            fontSize: '13px',
            fontWeight: 600,
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
        emptyState: {
            fontSize: '14px',
            color: colors.textSubtle,
            textAlign: 'center' as const,
            padding: '24px 0',
        },
    };

    const renderBody = () => {
        if (fetching) {
            return <p style={{...modalStyles.emptyState, color: colors.textMuted}}>{'Loading...'}</p>;
        }

        if (teamConnections.length === 0) {
            return (
                <p style={modalStyles.emptyState}>
                    {'No connections available. Configure connections in the System Console.'}
                </p>
            );
        }

        return (
            <table style={modalStyles.table}>
                <thead>
                    <tr>
                        <th style={modalStyles.th}>{'Connection'}</th>
                        <th style={{...modalStyles.th, textAlign: 'right' as const, width: '100px'}}>{'Action'}</th>
                    </tr>
                </thead>
                <tbody>
                    {teamConnections.map((conn) => {
                        const isActioning = actionInProgress === conn.name;
                        return (
                            <tr key={conn.name}>
                                <td style={modalStyles.td}>
                                    {formatConnectionLabel(conn.name, teamName)}
                                </td>
                                <td style={{...modalStyles.td, textAlign: 'right' as const}}>
                                    {conn.linked ? (
                                        <button
                                            style={modalStyles.btnUnlink}
                                            onClick={() => handleToggle(conn.name, true)}
                                            disabled={actionInProgress !== null}
                                        >
                                            {isActioning ? 'Unlinking...' : 'Unlink'}
                                        </button>
                                    ) : (
                                        <button
                                            style={modalStyles.btnLink}
                                            onClick={() => handleToggle(conn.name, false)}
                                            disabled={actionInProgress !== null}
                                        >
                                            {isActioning ? 'Linking...' : 'Link'}
                                        </button>
                                    )}
                                </td>
                            </tr>
                        );
                    })}
                </tbody>
            </table>
        );
    };

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
                    {renderBody()}
                    {renderStatus()}
                </div>
            </div>
        </div>
    );
};

export default CrossguardModal;
