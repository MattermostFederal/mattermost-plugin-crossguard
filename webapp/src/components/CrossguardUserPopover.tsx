import React from 'react';

interface UserProps {
    user: {
        props?: Record<string, string>;
        last_name?: string;
    };
}

const CrossguardUserPopover: React.FC<UserProps> = ({user}) => {
    const remoteUsername = user?.props?.CrossguardRemoteUsername;
    if (!remoteUsername) {
        return null;
    }

    const match = user?.last_name?.match(/^\(via\s+(.+)\)$/);
    const connName = match?.[1] || 'unknown';

    return (
        <div style={{padding: '4px 0'}}>
            <span style={{fontSize: '12px', color: 'rgba(var(--center-channel-color-rgb), 0.64)'}}>
                {`Relayed from: ${remoteUsername} (via ${connName})`}
            </span>
        </div>
    );
};

export default CrossguardUserPopover;
