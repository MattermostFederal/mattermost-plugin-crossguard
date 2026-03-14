import manifest from 'manifest';

import type {PluginRegistry} from 'types/mattermost-webapp';

import AdminPanel from './components/AdminPanel';

export default class Plugin {
    public async initialize(registry: PluginRegistry) {
        registry.registerAdminConsoleCustomSection(
            'crossguard_info',
            AdminPanel,
        );
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void;
    }
}

window.registerPlugin(manifest.id, new Plugin());
