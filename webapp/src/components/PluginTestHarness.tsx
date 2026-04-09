import React from 'react';

import {getChannelConnections, setChannelConnections} from '../connection_state';
import Plugin from '../index';

// Expose Plugin class and connection_state helpers to the browser window
// so page.evaluate() can access them without dynamic imports.
/* eslint-disable no-underscore-dangle */
(window as any).__PluginClass = Plugin;
(window as any).__getChannelConnections = getChannelConnections;
(window as any).__setChannelConnections = setChannelConnections;
/* eslint-enable no-underscore-dangle */

const PluginTestHarness: React.FC = () => <div data-testid='plugin-harness'/>;

export default PluginTestHarness;
