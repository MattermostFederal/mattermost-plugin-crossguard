// Setup file for Playwright component tests

// Stub window.registerPlugin before index.tsx module-level code runs.
if (!(window as any).registerPlugin) {
    (window as any).registerPlugin = () => {};
}

// Expose connection_state module for test manipulation
import {setChannelConnections, getChannelConnections} from '../src/connection_state';

(window as any).__connectionState = {setChannelConnections, getChannelConnections};
