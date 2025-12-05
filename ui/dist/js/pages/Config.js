// Config page - shows apps and downloaders configuration

import m from 'mithril';
import { appsList, downloadersList } from '../state.js';
import { getIconUrl } from '../utils/icons.js';

// State for modal
const modalState = {
    isOpen: false,
    type: null, // 'app' or 'downloader'
    item: null
};

function openModal(type, item) {
    modalState.isOpen = true;
    modalState.type = type;
    modalState.item = item;
}

function closeModal() {
    modalState.isOpen = false;
    modalState.type = null;
    modalState.item = null;
}

// Generic icon placeholder
const PlaceholderIcon = {
    view: (vnode) => {
        const { type } = vnode.attrs;
        const firstLetter = (type || '?')[0].toUpperCase();

        return m('.w-12.h-12.rounded-lg.bg-base-300.flex.items-center.justify-center.text-xl.font-bold.text-base-content/50', firstLetter);
    }
};

// App/Downloader icon component
const ItemIcon = {
    view: (vnode) => {
        const { type, name } = vnode.attrs;
        const iconUrl = getIconUrl(type);

        if (iconUrl) {
            return m('.w-12.h-12.rounded-lg.bg-base-300.p-2.flex.items-center.justify-center', [
                m('img.w-full.h-full.object-contain', {
                    src: iconUrl,
                    alt: name,
                    onerror: (e) => {
                        // Hide broken image and show placeholder
                        e.target.style.display = 'none';
                    }
                })
            ]);
        }

        return m(PlaceholderIcon, { type });
    }
};

// Card component for apps/downloaders
const ConfigCard = {
    view: (vnode) => {
        const { item, type, onclick } = vnode.attrs;

        return m('.card.bg-base-200.border.border-base-300.cursor-pointer.hover:border-primary.transition-colors', {
            onclick: onclick
        }, [
            m('.card-body.p-4.flex.flex-row.items-center.gap-4', [
                m(ItemIcon, { type: item.type, name: item.name }),
                m('.flex-1.min-w-0', [
                    m('h3.font-semibold.truncate', item.name),
                    m('p.text-sm.opacity-70.truncate', item.type),
                    type === 'app' && item.category && m('span.badge.badge-sm.badge-ghost.mt-1', item.category)
                ]),
                m('svg.w-5.h-5.opacity-50', {
                    xmlns: 'http://www.w3.org/2000/svg',
                    viewBox: '0 0 24 24',
                    fill: 'none',
                    stroke: 'currentColor',
                    'stroke-width': '2'
                }, m('path', { d: 'M9 18l6-6-6-6' }))
            ])
        ]);
    }
};

// Modal for showing config details
const ConfigModal = {
    view: () => {
        if (!modalState.isOpen || !modalState.item) return null;

        const { type, item } = modalState;
        const title = type === 'app' ? 'App Configuration' : 'Downloader Configuration';

        return m('.modal.modal-open', [
            m('.modal-box.max-w-lg', [
                // Header
                m('.flex.items-center.gap-4.mb-4', [
                    m(ItemIcon, { type: item.type, name: item.name }),
                    m('div', [
                        m('h3.text-lg.font-bold', item.name),
                        m('p.text-sm.opacity-70', item.type)
                    ])
                ]),

                m('.divider.my-2'),

                // Config details
                m('.space-y-3', [
                    m('.flex.justify-between.items-center', [
                        m('span.text-sm.opacity-70', 'Name'),
                        m('span.font-medium', item.name)
                    ]),
                    m('.flex.justify-between.items-center', [
                        m('span.text-sm.opacity-70', 'Type'),
                        m('span.badge.badge-ghost', item.type)
                    ]),
                    type === 'app' && item.category && m('.flex.justify-between.items-center', [
                        m('span.text-sm.opacity-70', 'Category'),
                        m('span.badge.badge-primary', item.category)
                    ])
                ]),

                // Actions
                m('.modal-action', [
                    m('button.btn', { onclick: closeModal }, 'Close')
                ])
            ]),
            m('.modal-backdrop', { onclick: closeModal })
        ]);
    }
};

// Section header component
const SectionHeader = {
    view: (vnode) => {
        const { title, count } = vnode.attrs;

        return m('.flex.items-center.justify-between.mb-4', [
            m('h2.text-lg.font-semibold', title),
            count !== undefined && m('span.badge.badge-ghost', count)
        ]);
    }
};

// Main Config page
const Config = {
    view: () => {
        return m('.flex-1.min-h-0.overflow-auto', [
            // Page header
            m('.px-4.sm:px-6.lg:px-8.py-4.border-b.border-base-300', [
                m('h1.text-xl.font-semibold', 'Configuration')
            ]),

            // Content
            m('.px-4.sm:px-6.lg:px-8.py-6', [
                // Apps section
                m('section.mb-8', [
                    m(SectionHeader, { title: 'Apps', count: appsList.length }),
                    appsList.length > 0
                        ? m('.grid.grid-cols-1.sm:grid-cols-2.lg:grid-cols-3.gap-4',
                            appsList.map(app =>
                                m(ConfigCard, {
                                    key: app.name,
                                    item: app,
                                    type: 'app',
                                    onclick: () => openModal('app', app)
                                })
                            )
                        )
                        : m('.text-center.py-8.opacity-50', 'No apps configured')
                ]),

                // Downloaders section
                m('section.mb-8', [
                    m(SectionHeader, { title: 'Downloaders', count: downloadersList.length }),
                    downloadersList.length > 0
                        ? m('.grid.grid-cols-1.sm:grid-cols-2.lg:grid-cols-3.gap-4',
                            downloadersList.map(dl =>
                                m(ConfigCard, {
                                    key: dl.name,
                                    item: dl,
                                    type: 'downloader',
                                    onclick: () => openModal('downloader', dl)
                                })
                            )
                        )
                        : m('.text-center.py-8.opacity-50', 'No downloaders configured')
                ])
            ]),

            // Modal
            m(ConfigModal)
        ]);
    }
};

export default Config;
