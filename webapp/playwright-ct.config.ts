import path from 'path';

import {defineConfig, devices} from '@playwright/experimental-ct-react';

export default defineConfig({
    testMatch: '**/*.pw.tsx',
    testDir: './src',
    use: {
        ctViteConfig: {
            resolve: {
                alias: {
                    manifest: path.resolve(__dirname, 'src/manifest.ts'),
                },
            },
        },
    },
    projects: [
        {
            name: 'chromium',
            use: {...devices['Desktop Chrome']},
        },
    ],
});
