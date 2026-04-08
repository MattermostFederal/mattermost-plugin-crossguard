import manifest from 'manifest';

import type {PluginRegistry, UniqueIdentifier} from 'types/mattermost-webapp';

import ConnectionSettings from './components/ConnectionSettings';
import CrossguardChannelIndicator from './components/CrossguardChannelIndicator';
import CrossguardChannelModal from './components/CrossguardChannelModal';
import CrossguardTeamModal from './components/CrossguardTeamModal';
import CrossguardUserPopover from './components/CrossguardUserPopover';
import {fetchChannelConnections, setChannelConnections} from './connection_state';

interface ReduxStore {
    getState(): {
        entities: {
            teams: {
                currentTeamId: string;
            };
            channels: {
                currentChannelId: string;
                channels: Record<string, {
                    props?: Record<string, string>;
                }>;
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
            ConnectionSettings,
        );
        registry.registerAdminConsoleCustomSetting(
            'OutboundConnections',
            ConnectionSettings,
        );
        registry.registerRootComponent(CrossguardChannelModal);
        registry.registerRootComponent(CrossguardTeamModal);

        registry.registerSidebarChannelLinkLabelComponent(CrossguardChannelIndicator);
        registry.registerPopoverUserAttributesComponent(CrossguardUserPopover);

        registry.registerWebSocketEventHandler(
            `custom_${manifest.id}_channel_connections_updated`,
            (event: {data: {channel_id: string; connections: string}}) => {
                setChannelConnections(event.data.channel_id, event.data.connections);
            },
        );

        let lastTeamId = '';
        let lastChannelId = '';
        let lastChannelsRef: Record<string, unknown> = {};
        let knownChannelIds = new Set<string>();
        const checkState = () => {
            const state = store.getState();
            const teamId = state?.entities?.teams?.currentTeamId || '';
            const channelId = state?.entities?.channels?.currentChannelId || '';
            const channels = state?.entities?.channels?.channels || {};

            if (teamId && teamId !== lastTeamId) {
                lastTeamId = teamId;
                knownChannelIds = new Set<string>();
                this.updateMenuForTeam(teamId);
            }

            if (channels !== lastChannelsRef) {
                lastChannelsRef = channels;
                const currentIds = Object.keys(channels);
                const newIds = currentIds.filter((id) => !knownChannelIds.has(id));
                if (newIds.length > 0) {
                    knownChannelIds = new Set(currentIds);
                    fetchChannelConnections(newIds);
                }
            } else if (channelId && channelId !== lastChannelId) {
                lastChannelId = channelId;
                fetchChannelConnections([channelId]);
            }
        };

        this.unsubscribe = store.subscribe(checkState);
        checkState();
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
        } catch (e) {
            // eslint-disable-next-line no-console
            console.warn('updateMenuForTeam failed', e);
            this.removeMenuAction();
            this.removeMainMenuAction();
        }
    }

    private addMenuAction() {
        if (this.menuActionId || !this.registry) {
            return;
        }
        this.menuActionId = this.registry.registerChannelHeaderMenuAction(
            'Cross Guard',
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
            'Cross Guard',
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
