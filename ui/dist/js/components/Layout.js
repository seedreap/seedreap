// Layout component with drawer sidebar

import m from 'mithril';
import TopBar from './TopBar.js';

// Sidebar state - persisted to localStorage
const sidebarState = {
    open: localStorage.getItem('sidebarOpen') !== 'false', // default open (desktop)
    mobileOpen: false // mobile overlay state
};

function toggleSidebar() {
    sidebarState.open = !sidebarState.open;
    localStorage.setItem('sidebarOpen', sidebarState.open);
}

function toggleMobileSidebar() {
    sidebarState.mobileOpen = !sidebarState.mobileOpen;
}

function closeMobileSidebar() {
    sidebarState.mobileOpen = false;
}

// Icons for sidebar navigation
const HomeIcon = {
    view: () => m('svg.size-5', {
        xmlns: 'http://www.w3.org/2000/svg',
        viewBox: '0 0 24 24',
        'stroke-linejoin': 'round',
        'stroke-linecap': 'round',
        'stroke-width': '2',
        fill: 'none',
        stroke: 'currentColor'
    }, [
        m('path', { d: 'M15 21v-8a1 1 0 0 0-1-1h-4a1 1 0 0 0-1 1v8' }),
        m('path', { d: 'M3 10a2 2 0 0 1 .709-1.528l7-5.999a2 2 0 0 1 2.582 0l7 5.999A2 2 0 0 1 21 10v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z' })
    ])
};

// Gear icon for Configuration
const ConfigIcon = {
    view: () => m('svg.size-5', {
        xmlns: 'http://www.w3.org/2000/svg',
        viewBox: '0 0 24 24',
        'stroke-linejoin': 'round',
        'stroke-linecap': 'round',
        'stroke-width': '2',
        fill: 'none',
        stroke: 'currentColor'
    }, [
        m('path', { d: 'M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z' }),
        m('circle', { cx: '12', cy: '12', r: '3' })
    ])
};

// Clock icon for Timeline
const TimelineIcon = {
    view: () => m('svg.size-5', {
        xmlns: 'http://www.w3.org/2000/svg',
        viewBox: '0 0 24 24',
        'stroke-linejoin': 'round',
        'stroke-linecap': 'round',
        'stroke-width': '2',
        fill: 'none',
        stroke: 'currentColor'
    }, [
        m('circle', { cx: '12', cy: '12', r: '10' }),
        m('polyline', { points: '12 6 12 12 16 14' })
    ])
};

// Sliders icon for Settings
const SettingsIcon = {
    view: () => m('svg.size-5', {
        xmlns: 'http://www.w3.org/2000/svg',
        viewBox: '0 0 24 24',
        'stroke-linejoin': 'round',
        'stroke-linecap': 'round',
        'stroke-width': '2',
        fill: 'none',
        stroke: 'currentColor'
    }, [
        m('line', { x1: '4', y1: '21', x2: '4', y2: '14' }),
        m('line', { x1: '4', y1: '10', x2: '4', y2: '3' }),
        m('line', { x1: '12', y1: '21', x2: '12', y2: '12' }),
        m('line', { x1: '12', y1: '8', x2: '12', y2: '3' }),
        m('line', { x1: '20', y1: '21', x2: '20', y2: '16' }),
        m('line', { x1: '20', y1: '12', x2: '20', y2: '3' }),
        m('line', { x1: '1', y1: '14', x2: '7', y2: '14' }),
        m('line', { x1: '9', y1: '8', x2: '15', y2: '8' }),
        m('line', { x1: '17', y1: '16', x2: '23', y2: '16' })
    ])
};

// Collapse/Expand arrow icons
const CollapseIcon = {
    view: () => m('svg.size-5', {
        xmlns: 'http://www.w3.org/2000/svg',
        viewBox: '0 0 24 24',
        fill: 'none',
        stroke: 'currentColor',
        'stroke-width': '2',
        'stroke-linecap': 'round',
        'stroke-linejoin': 'round'
    }, [
        m('path', { d: 'M15 18l-6-6 6-6' })
    ])
};

const ExpandIcon = {
    view: () => m('svg.size-5', {
        xmlns: 'http://www.w3.org/2000/svg',
        viewBox: '0 0 24 24',
        fill: 'none',
        stroke: 'currentColor',
        'stroke-width': '2',
        'stroke-linecap': 'round',
        'stroke-linejoin': 'round'
    }, [
        m('path', { d: 'M9 18l6-6-6-6' })
    ])
};

// Close icon for mobile
const CloseIcon = {
    view: () => m('svg.size-5', {
        xmlns: 'http://www.w3.org/2000/svg',
        viewBox: '0 0 24 24',
        fill: 'none',
        stroke: 'currentColor',
        'stroke-width': '2',
        'stroke-linecap': 'round',
        'stroke-linejoin': 'round'
    }, [
        m('path', { d: 'M18 6L6 18' }),
        m('path', { d: 'M6 6l12 12' })
    ])
};

// Sidebar content (shared between desktop and mobile)
const SidebarContent = {
    view: (vnode) => {
        const { isOpen, showCollapseButton, onClose } = vnode.attrs;
        const currentRoute = m.route.get() || '/';

        return m('.bg-base-200.flex.flex-col.h-full', [
            // Logo header for mobile only
            onClose && m('.flex.items-center.justify-between.p-4.border-b.border-base-300', [
                m('.flex.items-center.gap-3', [
                    m('img.w-8.h-8', { src: '/logo.svg', alt: 'SeedReap' }),
                    m('span.text-lg.font-semibold', 'SeedReap')
                ]),
                m('button.btn.btn-ghost.btn-sm.btn-square', {
                    onclick: onClose
                }, m(CloseIcon))
            ]),

            // Main navigation
            m('ul.menu.flex-1.p-2.gap-1', [
                m('li', [
                    m('a.flex.items-center.gap-3', {
                        href: '#!/',
                        class: currentRoute === '/' ? 'active' : '',
                        title: 'Dashboard',
                        onclick: onClose
                    }, [
                        m(HomeIcon),
                        isOpen && m('span', 'Dashboard')
                    ])
                ]),
                m('li', [
                    m('a.flex.items-center.gap-3', {
                        href: '#!/config',
                        class: currentRoute === '/config' ? 'active' : '',
                        title: 'Configuration',
                        onclick: onClose
                    }, [
                        m(ConfigIcon),
                        isOpen && m('span', 'Configuration')
                    ])
                ]),
                m('li', [
                    m('a.flex.items-center.gap-3', {
                        href: '#!/timeline',
                        class: currentRoute === '/timeline' ? 'active' : '',
                        title: 'Timeline',
                        onclick: onClose
                    }, [
                        m(TimelineIcon),
                        isOpen && m('span', 'Timeline')
                    ])
                ])
            ]),

            // Bottom section - Settings + Collapse toggle
            m('.border-t.border-base-300', [
                // Settings link
                m('ul.menu.p-2.gap-1', [
                    m('li', [
                        m('a.flex.items-center.gap-3', {
                            href: '#!/settings',
                            class: currentRoute === '/settings' ? 'active' : '',
                            title: 'Settings',
                            onclick: onClose
                        }, [
                            m(SettingsIcon),
                            isOpen && m('span', 'Settings')
                        ])
                    ])
                ]),

                // Collapse/Expand button (desktop only)
                showCollapseButton && m('.px-2.pb-2', [
                    m('button.btn.btn-ghost.btn-sm.w-full.flex.items-center', {
                        onclick: toggleSidebar,
                        title: isOpen ? 'Collapse sidebar' : 'Expand sidebar',
                        class: isOpen ? 'justify-start gap-2' : 'justify-center'
                    }, [
                        isOpen ? m(CollapseIcon) : m(ExpandIcon),
                        isOpen && m('span.text-xs.text-base-content/70', 'Collapse')
                    ])
                ])
            ])
        ]);
    }
};

// Desktop sidebar
const DesktopSidebar = {
    view: () => {
        const isOpen = sidebarState.open;

        return m('.border-r.border-base-300.transition-all.duration-200.overflow-hidden.h-full', {
            class: isOpen ? 'w-64' : 'w-16'
        }, [
            m(SidebarContent, { isOpen, showCollapseButton: true })
        ]);
    }
};

// Mobile sidebar overlay
const MobileSidebar = {
    view: () => {
        if (!sidebarState.mobileOpen) return null;

        return m('.fixed.inset-0.z-50.lg:hidden', [
            // Backdrop
            m('.absolute.inset-0.bg-black/50', {
                onclick: closeMobileSidebar
            }),
            // Sidebar panel
            m('.absolute.left-0.top-0.bottom-0.w-64.shadow-xl', [
                m(SidebarContent, { isOpen: true, showCollapseButton: false, onClose: closeMobileSidebar })
            ])
        ]);
    }
};

const Layout = {
    view: (vnode) => {
        return m('.flex.flex-col.h-full', [
            // Top bar - full width at the top
            m(TopBar, { onToggleSidebar: toggleMobileSidebar }),

            // Content area with sidebar
            m('.flex.flex-1.min-h-0', [
                // Desktop sidebar - hidden on mobile
                m('.hidden.lg:flex.h-full', [
                    m(DesktopSidebar)
                ]),

                // Page content
                m('.flex-1.overflow-auto.min-w-0', vnode.children)
            ]),

            // Mobile sidebar overlay
            m(MobileSidebar)
        ]);
    }
};

export default Layout;
