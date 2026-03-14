import {test, expect} from '@playwright/test';
import manifest from 'manifest';

test.describe('manifest', () => {
    test('plugin manifest, id and version are defined', () => {
        expect(manifest).toBeDefined();
        expect(manifest.id).toBeDefined();
        expect(manifest.version).toBeDefined();
    });
});
