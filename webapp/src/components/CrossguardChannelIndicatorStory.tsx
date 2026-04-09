import React from 'react';

import CrossguardChannelIndicator from './CrossguardChannelIndicator';

import {setChannelConnections} from '../connection_state';

// Test story that sets connection state in the same module graph as the component.
const CrossguardChannelIndicatorStory: React.FC<{channelId: string; connections: string}> = ({channelId, connections}) => {
    React.useEffect(() => {
        setChannelConnections(channelId, connections);
    }, [channelId, connections]);

    return <CrossguardChannelIndicator channel={{id: channelId}}/>;
};

export default CrossguardChannelIndicatorStory;
