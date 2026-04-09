import {test, expect} from '@playwright/experimental-ct-react';
import React from 'react';

import CrossguardTeamModal from './CrossguardTeamModal';

function teamStatusResponse(overrides?: any) {
    return {team_id: 'team1', team_name: 'test-team', team_display_name: 'Test Team', initialized: true, connections: [], ...overrides};
}

function connStatus(overrides?: any) {
    return {name: 'my-conn', direction: 'inbound', linked: false, orphaned: false, file_transfer_enabled: false, ...overrides};
}

async function openModal(page: any, teamID = 'team1') {
    await page.evaluate((id: string) => {
        document.dispatchEvent(new CustomEvent('crossguard:open-team-modal', {detail: {teamID: id}}));
    }, teamID);
}

async function setCsrfCookie(page: any) {
    await page.evaluate(() => {
        Object.defineProperty(document, 'cookie', {
            get: () => 'MMCSRF=test-csrf-token',
            configurable: true,
        });
    });
}

async function routeStatusOk(page: any, body?: unknown) {
    const responseBody = body || teamStatusResponse({connections: [connStatus()]});
    await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
        route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(responseBody)});
    });
}

async function mountAndOpen(page: any, mount: any, teamID = 'team1', body?: unknown) {
    const responseBody = body || teamStatusResponse({connections: [connStatus()]});
    await routeStatusOk(page, responseBody);
    const component = await mount(<CrossguardTeamModal/>);
    await openModal(page, teamID);
    await page.getByText('Test Team').waitFor({state: 'visible', timeout: 5000}).catch(() => {});
    return component;
}

// ---------------------------------------------------------------------------
// 1. Modal lifecycle
// ---------------------------------------------------------------------------
test.describe('Modal lifecycle edge cases', () => {
    test('opening for a different team resets previous state', async ({mount, page}) => {
        let callCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            callCount++;
            const body = callCount === 1 ? teamStatusResponse({connections: [connStatus({name: 'first-conn'})], team_display_name: 'First Team'}) : teamStatusResponse({connections: [connStatus({name: 'second-conn'})], team_display_name: 'Second Team'});
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(body)});
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('first-conn')).toBeVisible();

        await page.locator('button').filter({hasText: '\u00D7'}).click();
        await openModal(page, 'team2');
        await expect(page.getByText('second-conn')).toBeVisible();
        await expect(page.getByText('first-conn')).not.toBeVisible();
    });

    test('re-opening the same team re-fetches status', async ({mount, page}) => {
        let callCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            callCount++;
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(teamStatusResponse({connections: [connStatus()]}))});
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('my-conn')).toBeVisible();
        const firstCount = callCount;

        await page.locator('button').filter({hasText: '\u00D7'}).click();
        await openModal(page, 'team1');
        await expect(page.getByText('my-conn')).toBeVisible();
        expect(callCount).toBeGreaterThan(firstCount);
    });

    test('re-opening replaces previous content with new data', async ({mount, page}) => {
        let callCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            callCount++;
            const body = callCount === 1 ? teamStatusResponse({connections: [connStatus({name: 'alpha'})], team_display_name: 'Alpha'}) : teamStatusResponse({connections: [connStatus({name: 'beta'})], team_display_name: 'Beta'});
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(body)});
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('alpha', {exact: true})).toBeVisible();

        await page.locator('button').filter({hasText: '\u00D7'}).click();
        await openModal(page, 'team2');
        await expect(page.getByText('beta', {exact: true})).toBeVisible();
        await expect(page.getByText('Cross Guard Settings for Beta')).toBeVisible();
    });

    test('null teamID in event detail does not render modal', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardTeamModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-team-modal', {detail: {teamID: null}}));
        });
        await expect(component).toBeEmpty();
    });

    test('missing teamID in event detail does not render modal', async ({mount, page}) => {
        await routeStatusOk(page);
        const component = await mount(<CrossguardTeamModal/>);
        await page.evaluate(() => {
            document.dispatchEvent(new CustomEvent('crossguard:open-team-modal', {detail: {}}));
        });
        await expect(component).toBeEmpty();
    });
});

// ---------------------------------------------------------------------------
// 2. Fetch errors
// ---------------------------------------------------------------------------
test.describe('Fetch errors edge cases', () => {
    test('500 with error JSON shows the error message', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            route.fulfill({status: 500, contentType: 'application/json', body: JSON.stringify({error: 'Internal server error'})});
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('Internal server error')).toBeVisible();
    });

    test('500 with non-JSON body shows network error', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            route.fulfill({status: 500, contentType: 'text/plain', body: 'Bad Gateway'});
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('Network error loading team status.')).toBeVisible();
    });

    test('network abort shows network error message', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            route.abort('connectionfailed');
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('Network error loading team status.')).toBeVisible();
    });

    test('connections null shows empty state', async ({mount, page}) => {
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(teamStatusResponse({connections: null}))});
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('No connections available. Configure connections in the System Console.')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 3. Rewrite controls
// ---------------------------------------------------------------------------
test.describe('Rewrite controls edge cases', () => {
    test('outbound connection does not show rewrite controls', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'out-conn', direction: 'outbound'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await expect(page.getByText('out-conn')).toBeVisible();
        await expect(page.getByText('No remote team rewrite')).not.toBeVisible();
        await expect(page.getByRole('button', {name: 'Set'})).not.toBeVisible();
    });

    test('inbound without rewrite shows Set button', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: ''})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await expect(page.getByText('No remote team rewrite')).toBeVisible();
        await expect(page.getByRole('button', {name: 'Set'})).toBeVisible();
    });

    test('inbound with rewrite shows Edit and Clear', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'target-team'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await expect(page.getByText('target-team')).toBeVisible();
        await expect(page.getByRole('button', {name: 'Edit'})).toBeVisible();
        await expect(page.getByRole('button', {name: 'Clear'})).toBeVisible();
    });

    test('Set opens empty input field', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: ''})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Set'}).click();
        const input = page.getByPlaceholder('Remote team name');
        await expect(input).toBeVisible();
        await expect(input).toHaveValue('');
    });

    test('Edit pre-fills the input with current remote_team_name', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'existing-name'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Edit'}).click();
        const input = page.getByPlaceholder('Remote team name');
        await expect(input).toHaveValue('existing-name');
    });

    test('Save disabled when input is empty', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: ''})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Set'}).click();
        await expect(page.getByRole('button', {name: 'Save'})).toBeDisabled();
    });

    test('Save disabled when input is whitespace only', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: ''})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Set'}).click();
        await page.getByPlaceholder('Remote team name').fill('   ');
        await expect(page.getByRole('button', {name: 'Save'})).toBeDisabled();
    });

    test('Enter does not save when input is empty', async ({mount, page}) => {
        await setCsrfCookie(page);
        let saved = false;
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route: any) => {
            if (route.request().method() === 'POST') {
                saved = true;
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: ''})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Set'}).click();
        await page.getByPlaceholder('Remote team name').press('Enter');
        expect(saved).toBe(false);
        await expect(page.getByPlaceholder('Remote team name')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 4. Rewrite operations
// ---------------------------------------------------------------------------
test.describe('Rewrite operations edge cases', () => {
    test('Save sends POST with correct body', async ({mount, page}) => {
        await setCsrfCookie(page);
        let capturedBody = '';
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route: any) => {
            if (route.request().method() === 'POST') {
                capturedBody = route.request().postData() || '';
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'old-name'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('new-name');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(() => {
            expect(JSON.parse(capturedBody)).toEqual({connection: 'in-conn', remote_team_name: 'new-name'});
        }).toPass();
    });

    test('Clear sends DELETE with correct query param', async ({mount, page}) => {
        await setCsrfCookie(page);
        let capturedUrl = '';
        let capturedMethod = '';
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route: any) => {
            capturedUrl = route.request().url();
            capturedMethod = route.request().method();
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        const body = teamStatusResponse({
            connections: [connStatus({name: 'clear-conn', direction: 'inbound', remote_team_name: 'some-team'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(() => {
            expect(capturedMethod).toBe('DELETE');
            expect(capturedUrl).toContain('/rewrite?connection=clear-conn');
        }).toPass();
    });

    test('save clears editing state after success', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route: any) => {
            if (route.request().method() === 'POST') {
                route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
            } else {
                route.continue();
            }
        });
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'old'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('new-val');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(page.getByPlaceholder('Remote team name')).not.toBeVisible();
    });

    test('clear clears editing state after success', async ({mount, page}) => {
        await setCsrfCookie(page);
        let fetchCount = 0;
        await page.route('**/plugins/crossguard/api/v1/teams/*/status', (route: any) => {
            fetchCount++;
            const respBody = fetchCount > 1 ? teamStatusResponse({connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: ''})]}) : teamStatusResponse({connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'old-team'})]});
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(respBody)});
        });
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite*', (route: any) => {
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByText('old-team')).toBeVisible();
        await page.getByRole('button', {name: 'Clear'}).click();
        await expect(page.getByText('No remote team rewrite')).toBeVisible();
    });

    test('save error shows banner with error message', async ({mount, page}) => {
        await setCsrfCookie(page);
        await page.route('**/plugins/crossguard/api/v1/teams/*/rewrite', (route: any) => {
            if (route.request().method() === 'POST') {
                route.fulfill({status: 400, contentType: 'application/json', body: JSON.stringify({error: 'Rewrite conflict'})});
            } else {
                route.continue();
            }
        });
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'old'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Edit'}).click();
        await page.getByPlaceholder('Remote team name').fill('conflict-name');
        await page.getByRole('button', {name: 'Save'}).click();
        await expect(page.getByText('Rewrite conflict')).toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 5. Escape in rewrite
// ---------------------------------------------------------------------------
test.describe('Escape in rewrite edge cases', () => {
    test('Escape in input cancels edit but does not close modal', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'existing'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Edit'}).click();
        await expect(page.getByPlaceholder('Remote team name')).toBeVisible();
        await page.getByPlaceholder('Remote team name').press('Escape');
        await expect(page.getByPlaceholder('Remote team name')).not.toBeVisible();
        await expect(page.getByText('Cross Guard Settings for Test Team')).toBeVisible();
    });

    test('Escape outside rewrite input closes modal', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'existing'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await expect(page.getByText('Cross Guard Settings for Test Team')).toBeVisible();
        await page.keyboard.press('Escape');
        await expect(page.getByText('Cross Guard Settings for Test Team')).not.toBeVisible();
    });

    test('Escape in input calls stopPropagation so modal stays open', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [connStatus({name: 'in-conn', direction: 'inbound', remote_team_name: 'team-x'})],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await page.getByRole('button', {name: 'Edit'}).click();
        const input = page.getByPlaceholder('Remote team name');
        await expect(input).toBeFocused();

        await input.press('Escape');
        await expect(input).not.toBeVisible();
        await expect(page.getByText('Cross Guard Settings for Test Team')).toBeVisible();

        // Now pressing Escape again should close the modal
        await page.keyboard.press('Escape');
        await expect(page.getByText('Cross Guard Settings for Test Team')).not.toBeVisible();
    });
});

// ---------------------------------------------------------------------------
// 6. Mixed states
// ---------------------------------------------------------------------------
test.describe('Mixed states edge cases', () => {
    test('multiple mixed connections render with correct badges', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [
                connStatus({name: 'in-a', direction: 'inbound', linked: true, remote_team_name: 'remote-a'}),
                connStatus({name: 'out-b', direction: 'outbound', linked: false}),
                connStatus({name: 'in-c', direction: 'inbound', linked: false, orphaned: true}),
            ],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await expect(page.getByText('in-a')).toBeVisible();
        await expect(page.getByText('out-b')).toBeVisible();
        await expect(page.getByText('in-c')).toBeVisible();
        const inboundBadges = page.getByText('NATS \u2192 MATTERMOST');
        const outboundBadges = page.getByText('MATTERMOST \u2192 NATS');
        await expect(inboundBadges).toHaveCount(2);
        await expect(outboundBadges).toHaveCount(1);
        await expect(page.getByTitle('Connection no longer in configuration')).toBeVisible();
    });

    test('action disables all buttons across multiple connections', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [
                connStatus({name: 'conn-a', direction: 'inbound', linked: false}),
                connStatus({name: 'conn-b', direction: 'outbound', linked: true}),
            ],
        });
        await routeStatusOk(page, body);
        await mount(<CrossguardTeamModal/>);
        await openModal(page, 'team1');
        await expect(page.getByRole('button', {name: 'Link', exact: true})).toBeVisible();

        await page.route('**/plugins/crossguard/api/v1/teams/*/init*', async (route: any) => {
            await new Promise((resolve) => setTimeout(resolve, 2000));
            route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify({status: 'ok'})});
        });
        await setCsrfCookie(page);
        await page.getByRole('button', {name: 'Link', exact: true}).click();
        await expect(page.getByText('Linking...')).toBeVisible();
        await expect(page.getByRole('button', {name: 'Unlink', exact: true})).toBeDisabled();
    });

    test('file transfer display variants render correctly', async ({mount, page}) => {
        const body = teamStatusResponse({
            connections: [
                connStatus({name: 'allow-conn', direction: 'inbound', file_transfer_enabled: true, file_filter_mode: 'allow', file_filter_types: '.pdf,.docx'}),
                connStatus({name: 'deny-conn', direction: 'outbound', file_transfer_enabled: true, file_filter_mode: 'deny', file_filter_types: '.exe'}),
                connStatus({name: 'all-conn', direction: 'inbound', file_transfer_enabled: true}),
                connStatus({name: 'disabled-conn', direction: 'outbound', file_transfer_enabled: false}),
            ],
        });
        await mountAndOpen(page, mount, 'team1', body);
        await expect(page.getByText('Allow .pdf,.docx')).toBeVisible();
        await expect(page.getByText('Deny .exe')).toBeVisible();
        await expect(page.getByText('All types')).toBeVisible();
        await expect(page.getByText('Files: Disabled').first()).toBeVisible();
    });
});
