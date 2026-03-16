import manifest from 'manifest';

import type {PluginRegistry, UniqueIdentifier} from 'types/mattermost-webapp';

import CrossguardChannelModal from './components/CrossguardChannelModal';
import CrossguardTeamModal from './components/CrossguardTeamModal';
import NATSConnectionSettings from './components/NATSConnectionSettings';

interface ReduxStore {
    getState(): {
        entities: {
            teams: {
                currentTeamId: string;
            };
        };
    };
    subscribe(listener: () => void): () => void;
}

export default class Plugin {
    private menuActionId: UniqueIdentifier | null = null;
    private mainMenuActionId: UniqueIdentifier | null = null;
    private registry: PluginRegistry | null = null;
    private unsubscribe: (() => void) | null = null;

    public async initialize(registry: PluginRegistry, store: ReduxStore) {
        this.registry = registry;

        registry.registerAdminConsoleCustomSetting(
            'InboundConnections',
            NATSConnectionSettings,
        );
        registry.registerAdminConsoleCustomSetting(
            'OutboundConnections',
            NATSConnectionSettings,
        );
        registry.registerRootComponent(CrossguardChannelModal);
        registry.registerRootComponent(CrossguardTeamModal);

        let lastTeamId = '';
        const checkTeam = () => {
            const state = store.getState();
            const teamId = state?.entities?.teams?.currentTeamId || '';
            if (teamId && teamId !== lastTeamId) {
                lastTeamId = teamId;
                this.updateMenuForTeam(teamId);
            }
        };

        this.unsubscribe = store.subscribe(checkTeam);
        checkTeam();
    }

    public uninitialize() {
        if (this.unsubscribe) {
            this.unsubscribe();
            this.unsubscribe = null;
        }
    }

    private async updateMenuForTeam(teamId: string) {
        try {
            const resp = await fetch(
                `/plugins/${manifest.id}/api/v1/teams/${teamId}/status`,
                {
                    credentials: 'same-origin',
                    headers: {'X-Requested-With': 'XMLHttpRequest'},
                },
            );
            if (!resp.ok) {
                this.removeMenuAction();
                this.removeMainMenuAction();
                return;
            }
            this.addMenuAction();
            this.addMainMenuAction(teamId);
        } catch {
            this.removeMenuAction();
            this.removeMainMenuAction();
        }
    }

    private addMenuAction() {
        if (this.menuActionId || !this.registry) {
            return;
        }
        this.menuActionId = this.registry.registerChannelHeaderMenuAction(
            'Crossguard',
            (channelID: string) => {
                document.dispatchEvent(
                    new CustomEvent('crossguard:open-modal', {detail: {channelID}}),
                );
            },
        );
    }

    private removeMenuAction() {
        if (!this.menuActionId || !this.registry) {
            return;
        }
        this.registry.unregisterComponent(this.menuActionId);
        this.menuActionId = null;
    }

    private addMainMenuAction(teamId: string) {
        this.removeMainMenuAction();
        if (!this.registry) {
            return;
        }
        this.mainMenuActionId = this.registry.registerMainMenuAction(
            'Crossguard',
            () => {
                document.dispatchEvent(
                    new CustomEvent('crossguard:open-team-modal', {detail: {teamID: teamId}}),
                );
            },
            null,
        );
    }

    private removeMainMenuAction() {
        if (!this.mainMenuActionId || !this.registry) {
            return;
        }
        this.registry.unregisterComponent(this.mainMenuActionId);
        this.mainMenuActionId = null;
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void;
    }
}

window.registerPlugin(manifest.id, new Plugin());
