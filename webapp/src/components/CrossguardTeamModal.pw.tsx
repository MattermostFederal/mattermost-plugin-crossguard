
import {test, expect} from '@playwright/experimental-ct-react';
import type {Page} from '@playwright/test';
import React from 'react';

import CrossguardTeamModal from './CrossguardTeamModal';

const mockTeamStatus = {
    team_id: 'team-456',
    team_name: 'alpha-team',
    team_display_name: 'Alpha Team',
    initialized: true,
    connections: [
        {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: 'beta-team', file_transfer_enabled: true, file_filter_mode: ''},
        {name: 'outbound-relay', direction: 'outbound', linked: false, remote_team_name: '', file_transfer_enabled: false, file_filter_mode: ''},
    ],
};

async function openTeamModal(page: Page, teamID: string) {
    await page.evaluate((id: string) => {
        document.dispatchEvent(new CustomEvent('crossguard:open-team-modal', {detail: {teamID: id}}));
    }, teamID);
}

async function setCsrfCookie(page: Page) {
    await page.evaluate(() => {
        Object.defineProperty(document, 'cookie', {
            get: () => 'MMCSRF=test-csrf-token',
            configurable: true,
        });
    });
}

async function routeStatusOk(page: Page, response = mockTeamStatus) {
    await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
        route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify(response),
        });
    });
}

async function mountAndOpen(page: Page, mount: any, teamID = 'team-456', response = mockTeamStatus) {
    await routeStatusOk(page, response);
    const component = await mount(<CrossguardTeamModal/>);
    await openTeamModal(page, teamID);
    await page.getByText('Alpha Team').waitFor({state: 'visible', timeout: 5000}).catch(() => {});
    return component;
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Modal lifecycle
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Modal lifecycle', () => {
    test('renders nothing before the custom event is dispatched', async ({mount}) => {
        const component = await mount(<CrossguardTeamModal/>);
        await expect(component).toBeEmpty();
    });

    test('opens when crossguard:open-team-modal event fires', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardTeamModal/>);
        await expect(component).toBeEmpty();
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('Cross Guard Settings for')).toBeVisible();
    });

    test('shows loading state while fetching', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', async (route) => {
            await new Promise((r) => setTimeout(r, 2000));
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify(mockTeamStatus),
            });
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('Loading...')).toBeVisible();
    });

    test('displays team name in the header after fetch completes', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await expect(page.getByText('Cross Guard Settings for Alpha Team')).toBeVisible();
    });

    test('shows "..." placeholder before team name is loaded', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', async (route) => {
            await new Promise((r) => setTimeout(r, 3000));
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify(mockTeamStatus),
            });
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('Cross Guard Settings for ...')).toBeVisible();
    });

    test('clears previous state when reopened with a new team', async ({mount, page}) => {
        let callCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            callCount++;
            const body = callCount === 1 ? mockTeamStatus : {
                ...mockTeamStatus,
                team_display_name: 'Bravo Team',
                connections: [],
            };
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify(body),
            });
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('Alpha Team')).toBeVisible();

        // Close via close button
        await page.locator('button').filter({hasText: '\u00D7'}).click();
        await expect(page.getByText('Alpha Team')).not.toBeVisible();

        // Open with new team
        await openTeamModal(page, 'team-789');
        await expect(page.getByText('Bravo Team')).toBeVisible();
        await expect(page.getByText('No connections available')).toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 2. Closing
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Closing', () => {
    test('closes when close button is clicked', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.locator('button').filter({hasText: '\u00D7'}).click();
        await expect(page.getByText('Cross Guard Settings for')).not.toBeVisible();
    });

    test('closes when Escape key is pressed', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.keyboard.press('Escape');
        await expect(page.getByText('Cross Guard Settings for')).not.toBeVisible();
    });

    test('closes when backdrop is clicked', async ({mount, page}) => {
        await mountAndOpen(page, mount);

        // Click position 0,0 which is on the backdrop, not the modal
        await page.mouse.click(1, 1);
        await expect(page.getByText('Cross Guard Settings for')).not.toBeVisible();
    });

    test('stays open when clicking inside the modal', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.getByText('Cross Guard Settings for Alpha Team').click();
        await expect(page.getByText('Cross Guard Settings for Alpha Team')).toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 3. Connection cards
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Connection cards', () => {
    test('renders a card for each connection', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await expect(page.getByText('inbound-relay')).toBeVisible();
        await expect(page.getByText('outbound-relay')).toBeVisible();
    });

    test('shows inbound badge with correct arrow text', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await expect(page.getByText('NATS \u2192 MATTERMOST').first()).toBeVisible();
    });

    test('shows outbound badge with correct arrow text', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await expect(page.getByText('MATTERMOST \u2192 NATS')).toBeVisible();
    });

    test('displays orphaned indicator when connection is orphaned', async ({mount, page}) => {
        const orphanedStatus = {
            ...mockTeamStatus,
            connections: [
                {name: 'old-conn', direction: 'inbound', linked: true, orphaned: true, remote_team_name: '', file_transfer_enabled: false, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', orphanedStatus);
        await expect(page.getByTitle('Connection no longer in configuration')).toBeVisible();
    });

    test('shows file transfer info based on connection settings', async ({mount, page}) => {
        const fileStatus = {
            ...mockTeamStatus,
            connections: [
                {name: 'allow-conn', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: 'allow', file_filter_types: '.pdf,.docx'},
                {name: 'deny-conn', direction: 'outbound', linked: false, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: 'deny', file_filter_types: '.exe'},
                {name: 'all-conn', direction: 'inbound', linked: false, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: ''},
                {name: 'disabled-conn', direction: 'outbound', linked: false, remote_team_name: '', file_transfer_enabled: false, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', fileStatus);
        await expect(page.getByText('Allow .pdf,.docx')).toBeVisible();
        await expect(page.getByText('Deny .exe')).toBeVisible();
        await expect(page.getByText('All types')).toBeVisible();
        await expect(page.getByText('Files: Disabled')).toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 4. Link/unlink actions
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Link/unlink actions', () => {
    test('shows Unlink button for linked connections and Link for unlinked', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await expect(page.getByRole('button', {name: 'Unlink'})).toBeVisible();
        await expect(page.getByRole('button', {name: 'Link', exact: true})).toBeVisible();
    });

    test('sends POST to teardown URL when unlinking', async ({mount, page}) => {
        await setCsrfCookie(page);
        let capturedUrl = '';
        await page.route('**/plugins/crossguard/api/v1/teams/*/teardown*', (route) => {
            capturedUrl = route.request().url();
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Unlink'}).click();
        expect(capturedUrl).toContain('/teardown?connection_name=inbound%3Ainbound-relay');
    });

    test('sends POST to init URL when linking', async ({mount, page}) => {
        await setCsrfCookie(page);
        let capturedUrl = '';
        await page.route('**/plugins/crossguard/api/v1/teams/*/init*', (route) => {
            capturedUrl = route.request().url();
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Link', exact: true}).click();
        expect(capturedUrl).toContain('/init?connection_name=outbound%3Aoutbound-relay');
    });

    test('shows loading text during link action', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/init*', async (route) => {
            await new Promise((r) => setTimeout(r, 1500));
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(page.getByText('Linking...')).toBeVisible();
    });

    test('shows loading text during unlink action', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/teardown*', async (route) => {
            await new Promise((r) => setTimeout(r, 1500));
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Unlink'}).click();
        await expect(page.getByText('Unlinking...')).toBeVisible();
    });

    test('disables all action buttons while an action is in progress', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/teardown*', async (route) => {
            await new Promise((r) => setTimeout(r, 1500));
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Unlink'}).click();
        await expect(page.getByRole('button', {name: 'Unlinking...'})).toBeDisabled();
        await expect(page.getByRole('button', {name: 'Link', exact: true})).toBeDisabled();
    });

    test('shows success banner after successful link', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/init*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(page.getByText('Connection "outbound-relay" linked.')).toBeVisible();
    });

    test('shows error banner when API returns error', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/init*', (route) => {
            route.fulfill({status: 400, contentType: 'application/json', body: JSON.stringify({error: 'Connection limit exceeded'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(page.getByText('Connection limit exceeded')).toBeVisible();
    });

    test('shows network error banner on fetch failure', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/teardown*', (route) => {
            route.abort('connectionrefused');
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Unlink'}).click();
        await expect(page.getByText('Network error.')).toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 5. Rewrite display
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Rewrite display', () => {
    test('inbound connection with remote_team_name shows Edit and Clear buttons', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await expect(page.getByText('Remote team:')).toBeVisible();
        await expect(page.getByText('beta-team')).toBeVisible();
        await expect(page.getByRole('button', {name: 'Edit'})).toBeVisible();
        await expect(page.getByRole('button', {name: 'Clear'})).toBeVisible();
    });

    test('inbound connection without remote_team_name shows Set button', async ({mount, page}) => {
        const noRewriteStatus = {
            ...mockTeamStatus,
            connections: [
                {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', noRewriteStatus);
        await expect(page.getByText('No remote team rewrite')).toBeVisible();
        await expect(page.getByRole('button', {name: 'Set'})).toBeVisible();
    });

    test('outbound connection does not show rewrite controls', async ({mount, page}) => {
        const outboundOnly = {
            ...mockTeamStatus,
            connections: [
                {name: 'outbound-relay', direction: 'outbound', linked: false, remote_team_name: '', file_transfer_enabled: false, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', outboundOnly);
        await expect(page.getByText('No remote team rewrite')).not.toBeVisible();
        await expect(page.getByText('Remote team:')).not.toBeVisible();
        await expect(page.getByRole('button', {name: 'Set'})).not.toBeVisible();
        await expect(page.getByRole('button', {name: 'Edit'})).not.toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 6. Rewrite edit flow
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Rewrite edit flow', () => {
    test('Set button opens empty input field', async ({mount, page}) => {
        const noRewriteStatus = {
            ...mockTeamStatus,
            connections: [
                {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', noRewriteStatus);
        await page.getByRole('button', {name: 'Set'}).click();
        const input = page.getByPlaceholder('Remote team name');
        await expect(input).toBeVisible();
        await expect(input).toHaveValue('');
    });

    test('Edit button pre-fills the input with current remote_team_name', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        const input = page.getByPlaceholder('Remote team name');
        await expect(input).toHaveValue('beta-team');
    });

    test('input field receives focus automatically', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        const input = page.getByPlaceholder('Remote team name');
        await expect(input).toBeFocused();
    });

    test('Save button is disabled when input is empty', async ({mount, page}) => {
        const noRewriteStatus = {
            ...mockTeamStatus,
            connections: [
                {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', noRewriteStatus);
        await page.getByRole('button', {name: 'Set'}).click();
        await expect(page.getByRole('button', {name: 'Save'})).toBeDisabled();
    });

    test('Save button is enabled when input has text', async ({mount, page}) => {
        const noRewriteStatus = {
            ...mockTeamStatus,
            connections: [
                {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', noRewriteStatus);
        await page.getByRole('button', {name: 'Set'}).click();
        await page.getByPlaceholder('Remote team name').fill('gamma-team');
        await expect(page.getByRole('button', {name: 'Save'})).toBeEnabled();
    });

    test('Save sends POST with correct body', async ({mount, page}) => {
        await setCsrfCookie(page);
        let capturedBody = '';
        let capturedHeaders: Record<string, string> = {};
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                capturedBody = route.request().postData() || '';
                capturedHeaders = route.request().headers();
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('gamma-team');
        await page.getByRole('button', {name: 'Save'}).click();
        expect(JSON.parse(capturedBody)).toEqual({connection: 'inbound-relay', remote_team_name: 'gamma-team'});
        expect(capturedHeaders['x-csrf-token']).toBe('test-csrf-token');
    });

    test('Enter key triggers save when input is non-empty', async ({mount, page}) => {
        await setCsrfCookie(page);
        let saved = false;
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                saved = true;
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('delta-team');
        await page.getByPlaceholder('Remote team name').press('Enter');
        await expect(page.getByText('Remote team rewrite updated for "inbound-relay".')).toBeVisible();
        expect(saved).toBe(true);
    });

    test('Enter key does not save when input is empty', async ({mount, page}) => {
        await setCsrfCookie(page);
        let saved = false;
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                saved = true;
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        const noRewriteStatus = {
            ...mockTeamStatus,
            connections: [
                {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: '', file_transfer_enabled: true, file_filter_mode: ''},
            ],
        };
        await mountAndOpen(page, mount, 'team-456', noRewriteStatus);
        await page.getByRole('button', {name: 'Set'}).click();
        await page.getByPlaceholder('Remote team name').press('Enter');
        expect(saved).toBe(false);

        // Input should still be visible (editing not cancelled)
        await expect(page.getByPlaceholder('Remote team name')).toBeVisible();
    });

    test('Escape key cancels editing but does not close modal', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await expect(page.getByPlaceholder('Remote team name')).toBeVisible();
        await page.getByPlaceholder('Remote team name').press('Escape');

        // Input should be gone
        await expect(page.getByPlaceholder('Remote team name')).not.toBeVisible();

        // Modal should still be open (stopPropagation prevents the modal Escape handler)
        await expect(page.getByText('Cross Guard Settings for Alpha Team')).toBeVisible();
    });

    test('Cancel button exits editing mode', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await expect(page.getByPlaceholder('Remote team name')).toBeVisible();
        await page.getByRole('button', {name: 'Cancel'}).click();
        await expect(page.getByPlaceholder('Remote team name')).not.toBeVisible();

        // Rewrite display returns
        await expect(page.getByText('Remote team:')).toBeVisible();
    });

    test('shows success banner after saving rewrite', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('gamma-team');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(page.getByText('Remote team rewrite updated for "inbound-relay".')).toBeVisible();
    });

    test('shows error banner when rewrite save fails', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                route.fulfill({status: 400, contentType: 'application/json', body: JSON.stringify({error: 'Invalid team name'})});
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('bad-team');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(page.getByText('Invalid team name')).toBeVisible();
    });

    test('shows network error banner when save request fails completely', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                route.abort('connectionrefused');
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('gamma-team');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(page.getByText('Network error.')).toBeVisible();
    });

    test('editing state is cleared after successful save', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('gamma-team');
        await page.getByRole('button', {name: 'Save'}).click();

        // Input should be gone after save completes
        await expect(page.getByPlaceholder('Remote team name')).not.toBeVisible();
    });

    test('re-fetches team status after successful save', async ({mount, page}) => {
        await setCsrfCookie(page);
        let fetchCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            fetchCount++;
            const body = fetchCount > 1 ? {
                ...mockTeamStatus,
                connections: [
                    {name: 'inbound-relay', direction: 'inbound', linked: true, remote_team_name: 'gamma-team', file_transfer_enabled: true, file_filter_mode: ''},
                    mockTeamStatus.connections[1],
                ],
            } : mockTeamStatus;
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(body)});
        });
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route) => {
            if (route.request().method() === 'POST') {
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('beta-team')).toBeVisible();
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('gamma-team');
        await page.getByRole('button', {name: 'Save'}).click();

        // After re-fetch the new remote_team_name should appear
        await expect(page.getByText('gamma-team')).toBeVisible();
        expect(fetchCount).toBeGreaterThanOrEqual(2);
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 7. Rewrite clear flow
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Rewrite clear flow', () => {
    test('Clear sends DELETE with correct query param', async ({mount, page}) => {
        await setCsrfCookie(page);
        let capturedUrl = '';
        let capturedMethod = '';
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            capturedUrl = route.request().url();
            capturedMethod = route.request().method();
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Clear'}).click();
        expect(capturedMethod).toBe('DELETE');
        expect(capturedUrl).toContain('/rewrite?connection=inbound-relay');
    });

    test('Clear includes CSRF token header', async ({mount, page}) => {
        await setCsrfCookie(page);
        let capturedHeaders: Record<string, string> = {};
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            capturedHeaders = route.request().headers();
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Clear'}).click();
        expect(capturedHeaders['x-csrf-token']).toBe('test-csrf-token');
    });

    test('shows success banner after clearing rewrite', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('Remote team rewrite cleared for "inbound-relay".')).toBeVisible();
    });

    test('shows error banner when clear fails', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            route.fulfill({status: 500, contentType: 'application/json', body: JSON.stringify({error: 'Server error on clear'})});
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('Server error on clear')).toBeVisible();
    });

    test('shows network error banner when clear request fails completely', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            route.abort('connectionrefused');
        });
        await mountAndOpen(page, mount);
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('Network error.')).toBeVisible();
    });

    test('editing state is cleared after successful clear', async ({mount, page}) => {
        await setCsrfCookie(page);
        let fetchCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            fetchCount++;
            const body = fetchCount > 1 ? {
                ...mockTeamStatus,
                connections: [
                    {name: 'inbound-relay', direction: 'inbound', linked: true, file_transfer_enabled: true, file_filter_mode: ''},
                    mockTeamStatus.connections[1],
                ],
            } : mockTeamStatus;
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(body)});
        });
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('beta-team')).toBeVisible();
        await page.getByRole('button', {name: 'Clear'}).click();

        // After clear + re-fetch, should show "No remote team rewrite"
        await expect(page.getByText('No remote team rewrite')).toBeVisible();
    });

    test('re-fetches team status after successful clear', async ({mount, page}) => {
        await setCsrfCookie(page);
        let fetchCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            fetchCount++;
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(mockTeamStatus)});
        });
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('Alpha Team')).toBeVisible();
        const fetchBefore = fetchCount;
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('Remote team rewrite cleared')).toBeVisible();
        expect(fetchCount).toBeGreaterThan(fetchBefore);
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 8. Status banner
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Status banner', () => {
    test('no banner is visible on initial open', async ({mount, page}) => {
        await mountAndOpen(page, mount);

        // The status banner contains success/error messages. Neither should appear on load.
        await expect(page.getByText('Connection "')).not.toBeVisible();
        await expect(page.getByText('Network error.')).not.toBeVisible();
        await expect(page.getByText('Remote team rewrite')).not.toBeVisible();
    });

    test('auto-hides banner after 5 seconds', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/init*', (route) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mountAndOpen(page, mount);

        await page.clock.install();
        await page.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(page.getByText('Connection "outbound-relay" linked.')).toBeVisible();

        // Advance time past the auto-hide threshold
        await page.clock.fastForward(5100);
        await expect(page.getByText('Connection "outbound-relay" linked.')).not.toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 9. Empty state and errors
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Empty state and errors', () => {
    test('shows empty message when there are no connections', async ({mount, page}) => {
        const emptyStatus = {...mockTeamStatus, connections: []};
        await mountAndOpen(page, mount, 'team-456', emptyStatus);
        await expect(page.getByText('No connections available. Configure connections in the System Console.')).toBeVisible();
    });

    test('shows error when fetchStatus returns non-ok response', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            route.fulfill({
                status: 500,
                contentType: 'application/json',
                body: JSON.stringify({error: 'Internal server error'}),
            });
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('Internal server error')).toBeVisible();
    });

    test('shows network error when fetchStatus request fails', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            route.abort('connectionrefused');
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('Network error loading team status.')).toBeVisible();
    });

    test('null connections in response treated as empty array', async ({mount, page}) => {
        const nullConnStatus = {...mockTeamStatus, connections: null};
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route) => {
            route.fulfill({
                status: 200,
                contentType: 'application/json',
                body: JSON.stringify(nullConnStatus),
            });
        });
        await mount(<CrossguardTeamModal/>);
        await openTeamModal(page, 'team-456');
        await expect(page.getByText('No connections available. Configure connections in the System Console.')).toBeVisible();
    });
});

// ─────────────────────────────────────────────────────────────────────────────
// 10. Help link
// ─────────────────────────────────────────────────────────────────────────────
test.describe('Help link', () => {
    test('renders documentation link with correct URL', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        const link = page.getByRole('link', {name: 'View Cross Guard Documentation'});
        await expect(link).toHaveAttribute('href', '/plugins/crossguard/public/help/help.html');
    });

    test('documentation link opens in new tab with security attributes', async ({mount, page}) => {
        await mountAndOpen(page, mount);
        const link = page.getByRole('link', {name: 'View Cross Guard Documentation'});
        await expect(link).toHaveAttribute('target', '_blank');
        await expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    });
});
