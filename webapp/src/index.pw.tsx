import {test, expect} from '@playwright/experimental-ct-react';
import React from 'react';

import PluginTestHarness from './components/PluginTestHarness';

// These tests mount PluginTestHarness to load the Plugin class and connection_state
// into the browser's window object, then use page.evaluate() to exercise Plugin behavior.

test.describe('Plugin class', () => {
    test.describe('initialize', () => {
        test('registers admin console settings for InboundConnections and OutboundConnections', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const result = await page.evaluate(async () => {
                const calls: string[] = [];
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                const registry = {
                    registerAdminConsoleCustomSetting: (...args: any[]) => calls.push('adminSetting:' + args[0]),
                    registerRootComponent: () => calls.push('rootComponent'),
                    registerSidebarChannelLinkLabelComponent: () => calls.push('sidebar'),
                    registerPopoverUserAttributesComponent: () => calls.push('popover'),
                    registerWebSocketEventHandler: (...args: any[]) => calls.push('wsHandler:' + args[0]),
                    registerChannelHeaderMenuAction: () => {
                        calls.push('menuAction');
                        return 'menu-id-1';
                    },
                    registerMainMenuAction: () => {
                        calls.push('mainMenu');
                        return 'main-menu-id-1';
                    },
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                plugin.uninitialize();
                return calls;
            });

            expect(result).toContain('adminSetting:InboundConnections');
            expect(result).toContain('adminSetting:OutboundConnections');
        });

        test('registers root components for channel and team modals', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const rootComponentCount = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let count = 0;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {
                        count++;
                    },
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                plugin.uninitialize();
                return count;
            });

            expect(rootComponentCount).toBe(2);
        });

        test('registers sidebar and popover components', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const result = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let sidebarCalled = false;
                let popoverCalled = false;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {
                        sidebarCalled = true;
                    },
                    registerPopoverUserAttributesComponent: () => {
                        popoverCalled = true;
                    },
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                plugin.uninitialize();
                return {sidebarCalled, popoverCalled};
            });

            expect(result.sidebarCalled).toBe(true);
            expect(result.popoverCalled).toBe(true);
        });

        test('registers WebSocket handler for channel connections updated', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const eventName = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let capturedName = '';
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: (name: string) => {
                        capturedName = name;
                    },
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                plugin.uninitialize();
                return capturedName;
            });

            expect(eventName).toBe('custom_crossguard_channel_connections_updated');
        });

        test('subscribes to Redux store', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const subscribed = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let subscribeCalled = false;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => {
                        subscribeCalled = true;
                        return () => {};
                    },
                };
                await plugin.initialize(registry, store);
                plugin.uninitialize();
                return subscribeCalled;
            });

            expect(subscribed).toBe(true);
        });
    });

    test.describe('store subscription behavior', () => {
        test('fetches team status when team ID changes', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);

            const fetchedUrls: string[] = [];
            await page.route('**/plugins/crossguard/**', (route) => {
                fetchedUrls.push(route.request().url());
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: 'team-xyz'}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 100));
                plugin.uninitialize();
            });

            const teamStatusFetch = fetchedUrls.find((u) => u.includes('/teams/team-xyz/status'));
            expect(teamStatusFetch).toBeDefined();
        });

        test('registers channel menu on successful team status', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{"team_id":"t1"}'});
            });

            const menuRegistered = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let menuCalled = false;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => {
                        menuCalled = true;
                        return 'id';
                    },
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: 'team-ok'}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 100));
                plugin.uninitialize();
                return menuCalled;
            });

            expect(menuRegistered).toBe(true);
        });

        test('does not register menu on non-ok team status', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
                route.fulfill({status: 404, contentType: 'application/json', body: '{}'});
            });

            const menuRegistered = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let menuCalled = false;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => {
                        menuCalled = true;
                        return 'id';
                    },
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: 'team-bad'}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);
                await new Promise((r) => setTimeout(r, 100));
                plugin.uninitialize();
                return menuCalled;
            });

            expect(menuRegistered).toBe(false);
        });
    });

    test.describe('WebSocket handler', () => {
        test('updates connection state when WS event received', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const result = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let wsCallback: any = null;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: (_name: string, cb: any) => {
                        wsCallback = cb;
                    },
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => () => {},
                };
                await plugin.initialize(registry, store);

                if (wsCallback) {
                    wsCallback({data: {channel_id: 'ws-test-ch', connections: 'ConnA, ConnB'}});
                }

                const getConns = (window as any).__getChannelConnections; // eslint-disable-line no-underscore-dangle
                const setConns = (window as any).__setChannelConnections; // eslint-disable-line no-underscore-dangle
                const connResult = getConns('ws-test-ch');
                setConns('ws-test-ch', '');
                plugin.uninitialize();
                return connResult;
            });

            expect(result).toBe('ConnA, ConnB');
        });
    });

    test.describe('uninitialize', () => {
        test('calls unsubscribe from store', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const unsubCalled = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let unsubbed = false;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => {
                        return () => {
                            unsubbed = true;
                        };
                    },
                };
                await plugin.initialize(registry, store);
                plugin.uninitialize();
                return unsubbed;
            });

            expect(unsubCalled).toBe(true);
        });

        test('double uninitialize does not error', async ({mount, page}) => {
            await mount(<PluginTestHarness/>);
            await page.route('**/plugins/crossguard/**', (route) => {
                route.fulfill({status: 200, contentType: 'application/json', body: '{}'});
            });

            const result = await page.evaluate(async () => {
                const plugin = new (window as any).__PluginClass(); // eslint-disable-line no-underscore-dangle
                let unsubCount = 0;
                const registry = {
                    registerAdminConsoleCustomSetting: () => {},
                    registerRootComponent: () => {},
                    registerSidebarChannelLinkLabelComponent: () => {},
                    registerPopoverUserAttributesComponent: () => {},
                    registerWebSocketEventHandler: () => {},
                    registerChannelHeaderMenuAction: () => 'id',
                    registerMainMenuAction: () => 'id',
                    unregisterComponent: () => {},
                };
                const store = {
                    getState: () => ({entities: {teams: {currentTeamId: ''}, channels: {currentChannelId: '', channels: {}}}}),
                    subscribe: () => {
                        return () => {
                            unsubCount++;
                        };
                    },
                };
                await plugin.initialize(registry, store);
                plugin.uninitialize();
                plugin.uninitialize();
                return unsubCount;
            });

            // Should only be called once due to null guard
            expect(result).toBe(1);
        });
    });
});
