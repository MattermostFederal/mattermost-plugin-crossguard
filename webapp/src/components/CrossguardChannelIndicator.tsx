import React, {useEffect, useState} from 'react';

import {getChannelConnections, subscribe} from '../connection_state';

interface ChannelProps {
    channel: {
        id: string;
    };
}

const CrossguardChannelIndicator: React.FC<ChannelProps> = ({channel}) => {
    const [connections, setConnections] = useState(() => getChannelConnections(channel?.id));

    useEffect(() => {
        const update = () => {
            setConnections(getChannelConnections(channel?.id));
        };
        return subscribe(update);
    }, [channel?.id]);

    if (!connections) {
        return null;
    }

    return (
        <span
            className='icon icon-circle-multiple-outline'
            data-testid='SharedChannelIcon'
            title={connections}
            style={{fontSize: '14px', marginLeft: '4px', opacity: 0.64}}
        />
    );
};

export default CrossguardChannelIndicator;
