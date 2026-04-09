import {test, expect} from '@playwright/experimental-ct-react';
import React from 'react';

import CrossguardChannelIndicator from './CrossguardChannelIndicator';
import CrossguardChannelIndicatorStory from './CrossguardChannelIndicatorStory';

// Note: The icon span uses an icon font class (icon-circle-multiple-outline) which
// isn't loaded in the test environment. The element exists in the DOM but has zero
// dimensions, so we use toBeAttached() instead of toBeVisible().
//
// Playwright CT's component locator doesn't track content that appears via async
// re-render (the indicator renders null initially, then re-renders after useEffect
// fires setChannelConnections). We use page.getByTestId() for dynamically rendered
// content instead of component.getByTestId().

test.describe('CrossguardChannelIndicator', () => {
    test('renders null when channel has no connections', async ({mount, page}) => {
        await mount(
            <CrossguardChannelIndicator channel={{id: 'ch-empty'}}/>,
        );
        await expect(page.getByTestId('SharedChannelIcon')).not.toBeAttached();
    });

    test('renders SharedChannelIcon when connections exist', async ({mount, page}) => {
        await mount(
            <CrossguardChannelIndicatorStory
                channelId={'ch-icon'}
                connections={'ConnectionA'}
            />,
        );
        await expect(page.getByTestId('SharedChannelIcon')).toBeAttached();
    });

    test('displays connection names as title attribute', async ({mount, page}) => {
        await mount(
            <CrossguardChannelIndicatorStory
                channelId={'ch-title'}
                connections={'ConnA, ConnB'}
            />,
        );
        await expect(page.getByTestId('SharedChannelIcon')).toHaveAttribute('title', 'ConnA, ConnB');
    });

    test('updates reactively when connections change', async ({mount, page}) => {
        const component = await mount(
            <CrossguardChannelIndicatorStory
                channelId={'ch-reactive'}
                connections={''}
            />,
        );

        // Initially no icon
        await expect(page.getByTestId('SharedChannelIcon')).not.toBeAttached();

        // Update with connections
        await component.update(
            <CrossguardChannelIndicatorStory
                channelId={'ch-reactive'}
                connections={'NewConn'}
            />,
        );

        await expect(page.getByTestId('SharedChannelIcon')).toBeAttached();
        await expect(page.getByTestId('SharedChannelIcon')).toHaveAttribute('title', 'NewConn');
    });

    test('removes icon when connections are cleared', async ({mount, page}) => {
        const component = await mount(
            <CrossguardChannelIndicatorStory
                channelId={'ch-clear'}
                connections={'SomeConn'}
            />,
        );
        await expect(page.getByTestId('SharedChannelIcon')).toBeAttached();

        await component.update(
            <CrossguardChannelIndicatorStory
                channelId={'ch-clear'}
                connections={''}
            />,
        );
        await expect(page.getByTestId('SharedChannelIcon')).not.toBeAttached();
    });

    test('shows correct title for its channel', async ({mount, page}) => {
        await mount(
            <CrossguardChannelIndicatorStory
                channelId={'ch-own'}
                connections={'OwnConn'}
            />,
        );
        await expect(page.getByTestId('SharedChannelIcon')).toHaveAttribute('title', 'OwnConn');
    });

    test('has correct CSS class on the icon element', async ({mount, page}) => {
        await mount(
            <CrossguardChannelIndicatorStory
                channelId={'ch-class'}
                connections={'X'}
            />,
        );

        const icon = page.getByTestId('SharedChannelIcon');
        await expect(icon).toBeAttached();
        const className = await icon.getAttribute('class');
        expect(className).toContain('icon');
        expect(className).toContain('icon-circle-multiple-outline');
    });

    test('handles undefined channel id gracefully', async ({mount, page}) => {
        await mount(
            <CrossguardChannelIndicator channel={{id: undefined as unknown as string}}/>,
        );
        await expect(page.getByTestId('SharedChannelIcon')).not.toBeAttached();
    });
});
