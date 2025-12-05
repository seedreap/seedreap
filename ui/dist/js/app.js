// Main application entry point

import m from 'mithril';
import { refresh, initialFetch } from './api.js';

import Layout from './components/Layout.js';
import Dashboard from './pages/Dashboard.js';
import Config from './pages/Config.js';
import Settings from './pages/Settings.js';
import Timeline from './pages/Timeline.js';

// Wrap page in layout
function withLayout(PageComponent) {
    return {
        onmatch: () => PageComponent,
        render: (vnode) => m(Layout, vnode)
    };
}

// Initialize app
async function init() {
    // Initial data fetch (includes apps, downloaders, speed history)
    await initialFetch();

    // Start polling every 3 seconds
    setInterval(() => {
        refresh();
    }, 3000);
}

// Initialize and set up routes
init();

m.route(document.getElementById('app'), '/', {
    '/': withLayout(Dashboard),
    '/config': withLayout(Config),
    '/settings': withLayout(Settings),
    '/timeline': withLayout(Timeline)
});
