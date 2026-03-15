import manifest from 'manifest';
import React from 'react';

interface NATSConnection {
    name: string;
    address: string;
    subject: string;
    tls_enabled: boolean;
    auth_type: 'none' | 'token' | 'credentials';
    token: string;
    username: string;
    password: string;
    client_cert: string;
    client_key: string;
    ca_cert: string;
}

interface CustomSettingProps {
    id: string;
    value: string;
    onChange: (id: string, value: string) => void;
    setSaveNeeded: () => void;
    disabled: boolean;
    config: object;
    currentState: object;
    license: object;
    setByEnv: boolean;
}

interface TestStatus {
    loading: boolean;
    success?: boolean;
    message?: string;
}

const DEFAULT_NATS_ADDRESS = 'nats://localhost:4222';
const TEST_STATUS_DISPLAY_MS = 10000;
const SUBJECT_PREFIX = 'crossguard.';

function getCSRFToken(): string {
    const match = document.cookie.match(/MMCSRF=([^;]+)/);
    return match ? match[1] : '';
}

const emptyConnection: NATSConnection = {
    name: '',
    address: DEFAULT_NATS_ADDRESS,
    subject: '',
    tls_enabled: false,
    auth_type: 'none',
    token: '',
    username: '',
    password: '',
    client_cert: '',
    client_key: '',
    ca_cert: '',
};

const colors = {
    primary: '#1C58D9',
    primaryHover: '#1851C4',
    danger: '#D24B4E',
    success: '#3DB887',
    border: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.16)',
    borderStrong: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.24)',
    bg: 'var(--center-channel-bg, #fff)',
    bgAlt: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.04)',
    text: 'var(--center-channel-color, #3D3C40)',
    textMuted: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.64)',
    textSubtle: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.48)',
};

const styles = {
    container: {
        padding: '0 0 8px',
    } as React.CSSProperties,
    card: {
        border: `1px solid ${colors.border}`,
        borderRadius: '8px',
        padding: '16px 20px',
        marginBottom: '12px',
        background: colors.bg,
        transition: 'box-shadow 0.15s ease',
    } as React.CSSProperties,
    cardHeader: {
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        marginBottom: '12px',
    } as React.CSSProperties,
    cardTitle: {
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        fontSize: '15px',
        fontWeight: 600,
        color: colors.text,
        margin: 0,
    } as React.CSSProperties,
    cardMeta: {
        display: 'grid',
        gridTemplateColumns: '1fr 1fr',
        gap: '4px 24px',
        marginBottom: '14px',
    } as React.CSSProperties,
    cardMetaItem: {
        fontSize: '13px',
        color: colors.textMuted,
        lineHeight: '20px',
    } as React.CSSProperties,
    cardMetaLabel: {
        fontWeight: 600,
        color: colors.textSubtle,
        marginRight: '4px',
    } as React.CSSProperties,
    cardActions: {
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        paddingTop: '12px',
        borderTop: `1px solid ${colors.border}`,
    } as React.CSSProperties,
    form: {
        border: `1px solid ${colors.primary}`,
        borderRadius: '8px',
        padding: '24px',
        marginBottom: '12px',
        background: colors.bg,
    } as React.CSSProperties,
    formTitle: {
        fontSize: '15px',
        fontWeight: 600,
        color: colors.text,
        margin: '0 0 20px',
        paddingBottom: '12px',
        borderBottom: `1px solid ${colors.border}`,
    } as React.CSSProperties,
    formSection: {
        marginBottom: '20px',
    } as React.CSSProperties,
    formSectionTitle: {
        fontSize: '12px',
        fontWeight: 600,
        textTransform: 'uppercase',
        letterSpacing: '0.5px',
        color: colors.textSubtle,
        marginBottom: '12px',
    } as React.CSSProperties,
    formRow: {
        display: 'grid',
        gridTemplateColumns: '1fr 1fr',
        gap: '16px',
    } as React.CSSProperties,
    inputGroup: {
        marginBottom: '16px',
    } as React.CSSProperties,
    label: {
        display: 'block',
        marginBottom: '6px',
        fontWeight: 600,
        fontSize: '13px',
        color: colors.text,
    } as React.CSSProperties,
    input: {
        width: '100%',
        padding: '8px 12px',
        border: `1px solid ${colors.border}`,
        borderRadius: '4px',
        fontSize: '14px',
        lineHeight: '20px',
        boxSizing: 'border-box',
        color: colors.text,
        background: colors.bg,
        outline: 'none',
    } as React.CSSProperties,
    inputDisabled: {
        width: '100%',
        padding: '8px 12px',
        border: `1px solid ${colors.border}`,
        borderRadius: '4px',
        fontSize: '14px',
        lineHeight: '20px',
        boxSizing: 'border-box',
        color: colors.textMuted,
        background: colors.bgAlt,
        outline: 'none',
    } as React.CSSProperties,
    select: {
        padding: '8px 12px',
        border: `1px solid ${colors.border}`,
        borderRadius: '4px',
        fontSize: '14px',
        lineHeight: '20px',
        color: colors.text,
        background: colors.bg,
        outline: 'none',
    } as React.CSSProperties,
    helpText: {
        fontSize: '12px',
        color: colors.textSubtle,
        marginTop: '4px',
        lineHeight: '16px',
    } as React.CSSProperties,
    checkbox: {
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        fontSize: '14px',
        fontWeight: 500,
        color: colors.text,
        cursor: 'pointer',
    } as React.CSSProperties,
    badge: {
        display: 'inline-flex',
        alignItems: 'center',
        padding: '2px 8px',
        borderRadius: '10px',
        fontSize: '11px',
        fontWeight: 600,
        lineHeight: '16px',
        textTransform: 'uppercase',
        letterSpacing: '0.3px',
    } as React.CSSProperties,
    badgeAuth: {
        background: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.08)',
        color: colors.textMuted,
    } as React.CSSProperties,
    badgeTls: {
        background: 'rgba(56, 111, 229, 0.12)',
        color: colors.primary,
    } as React.CSSProperties,
    btnPrimary: {
        display: 'inline-flex',
        alignItems: 'center',
        gap: '6px',
        padding: '8px 16px',
        cursor: 'pointer',
        border: 'none',
        borderRadius: '4px',
        background: colors.primary,
        color: '#fff',
        fontSize: '13px',
        fontWeight: 600,
        lineHeight: '18px',
    } as React.CSSProperties,
    btnSecondary: {
        display: 'inline-flex',
        alignItems: 'center',
        gap: '6px',
        padding: '8px 16px',
        cursor: 'pointer',
        border: `1px solid ${colors.border}`,
        borderRadius: '4px',
        background: colors.bg,
        color: colors.text,
        fontSize: '13px',
        fontWeight: 600,
        lineHeight: '18px',
    } as React.CSSProperties,
    btnDanger: {
        display: 'inline-flex',
        alignItems: 'center',
        gap: '6px',
        padding: '8px 16px',
        cursor: 'pointer',
        border: `1px solid ${colors.danger}`,
        borderRadius: '4px',
        background: 'transparent',
        color: colors.danger,
        fontSize: '13px',
        fontWeight: 600,
        lineHeight: '18px',
    } as React.CSSProperties,
    btnSmall: {
        padding: '6px 12px',
        fontSize: '12px',
    } as React.CSSProperties,
    formActions: {
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        paddingTop: '16px',
        borderTop: `1px solid ${colors.border}`,
    } as React.CSSProperties,
    statusBanner: {
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        padding: '8px 12px',
        borderRadius: '4px',
        fontSize: '13px',
        fontWeight: 500,
        marginTop: '12px',
    } as React.CSSProperties,
    statusSuccess: {
        background: 'rgba(61, 184, 135, 0.12)',
        color: '#1B8A5C',
    } as React.CSSProperties,
    statusError: {
        background: 'rgba(210, 75, 78, 0.12)',
        color: colors.danger,
    } as React.CSSProperties,
    formError: {
        display: 'flex',
        alignItems: 'center',
        gap: '6px',
        padding: '8px 12px',
        borderRadius: '4px',
        background: 'rgba(210, 75, 78, 0.08)',
        color: colors.danger,
        fontSize: '13px',
        marginBottom: '16px',
    } as React.CSSProperties,
    emptyState: {
        textAlign: 'center',
        padding: '32px 20px',
        color: colors.textSubtle,
        fontSize: '14px',
        border: `1px dashed ${colors.border}`,
        borderRadius: '8px',
    } as React.CSSProperties,
    addArea: {
        marginBottom: '16px',
    } as React.CSSProperties,
    sectionHeader: {
        marginBottom: '16px',
    } as React.CSSProperties,
    sectionTitle: {
        display: 'flex',
        alignItems: 'center',
        gap: '10px',
        fontSize: '16px',
        fontWeight: 600,
        color: colors.text,
        margin: '0 0 4px',
    } as React.CSSProperties,
    sectionDesc: {
        fontSize: '13px',
        color: colors.textSubtle,
        margin: 0,
        lineHeight: '20px',
    } as React.CSSProperties,
    directionBadge: {
        display: 'inline-flex',
        alignItems: 'center',
        padding: '2px 10px',
        borderRadius: '10px',
        fontSize: '11px',
        fontWeight: 600,
        lineHeight: '16px',
        textTransform: 'uppercase',
        letterSpacing: '0.3px',
    } as React.CSSProperties,
    directionInbound: {
        background: 'rgba(61, 184, 135, 0.12)',
        color: '#1B8A5C',
    } as React.CSSProperties,
    directionOutbound: {
        background: 'rgba(56, 111, 229, 0.12)',
        color: colors.primary,
    } as React.CSSProperties,
};

function authLabel(authType: string): string {
    switch (authType) {
    case 'token':
        return 'Token';
    case 'credentials':
        return 'Credentials';
    default:
        return 'None';
    }
}

const NATSConnectionSettings: React.FC<CustomSettingProps> = ({
    id,
    value,
    onChange,
    setSaveNeeded,
    disabled,
}) => {
    const [connections, setConnections] = React.useState<NATSConnection[]>([]);
    const [editingIndex, setEditingIndex] = React.useState<number | null>(null);
    const [editForm, setEditForm] = React.useState<NATSConnection>({...emptyConnection});
    const [testStatus, setTestStatus] = React.useState<Record<number, TestStatus>>({});
    const [formError, setFormError] = React.useState<string | null>(null);

    React.useEffect(() => {
        try {
            const parsed = value ? JSON.parse(value) : [];
            setConnections(Array.isArray(parsed) ? parsed : []);
        } catch {
            setConnections([]);
        }
    }, [value]);

    const isInbound = id.toLowerCase().includes('inbound');

    const handleAdd = () => {
        setEditingIndex(-1);
        setEditForm({...emptyConnection});
        setFormError(null);
    };

    const handleEdit = (index: number) => {
        setEditingIndex(index);
        setEditForm({...connections[index]});
        setFormError(null);
    };

    const handleDelete = (index: number) => {
        const updated = connections.filter((_, i) => i !== index);
        const json = JSON.stringify(updated);
        onChange(id, json);
        setSaveNeeded();
        if (editingIndex === index) {
            setEditingIndex(null);
        } else if (editingIndex !== null && editingIndex > index) {
            setEditingIndex(editingIndex - 1);
        }
        setTestStatus((prev) => {
            const next: Record<number, TestStatus> = {};
            for (const [key, val] of Object.entries(prev)) {
                const k = Number(key);
                if (k < index) {
                    next[k] = val;
                } else if (k > index) {
                    next[k - 1] = val;
                }
            }
            return next;
        });
    };

    const handleSave = () => {
        const trimmedName = editForm.name.trim();
        if (!trimmedName || !editForm.address.trim()) {
            setFormError('Name and Address are required.');
            return;
        }

        if (editForm.auth_type === 'token' && !editForm.token.trim()) {
            setFormError('Token is required when auth type is Token.');
            return;
        }

        if (editForm.auth_type === 'credentials' && (!editForm.username.trim() || !editForm.password.trim())) {
            setFormError('Username and password are required when auth type is Credentials.');
            return;
        }

        const isDuplicate = connections.some(
            (conn, i) => i !== editingIndex && conn.name.trim() === trimmedName,
        );
        if (isDuplicate) {
            setFormError('A connection with this name already exists. Please use a unique name.');
            return;
        }

        setFormError(null);

        const trimmedSubject = editForm.subject.trim();
        if (!trimmedSubject) {
            setFormError('Subject is required.');
            return;
        }
        if (!trimmedSubject.startsWith(SUBJECT_PREFIX)) {
            setFormError(`Subject must start with "${SUBJECT_PREFIX}".`);
            return;
        }

        const cleanedForm = {...editForm, name: trimmedName, subject: trimmedSubject};
        let updated: NATSConnection[];
        if (editingIndex === -1) {
            updated = [...connections, cleanedForm];
        } else if (editingIndex === null) {
            return;
        } else {
            updated = connections.map((conn, i) => (i === editingIndex ? cleanedForm : conn));
        }

        const json = JSON.stringify(updated);
        onChange(id, json);
        setSaveNeeded();
        setEditingIndex(null);
    };

    const handleCancel = () => {
        setEditingIndex(null);
        setFormError(null);
    };

    const handleFormChange = (field: keyof NATSConnection, fieldValue: string | boolean) => {
        if (field === 'name' && typeof fieldValue === 'string') {
            const sanitized = fieldValue.toLowerCase().replace(/[^a-z0-9-]/g, '');
            setEditForm((prev) => {
                const autoSubject = SUBJECT_PREFIX + prev.name;
                const subjectIsAuto = prev.subject === '' || prev.subject === autoSubject || prev.subject === SUBJECT_PREFIX;
                return {
                    ...prev,
                    name: sanitized,
                    subject: subjectIsAuto ? SUBJECT_PREFIX + sanitized : prev.subject,
                };
            });
            return;
        }
        setEditForm((prev) => ({...prev, [field]: fieldValue}));
    };

    const handleTestConnection = async (index: number) => {
        setTestStatus((prev) => ({...prev, [index]: {loading: true}}));

        const direction = isInbound ? 'inbound' : 'outbound';

        try {
            const response = await fetch(`/plugins/${manifest.id}/api/v1/test-connection?direction=${direction}`, {
                method: 'POST',
                credentials: 'same-origin',
                headers: {
                    'Content-Type': 'application/json',
                    'X-CSRF-Token': getCSRFToken(),
                    'X-Requested-With': 'XMLHttpRequest',
                },
                body: JSON.stringify(connections[index]),
            });

            if (response.ok) {
                const data = await response.json();
                setTestStatus((prev) => ({
                    ...prev,
                    [index]: {
                        loading: false,
                        success: true,
                        message: data.message || 'Connection successful',
                    },
                }));
                setTimeout(() => {
                    setTestStatus((prev) => {
                        const next = {...prev};
                        delete next[index];
                        return next;
                    });
                }, TEST_STATUS_DISPLAY_MS);
            } else {
                let errorMessage = 'Connection failed';
                try {
                    const errorData = await response.json();
                    errorMessage = errorData.error || errorMessage;
                } catch {
                    // Use default error message
                }
                setTestStatus((prev) => ({
                    ...prev,
                    [index]: {loading: false, success: false, message: errorMessage},
                }));
                setTimeout(() => {
                    setTestStatus((prev) => {
                        const next = {...prev};
                        delete next[index];
                        return next;
                    });
                }, TEST_STATUS_DISPLAY_MS);
            }
        } catch (err) {
            const message = err instanceof Error ? err.message : 'Network error';
            setTestStatus((prev) => ({
                ...prev,
                [index]: {loading: false, success: false, message},
            }));
            setTimeout(() => {
                setTestStatus((prev) => {
                    const next = {...prev};
                    delete next[index];
                    return next;
                });
            }, TEST_STATUS_DISPLAY_MS);
        }
    };

    const renderForm = () => {
        const isEditing = editingIndex !== null && editingIndex >= 0;

        return (
            <div style={styles.form}>
                <div style={styles.formTitle}>
                    {isEditing ? 'Edit Connection' : 'New Connection'}
                </div>

                {formError && (
                    <div style={styles.formError}>
                        {formError}
                    </div>
                )}

                <div style={styles.formSection}>
                    <div style={styles.formSectionTitle as React.CSSProperties}>{'Connection'}</div>
                    <div style={styles.formRow}>
                        <div style={styles.inputGroup}>
                            <label style={styles.label}>{'Name'}</label>
                            <input
                                style={styles.input}
                                type='text'
                                value={editForm.name}
                                onChange={(e) => handleFormChange('name', e.target.value)}
                                disabled={disabled}
                                placeholder='my-nats-connection'
                            />
                            <div style={styles.helpText}>
                                {'Lowercase letters, numbers, and hyphens only.'}
                            </div>
                        </div>
                        <div style={styles.inputGroup}>
                            <label style={styles.label}>{'Address'}</label>
                            <input
                                style={styles.input}
                                type='text'
                                value={editForm.address}
                                onChange={(e) => handleFormChange('address', e.target.value)}
                                disabled={disabled}
                                placeholder={DEFAULT_NATS_ADDRESS}
                            />
                        </div>
                    </div>
                    <div style={styles.inputGroup}>
                        <label style={styles.label}>{'Subject'}</label>
                        <input
                            style={styles.input}
                            type='text'
                            value={editForm.subject || SUBJECT_PREFIX}
                            onChange={(e) => handleFormChange('subject', e.target.value)}
                            disabled={disabled}
                            placeholder={SUBJECT_PREFIX + 'my-connection'}
                        />
                        <div style={styles.helpText}>
                            {`Defaults from connection name. Must start with "${SUBJECT_PREFIX}".`}
                        </div>
                    </div>
                </div>

                <div style={styles.formSection}>
                    <div style={styles.formSectionTitle as React.CSSProperties}>{'Authentication'}</div>
                    <div style={styles.inputGroup}>
                        <label style={styles.label}>{'Auth Type'}</label>
                        <select
                            style={styles.select}
                            value={editForm.auth_type}
                            onChange={(e) => handleFormChange('auth_type', e.target.value)}
                            disabled={disabled}
                        >
                            <option value='none'>{'None'}</option>
                            <option value='token'>{'Token'}</option>
                            <option value='credentials'>{'Username / Password'}</option>
                        </select>
                    </div>
                    {editForm.auth_type === 'token' && (
                        <div style={styles.inputGroup}>
                            <label style={styles.label}>{'Token'}</label>
                            <input
                                style={styles.input}
                                type='password'
                                value={editForm.token}
                                onChange={(e) => handleFormChange('token', e.target.value)}
                                disabled={disabled}
                                placeholder='Enter token'
                            />
                        </div>
                    )}
                    {editForm.auth_type === 'credentials' && (
                        <div style={styles.formRow}>
                            <div style={styles.inputGroup}>
                                <label style={styles.label}>{'Username'}</label>
                                <input
                                    style={styles.input}
                                    type='text'
                                    value={editForm.username}
                                    onChange={(e) => handleFormChange('username', e.target.value)}
                                    disabled={disabled}
                                    placeholder='Enter username'
                                />
                            </div>
                            <div style={styles.inputGroup}>
                                <label style={styles.label}>{'Password'}</label>
                                <input
                                    style={styles.input}
                                    type='password'
                                    value={editForm.password}
                                    onChange={(e) => handleFormChange('password', e.target.value)}
                                    disabled={disabled}
                                    placeholder='Enter password'
                                />
                            </div>
                        </div>
                    )}
                </div>

                <div style={styles.formSection}>
                    <div style={styles.formSectionTitle as React.CSSProperties}>{'Security'}</div>
                    <div style={styles.inputGroup}>
                        <label style={styles.checkbox}>
                            <input
                                type='checkbox'
                                checked={editForm.tls_enabled}
                                onChange={(e) => handleFormChange('tls_enabled', e.target.checked)}
                                disabled={disabled}
                            />
                            {'Enable TLS'}
                        </label>
                        <div style={styles.helpText}>
                            {'Encrypt the connection to the NATS server using TLS.'}
                        </div>
                    </div>
                    {editForm.tls_enabled && (
                        <>
                            <div style={styles.formRow}>
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'Client Cert Path'}</label>
                                    <input
                                        style={styles.input}
                                        type='text'
                                        value={editForm.client_cert}
                                        onChange={(e) => handleFormChange('client_cert', e.target.value)}
                                        disabled={disabled}
                                        placeholder='/path/to/client.crt'
                                    />
                                </div>
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'Client Key Path'}</label>
                                    <input
                                        style={styles.input}
                                        type='text'
                                        value={editForm.client_key}
                                        onChange={(e) => handleFormChange('client_key', e.target.value)}
                                        disabled={disabled}
                                        placeholder='/path/to/client.key'
                                    />
                                </div>
                            </div>
                            <div style={styles.inputGroup}>
                                <label style={styles.label}>{'CA Cert Path'}</label>
                                <input
                                    style={styles.input}
                                    type='text'
                                    value={editForm.ca_cert}
                                    onChange={(e) => handleFormChange('ca_cert', e.target.value)}
                                    disabled={disabled}
                                    placeholder='/path/to/ca.crt'
                                />
                            </div>
                        </>
                    )}
                </div>

                <div style={styles.formActions}>
                    <button
                        style={styles.btnPrimary}
                        onClick={handleSave}
                        disabled={disabled}
                    >
                        {isEditing ? 'Update Connection' : 'Add Connection'}
                    </button>
                    <button
                        style={styles.btnSecondary}
                        onClick={handleCancel}
                    >
                        {'Cancel'}
                    </button>
                </div>
            </div>
        );
    };

    const renderCard = (conn: NATSConnection, index: number) => {
        if (editingIndex === index) {
            return (
                <div key={conn.name || `editing-${index}`}>
                    {renderForm()}
                </div>
            );
        }

        const status = testStatus[index];

        return (
            <div
                key={conn.name || `card-${index}`}
                style={styles.card}
            >
                <div style={styles.cardHeader}>
                    <div style={styles.cardTitle}>
                        {conn.name}
                        <span style={{...styles.badge, ...styles.badgeAuth}}>
                            {authLabel(conn.auth_type)}
                        </span>
                        {conn.tls_enabled && (
                            <span style={{...styles.badge, ...styles.badgeTls}}>
                                {'TLS'}
                            </span>
                        )}
                    </div>
                </div>
                <div style={styles.cardMeta}>
                    <div style={styles.cardMetaItem}>
                        <span style={styles.cardMetaLabel}>{'Address'}</span>
                        {conn.address}
                    </div>
                    <div style={styles.cardMetaItem}>
                        <span style={styles.cardMetaLabel}>{'Subject'}</span>
                        {conn.subject}
                    </div>
                </div>
                <div style={styles.cardActions}>
                    <button
                        style={{...styles.btnSecondary, ...styles.btnSmall}}
                        onClick={() => handleEdit(index)}
                        disabled={disabled || editingIndex !== null}
                    >
                        {'Edit'}
                    </button>
                    <button
                        style={{...styles.btnSecondary, ...styles.btnSmall}}
                        onClick={() => handleTestConnection(index)}
                        disabled={disabled || (status?.loading ?? false) || editingIndex !== null}
                    >
                        {status?.loading ? 'Testing...' : 'Test Connection'}
                    </button>
                    <div style={{flex: 1}}/>
                    <button
                        style={{...styles.btnDanger, ...styles.btnSmall}}
                        onClick={() => handleDelete(index)}
                        disabled={disabled || editingIndex !== null}
                    >
                        {'Remove'}
                    </button>
                </div>
                {status && !status.loading && status.success !== undefined && (
                    <div
                        style={{
                        ...styles.statusBanner,
                        ...(status.success ? styles.statusSuccess : styles.statusError),
                    }}
                    >
                        {status.message}
                    </div>
                )}
            </div>
        );
    };

    const sectionTitle = isInbound ? 'Inbound' : 'Outbound';
    const sectionDesc = isInbound ?
        'Messages received from NATS and relayed into Mattermost.' :
        'Messages sent from Mattermost to NATS.';
    const directionStyle = isInbound ? styles.directionInbound : styles.directionOutbound;

    return (
        <div style={styles.container}>
            <div style={styles.sectionHeader}>
                <div style={styles.sectionTitle as React.CSSProperties}>
                    {sectionTitle}
                    <span style={{...styles.directionBadge, ...directionStyle} as React.CSSProperties}>
                        {isInbound ? 'NATS \u2192 Mattermost' : 'Mattermost \u2192 NATS'}
                    </span>
                </div>
                <p style={styles.sectionDesc}>{sectionDesc}</p>
            </div>
            <div style={styles.addArea}>
                <button
                    style={styles.btnPrimary}
                    onClick={handleAdd}
                    disabled={disabled || editingIndex !== null}
                >
                    {'+ Add Connection'}
                </button>
            </div>
            {editingIndex === -1 && renderForm()}
            {connections.length === 0 && editingIndex === null && (
                <div style={styles.emptyState as React.CSSProperties}>
                    <div style={{fontSize: '15px', fontWeight: 600, marginBottom: '4px', color: colors.textMuted}}>
                        {'No connections configured'}
                    </div>
                    <div>
                        {'Click '}
                        <strong>{'+ Add Connection'}</strong>
                        {' to get started.'}
                    </div>
                </div>
            )}
            {connections.map((conn, index) => renderCard(conn, index))}
        </div>
    );
};

export default NATSConnectionSettings;
