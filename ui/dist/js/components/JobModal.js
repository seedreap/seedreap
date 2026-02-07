// JobModal component - shows job details with files and events

import m from 'mithril';
import { jobs, setFileSort, getSortedFiles, getDisplayState, getJobProgress, eventsList } from '../state.js';
import { fetchJobDetails, fetchEvents } from '../api.js';
import { formatBytes, formatSpeed, formatRelativeTime, formatTimestamp } from '../utils/format.js';
import {
    getJobStatusBadgeClass,
    getJobStatusProgressClass,
    getFileStatusBadgeClass,
    getFileStatusProgressClass
} from '../utils/statusConfig.js';
import { getEventInfo } from '../utils/eventConfig.js';

// Modal state
export const modalState = {
    isOpen: false,
    job: null,
    loading: false
};

export function openJobModal(job) {
    modalState.isOpen = true;
    modalState.job = job;
    modalState.loading = true;

    // Fetch download details and events
    Promise.all([
        fetchJobDetails(job.id),
        eventsList.items.length === 0 ? fetchEvents() : Promise.resolve()
    ]).finally(() => {
        modalState.loading = false;
        m.redraw();
    });
}

export function closeJobModal() {
    modalState.isOpen = false;
    modalState.job = null;
}

// Files table component
const FilesTable = {
    view: (vnode) => {
        const { job } = vnode.attrs;
        const files = job.files || [];

        if (files.length === 0) {
            return m('.text-center.py-4.text-base-content/50', 'No file details available');
        }

        const fileSort = jobs.fileSorts[job.id] || { column: 'name', direction: 'asc' };
        const sortedFiles = getSortedFiles(job.id, files);

        const sortIcon = (col) => {
            if (fileSort.column !== col) return '';
            return fileSort.direction === 'asc' ? ' ▲' : ' ▼';
        };

        const headerClass = 'cursor-pointer hover:bg-base-200 transition-colors';

        return m('.overflow-x-auto', [
            m('table.table.table-sm.w-full', [
                m('thead', [
                    m('tr.bg-base-200', [
                        m('th', {
                            class: headerClass,
                            onclick: () => setFileSort(job.id, 'name')
                        }, `Name${sortIcon('name')}`),
                        m('th', {
                            class: headerClass,
                            onclick: () => setFileSort(job.id, 'status')
                        }, `Status${sortIcon('status')}`),
                        m('th', {
                            class: headerClass,
                            onclick: () => setFileSort(job.id, 'progress')
                        }, `Progress${sortIcon('progress')}`),
                        m('th.text-right', {
                            class: headerClass,
                            onclick: () => setFileSort(job.id, 'speed')
                        }, `Speed${sortIcon('speed')}`),
                        m('th.text-right', {
                            class: headerClass,
                            onclick: () => setFileSort(job.id, 'size')
                        }, `Size${sortIcon('size')}`)
                    ])
                ]),
                m('tbody', sortedFiles.map(file => {
                    const progress = file.size > 0 ? (file.transferred / file.size * 100) : 0;
                    const fileName = file.path.split('/').pop();

                    return m('tr.hover', { key: file.path }, [
                        m('td.truncate.max-w-xs', { title: file.path }, fileName),
                        m('td', [
                            m('span.badge.badge-sm', {
                                class: getFileStatusBadgeClass(file.status)
                            }, file.status)
                        ]),
                        m('td', [
                            m('.flex.items-center.gap-2', [
                                m('progress.progress.progress-sm.w-16', {
                                    class: getFileStatusProgressClass(file.status),
                                    value: progress,
                                    max: 100
                                }),
                                m('span.text-xs.opacity-70.w-10', `${Math.round(progress)}%`)
                            ])
                        ]),
                        m('td.text-right.text-sm.opacity-70', formatSpeed(file.bytes_per_sec)),
                        m('td.text-right.text-sm.opacity-70', [
                            formatBytes(file.transferred),
                            ' / ',
                            formatBytes(file.size)
                        ])
                    ]);
                }))
            ])
        ]);
    }
};

// Get event details for display (details already parsed by normalizeEvent in api.js)
function getEventDetails(event) {
    const details = [];

    if (event.details && typeof event.details === 'object') {
        for (const [key, value] of Object.entries(event.details)) {
            let displayValue = value;
            if (key === 'size' || key === 'completed_size') {
                displayValue = formatBytes(value);
            }
            details.push({ label: key.replace(/_/g, ' '), value: displayValue });
        }
    }

    return details;
}

// Events table component
const EventsTable = {
    view: (vnode) => {
        const { job } = vnode.attrs;

        // Filter events for this job by subject_id (download job ID)
        const jobEvents = eventsList.items.filter(event =>
            event.subject_id === job.id
        );

        if (jobEvents.length === 0) {
            return m('.text-center.py-4.text-base-content/50', 'No events for this job');
        }

        return m('.overflow-x-auto', [
            m('table.table.table-sm.w-full', [
                m('thead', [
                    m('tr.bg-base-200', [
                        m('th.w-24', 'When'),
                        m('th.w-40', 'Event'),
                        m('th', 'Details')
                    ])
                ]),
                m('tbody', jobEvents.map(event => {
                    const info = getEventInfo(event.type);
                    const details = getEventDetails(event);

                    return m('tr.hover', { key: event.id }, [
                        m('td.text-xs.text-base-content/60.whitespace-nowrap', {
                            title: formatTimestamp(event.timestamp)
                        }, formatRelativeTime(event.timestamp)),
                        m('td.whitespace-nowrap', [
                            m('span.badge.badge-sm.whitespace-nowrap', { class: info.color }, info.label)
                        ]),
                        m('td.text-xs.text-base-content/60', [
                            details.length > 0
                                ? details.map((detail, i) => [
                                    i > 0 && ' • ',
                                    m('span', [
                                        m('span.font-medium', detail.label + ': '),
                                        String(detail.value)
                                    ])
                                ])
                                : '-'
                        ])
                    ]);
                }))
            ])
        ]);
    }
};

// Main modal component
const JobModal = {
    view: () => {
        if (!modalState.isOpen || !modalState.job) return null;

        // Get fresh download data from the list (ID is a ULID, globally unique)
        const job = jobs.list.find(j => j.id === modalState.job.id) || modalState.job;
        const displayState = getDisplayState(job);
        const progressInfo = getJobProgress(job);

        const sizeDisplay = progressInfo.type === 'seedbox'
            ? `${formatBytes(job.total_size * job.seedbox_progress)} / ${formatBytes(job.total_size)}`
            : `${formatBytes(job.completed_size)} / ${formatBytes(job.total_size)}`;

        return m('.modal.modal-open', [
            m('.modal-box.max-w-4xl.flex.flex-col', { style: { maxHeight: '90vh' } }, [
                // Header
                m('.flex.items-start.justify-between.gap-4.mb-4', [
                    m('div.min-w-0.flex-1', [
                        m('h3.text-lg.font-bold.truncate', { title: job.name }, job.name),
                        m('.flex.items-center.gap-2.mt-1.flex-wrap', [
                            m('span.badge.badge-sm', {
                                class: getJobStatusBadgeClass(displayState)
                            }, displayState),
                            m('span.badge.badge-ghost.badge-sm', job.downloader || '-'),
                            m('span.badge.badge-ghost.badge-sm', job.category || 'uncategorized'),
                            m('span.text-xs.text-base-content/60', `${job.total_files} file${job.total_files !== 1 ? 's' : ''}`),
                            m('span.text-xs.text-base-content/60', sizeDisplay)
                        ])
                    ]),
                    m('button.btn.btn-sm.btn-ghost.btn-square', {
                        onclick: closeJobModal
                    }, [
                        m('svg.w-5.h-5', {
                            xmlns: 'http://www.w3.org/2000/svg',
                            viewBox: '0 0 24 24',
                            fill: 'none',
                            stroke: 'currentColor',
                            'stroke-width': '2'
                        }, [
                            m('path', { d: 'M18 6L6 18' }),
                            m('path', { d: 'M6 6l12 12' })
                        ])
                    ])
                ]),

                // Progress bar
                m('.mb-4', [
                    m('.flex.items-center.gap-2', [
                        m('progress.progress.flex-1', {
                            class: getJobStatusProgressClass(displayState),
                            value: progressInfo.progress,
                            max: 100
                        }),
                        m('span.text-sm.font-medium.w-12.text-right', `${Math.round(progressInfo.progress)}%`),
                        job.bytes_per_sec > 0 && m('span.text-sm.text-base-content/60', formatSpeed(job.bytes_per_sec))
                    ])
                ]),

                m('.divider.my-2'),

                // Scrollable content
                m('.flex-1.overflow-y-auto.min-h-0', [
                    modalState.loading
                        ? m('.flex.items-center.justify-center.py-8', [
                            m('.loading.loading-spinner.loading-lg')
                        ])
                        : [
                            // Files section
                            m('section.mb-6', [
                                m('h4.font-semibold.mb-2', 'Files'),
                                m(FilesTable, { job })
                            ]),

                            // Events section
                            m('section', [
                                m('h4.font-semibold.mb-2', 'Events'),
                                m(EventsTable, { job })
                            ])
                        ]
                ]),

                // Footer
                m('.modal-action.mt-4', [
                    m('button.btn', { onclick: closeJobModal }, 'Close')
                ])
            ]),
            m('.modal-backdrop', { onclick: closeJobModal })
        ]);
    }
};

export default JobModal;
