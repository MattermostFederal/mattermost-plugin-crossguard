import React from 'react';

import ConnectionSettings from './ConnectionSettings';

// Wrapper component to capture onChange and setSaveNeeded callback invocations.
// Must be in a separate file for Playwright CT to mount it.
const ConnectionSettingsStory: React.FC<{id: string; value: string; disabled?: boolean}> = (props) => {
    const [calls, setCalls] = React.useState<{onChange: Array<{id: string; value: string}>; saveNeeded: number}>({onChange: [], saveNeeded: 0});

    React.useEffect(() => {
        (window as any).__testCalls = calls; // eslint-disable-line no-underscore-dangle
    }, [calls]);

    return (
        <ConnectionSettings
            id={props.id}
            value={props.value}
            onChange={(changeId, value) => setCalls((prev) => ({...prev, onChange: [...prev.onChange, {id: changeId, value}]}))}
            setSaveNeeded={() => setCalls((prev) => ({...prev, saveNeeded: prev.saveNeeded + 1}))}
            disabled={props.disabled || false}
            config={{}}
            currentState={{}}
            license={{}}
            setByEnv={false}
        />
    );
};

export default ConnectionSettingsStory;
