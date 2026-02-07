// Main application entry point

import m from 'mithril';
import { refresh, initialFetch } from './api.js';
import { initTheme } from './state.js';
import { POLL_INTERVAL } from './config.js';

import Layout from './components/Layout.js';
import Dashboard from './pages/Dashboard.js';
import Config from './pages/Config.js';
import Settings from './pages/Settings.js';
import Events from './pages/Events.js';

// Wrap page in layout
function withLayout(PageComponent) {
    return {
        onmatch: () => PageComponent,
        render: (vnode) => m(Layout, vnode)
    };
}

// Initialize app
async function init() {
    // Initialize theme from preferences/system
    initTheme();

    // Initial data fetch (includes apps, downloaders, speed history)
    await initialFetch();

    // Start polling
    setInterval(() => {
        refresh();
    }, POLL_INTERVAL);
}

// Initialize and set up routes
init();

m.route(document.getElementById('app'), '/', {
    '/': withLayout(Dashboard),
    '/config': withLayout(Config),
    '/settings': withLayout(Settings),
    '/events': withLayout(Events)
});
