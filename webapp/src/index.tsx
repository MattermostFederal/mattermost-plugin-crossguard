import manifest from 'manifest';

import type {PluginRegistry} from 'types/mattermost-webapp';

import NATSConnectionSettings from './components/NATSConnectionSettings';

export default class Plugin {
    public async initialize(registry: PluginRegistry) {
        registry.registerAdminConsoleCustomSetting(
            'InboundConnections',
            NATSConnectionSettings,
        );
        registry.registerAdminConsoleCustomSetting(
            'OutboundConnections',
            NATSConnectionSettings,
        );
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void;
    }
}

window.registerPlugin(manifest.id, new Plugin());
