// TopBar component - global header with transfer status

import m from 'mithril';
import { formatSpeed, formatDuration } from '../utils/format.js';
import { getSparklinePaths } from '../utils/sparkline.js';
import { calculateTransferStatus } from '../api.js';
import { connection } from '../state.js';

// Hamburger menu icon
const HamburgerIcon = {
    view: () => m('svg.size-5', {
        xmlns: 'http://www.w3.org/2000/svg',
        viewBox: '0 0 24 24',
        fill: 'none',
        stroke: 'currentColor',
        'stroke-width': '2',
        'stroke-linecap': 'round',
        'stroke-linejoin': 'round'
    }, [
        m('line', { x1: '4', y1: '6', x2: '20', y2: '6' }),
        m('line', { x1: '4', y1: '12', x2: '20', y2: '12' }),
        m('line', { x1: '4', y1: '18', x2: '20', y2: '18' })
    ])
};

// Sparkline component - uses currentColor with text-success class
const Sparkline = {
    view: () => {
        const { linePath, areaPath } = getSparklinePaths();
        if (!linePath) {
            return m('.h-8.w-24.sm:w-32');
        }
        return m('svg.h-8.w-24.sm:w-32.text-success', {
            viewBox: '0 0 120 35',
            preserveAspectRatio: 'none'
        }, [
            m('path', {
                d: areaPath,
                style: { fill: 'currentColor', opacity: '0.2', stroke: 'none' }
            }),
            m('path', {
                d: linePath,
                style: { fill: 'none', stroke: 'currentColor', strokeWidth: '2' }
            })
        ]);
    }
};

const TopBar = {
    view: (vnode) => {
        const { onToggleSidebar } = vnode.attrs;
        const status = calculateTransferStatus();
        const { syncingJobs, totalSpeed, etaSeconds } = status;

        return m('.navbar.bg-base-200.border-b.border-base-300.px-2.pr-4.min-h-12.gap-2', [
            // Hamburger menu - mobile only
            m('button.btn.btn-square.btn-ghost.btn-sm.lg:hidden', {
                onclick: onToggleSidebar,
                'aria-label': 'toggle sidebar'
            }, m(HamburgerIcon)),

            // Logo and app name - always visible
            // On mobile: show logo inline after hamburger
            m('.flex.lg:hidden.items-center.gap-2.mr-4', [
                m('img.w-8.h-8', { src: '/logo.svg', alt: 'SeedReap' }),
                m('span.font-semibold.text-lg', 'SeedReap')
            ]),
            // On desktop: logo in fixed-width container to align with sidebar icons (w-16)
            m('.hidden.lg:flex.w-12.justify-center.flex-none', [
                m('img.w-8.h-8', { src: '/logo.svg', alt: 'SeedReap' })
            ]),
            m('span.hidden.lg:inline.font-semibold.text-lg.mr-4', 'SeedReap'),

            // ETA section
            m('.flex.items-center.gap-1.text-sm', [
                m('span.text-base-content/50.hidden.sm:inline', 'ETA:'),
                m('span.font-medium.min-w-12',
                    syncingJobs > 0 ? formatDuration(etaSeconds) : '--'
                )
            ]),

            // Spacer
            m('.flex-1'),

            // Sparkline + Speed together on the right
            m('.flex.items-center.gap-3', [
                m(Sparkline),
                m('span.font-medium.text-sm', {
                    class: syncingJobs > 0 ? 'text-success' : 'text-base-content/50'
                }, syncingJobs > 0 ? formatSpeed(totalSpeed) : '0 B/s')
            ]),

            // Connection status indicator
            m('.w-3.h-3.rounded-full.ml-4', {
                class: {
                    'connecting': 'bg-warning',
                    'connected': 'bg-success',
                    'disconnected': 'bg-error'
                }[connection.status] || 'bg-warning',
                title: {
                    'connecting': 'Connecting...',
                    'connected': 'Connected',
                    'disconnected': 'Disconnected'
                }[connection.status] || 'Unknown'
            })
        ]);
    }
};

export default TopBar;
