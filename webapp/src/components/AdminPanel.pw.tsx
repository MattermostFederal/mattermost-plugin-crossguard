import {test, expect} from '@playwright/experimental-ct-react';
import React from 'react';

import AdminPanel from './AdminPanel';

test.describe('AdminPanel', () => {
    test('renders plugin name from manifest', async ({mount}) => {
        const component = await mount(<AdminPanel/>);
        await expect(component.getByRole('heading', {name: 'Cross Guard'})).toBeVisible();
    });

    test('renders version string from manifest', async ({mount}) => {
        const component = await mount(<AdminPanel/>);
        await expect(component.locator('text=Version:')).toBeVisible();
    });

    test('renders documentation link text', async ({mount}) => {
        const component = await mount(<AdminPanel/>);
        await expect(component.getByText('View Cross Guard Documentation')).toBeVisible();
    });

    test('documentation link href points to help page', async ({mount}) => {
        const component = await mount(<AdminPanel/>);
        const link = component.getByRole('link', {name: 'View Cross Guard Documentation'});
        await expect(link).toHaveAttribute('href', '/plugins/crossguard/public/help/help.html');
    });

    test('documentation link opens in new tab with security attributes', async ({mount}) => {
        const component = await mount(<AdminPanel/>);
        const link = component.getByRole('link', {name: 'View Cross Guard Documentation'});
        await expect(link).toHaveAttribute('target', '_blank');
        await expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    });

    test('container has padding', async ({mount, page}) => {
        await mount(<AdminPanel/>);
        const container = page.locator('[style*="padding"]').first();
        await expect(container).toHaveCSS('padding', '20px');
    });
});
