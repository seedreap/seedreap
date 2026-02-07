// Events page - shows full event history

import m from 'mithril';
import { eventsList, appsList, downloadersList } from '../state.js';
import { fetchEvents } from '../api.js';
import { formatRelativeTime, formatTimestamp, formatBytes } from '../utils/format.js';
import FilterDropdown from '../components/FilterDropdown.js';
import { getEventInfo, getEventLabel, getEventBadgeClass } from '../utils/eventConfig.js';

// Get all unique event types from the events
function getUniqueEventTypes(events) {
    const types = new Set();
    for (const event of events) {
        types.add(event.type);
    }
    return Array.from(types).sort();
}

// Get apps and categories from API data
function getAppsFromAPI() {
    return appsList.map(a => a.name).sort();
}

function getCategoriesFromAPI() {
    return [...new Set(appsList.map(a => a.category))].sort();
}

function getDownloadersFromAPI() {
    return downloadersList.map(d => d.name).sort();
}

// Get parsed details from event (already parsed by normalizeEvent in api.js)
function getDetails(event) {
    return event.details || null;
}

// Get the item description for an event based on its message
// The message field contains the human-readable description
function getItemDescription(event) {
    return event.message || 'Unknown event';
}

// Get subject type label
function getSubjectTypeLabel(subjectType) {
    switch (subjectType) {
        case 'download': return 'Download';
        case 'app': return 'App';
        case 'downloader': return 'Downloader';
        default: return 'System';
    }
}

// Get the details to show under the item description
function getItemDetails(event) {
    const details = [];

    // Add subject type
    if (event.subject_type) {
        details.push({ label: 'Subject', value: getSubjectTypeLabel(event.subject_type) });
    }

    // Add app name if present (for compound events)
    if (event.app_name) {
        details.push({ label: 'App', value: event.app_name });
    }

    // Add details (already parsed by normalizeEvent in api.js)
    const details_obj = getDetails(event);
    if (details_obj) {
        for (const [key, value] of Object.entries(details_obj)) {
            // Format size values
            let displayValue = value;
            if (key === 'size' || key === 'completed_size') {
                displayValue = formatBytes(value);
            }
            // Format the key to be more readable
            const label = key.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
            details.push({ label, value: displayValue });
        }
    }

    return details;
}

// Local filter state
const filterState = {
    search: '',
    selectedTypes: new Set(),
    selectedApps: new Set(),
    selectedSubjectTypes: new Set()
};


// Empty state
const EmptyState = {
    view: () => {
        return m('tr', [
            m('td.text-center.py-12', { colspan: 3 }, [
                m('.text-lg.font-medium.text-base-content/70', 'No events yet'),
                m('.text-sm.text-base-content/50.mt-2', 'Events will appear here as the system processes downloads')
            ])
        ]);
    }
};

// Loading state
const LoadingState = {
    view: () => {
        return m('tr', [
            m('td.text-center.py-12', { colspan: 3 }, [
                m('.loading.loading-spinner.loading-lg.mb-4'),
                m('.text-base-content/70', 'Loading events...')
            ])
        ]);
    }
};

// Main events page
const Events = {
    oninit: () => {
        fetchEvents();
    },

    view: () => {
        const allEventTypes = getUniqueEventTypes(eventsList.items);
        const allApps = getAppsFromAPI();
        const allSubjectTypes = ['download', 'app', 'downloader', ''];

        // Filter events
        let filteredEvents = eventsList.items;

        // Filter by selected types
        if (filterState.selectedTypes.size > 0) {
            filteredEvents = filteredEvents.filter(event =>
                filterState.selectedTypes.has(event.type)
            );
        }

        // Filter by selected apps - only show events that have a matching app_name
        if (filterState.selectedApps.size > 0) {
            filteredEvents = filteredEvents.filter(event =>
                event.app_name && filterState.selectedApps.has(event.app_name)
            );
        }

        // Filter by selected subject types
        if (filterState.selectedSubjectTypes.size > 0) {
            filteredEvents = filteredEvents.filter(event =>
                filterState.selectedSubjectTypes.has(event.subject_type || '')
            );
        }

        // Filter by search text
        if (filterState.search) {
            const search = filterState.search.toLowerCase();
            filteredEvents = filteredEvents.filter(event => {
                const message = (event.message || '').toLowerCase();
                const appName = (event.app_name || '').toLowerCase();
                const details = getItemDetails(event);
                const detailsText = details.map(d => `${d.label} ${d.value}`).join(' ').toLowerCase();
                return message.includes(search) || appName.includes(search) || detailsText.includes(search);
            });
        }

        const hasActiveFilters = filterState.search ||
            filterState.selectedTypes.size > 0 ||
            filterState.selectedApps.size > 0 ||
            filterState.selectedSubjectTypes.size > 0;

        return m('.flex-1.min-w-0.overflow-auto', [
            // Page header
            m('.px-4.sm:px-6.lg:px-8.py-4.border-b.border-base-300', [
                m('.flex.items-center.justify-between.flex-wrap.gap-4', [
                    m('h1.text-xl.font-semibold', 'Events'),
                    m('.flex.items-center.gap-2', [
                        m('.text-sm.text-base-content/60', [
                            `${filteredEvents.length}`,
                            hasActiveFilters ? ` of ${eventsList.items.length}` : '',
                            ` event${filteredEvents.length !== 1 ? 's' : ''}`
                        ]),
                        m('button.btn.btn-sm.btn-ghost', {
                            onclick: () => fetchEvents(),
                            title: 'Refresh'
                        }, [
                            m('svg.w-4.h-4', {
                                xmlns: 'http://www.w3.org/2000/svg',
                                viewBox: '0 0 24 24',
                                fill: 'none',
                                stroke: 'currentColor',
                                'stroke-width': '2',
                                'stroke-linecap': 'round',
                                'stroke-linejoin': 'round'
                            }, [
                                m('path', { d: 'M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8' }),
                                m('path', { d: 'M3 3v5h5' }),
                                m('path', { d: 'M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16' }),
                                m('path', { d: 'M16 16h5v5' })
                            ])
                        ])
                    ])
                ])
            ]),

            // Events table
            m('.px-4.sm:px-6.lg:px-8.py-4', [
                m('.overflow-x-auto', [
                    m('table.table.table-sm', [
                        // Header with filters
                        m('thead', [
                            // Filter row
                            m('tr.bg-base-200', [
                                m('th.w-24'),
                                m('th.w-48', [
                                    m(FilterDropdown, {
                                        label: 'Event',
                                        values: allEventTypes,
                                        filterSet: filterState.selectedTypes,
                                        getLabel: getEventLabel,
                                        getBadgeClass: getEventBadgeClass
                                    })
                                ]),
                                m('th', [
                                    m('.flex.items-center.gap-2.flex-wrap', [
                                        m(FilterDropdown, {
                                            label: 'App',
                                            values: allApps,
                                            filterSet: filterState.selectedApps
                                        }),
                                        m(FilterDropdown, {
                                            label: 'Subject',
                                            values: allSubjectTypes,
                                            filterSet: filterState.selectedSubjectTypes,
                                            getLabel: (v) => v ? getSubjectTypeLabel(v) : 'System'
                                        }),
                                        m('.flex-1'),
                                        m('input.input.input-xs.input-bordered.w-40', {
                                            type: 'text',
                                            placeholder: 'Search...',
                                            value: filterState.search,
                                            oninput: (e) => { filterState.search = e.target.value; }
                                        })
                                    ])
                                ])
                            ]),
                            // Column headers
                            m('tr', [
                                m('th', 'When'),
                                m('th', 'Event'),
                                m('th', 'Details')
                            ])
                        ]),
                        m('tbody', [
                            eventsList.loading && eventsList.items.length === 0
                                ? m(LoadingState)
                                : filteredEvents.length === 0
                                    ? m(EmptyState)
                                    : filteredEvents.map(event => {
                                        const info = getEventInfo(event.type);
                                        const itemDescription = getItemDescription(event);
                                        const itemDetails = getItemDetails(event);

                                        return m('tr.hover', { key: event.id }, [
                                            // When column
                                            m('td.text-xs.text-base-content/60.whitespace-nowrap', {
                                                title: formatTimestamp(event.timestamp)
                                            }, formatRelativeTime(event.timestamp)),

                                            // Event column
                                            m('td.whitespace-nowrap', [
                                                m('span.badge.badge-sm.whitespace-nowrap', { class: info.color }, info.label)
                                            ]),

                                            // Details column
                                            m('td', [
                                                m('.font-medium.text-sm', itemDescription),
                                                itemDetails.length > 0 && m('.text-xs.text-base-content/60.mt-0.5', [
                                                    itemDetails.map((detail, i) => [
                                                        i > 0 && m('span.mx-1', '|'),
                                                        m('span', [
                                                            m('span.font-medium', detail.label + ': '),
                                                            String(detail.value)
                                                        ])
                                                    ])
                                                ])
                                            ])
                                        ]);
                                    })
                        ])
                    ])
                ])
            ])
        ]);
    }
};

export default Events;
