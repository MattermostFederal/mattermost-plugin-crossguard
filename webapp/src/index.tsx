import manifest from 'manifest';

import type {PluginRegistry} from 'types/mattermost-webapp';

import CrossguardModal from './components/CrossguardModal';
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
        registry.registerRootComponent(CrossguardModal);
        registry.registerChannelHeaderMenuAction(
            'Crossguard',
            (channelID: string) => {
                document.dispatchEvent(
                    new CustomEvent('crossguard:open-modal', {detail: {channelID}}),
                );
            },
        );
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void;
    }
}

window.registerPlugin(manifest.id, new Plugin());
