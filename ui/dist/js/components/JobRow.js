// JobRow component - displays a single job in the table

import m from 'mithril';
import { getDisplayState, getJobProgress } from '../state.js';
import { formatBytes, formatSpeed } from '../utils/format.js';
import { openJobModal } from './JobModal.js';
import {
    getJobStatusBadgeClass,
    getJobStatusProgressClass,
    getJobStatusTooltip
} from '../utils/statusConfig.js';

// Mobile card view for a job
const JobCard = {
    view: (vnode) => {
        const { job } = vnode.attrs;
        const displayState = getDisplayState(job);
        const progressInfo = getJobProgress(job);
        const progress = progressInfo.progress;

        const sizeDisplay = progressInfo.type === 'seedbox'
            ? `${formatBytes(job.total_size * job.seedbox_progress)} / ${formatBytes(job.total_size)}`
            : `${formatBytes(job.completed_size)} / ${formatBytes(job.total_size)}`;

        return m('.card.bg-base-200.border.border-base-300.overflow-hidden.cursor-pointer.hover:border-primary.transition-colors', {
            onclick: () => openJobModal(job)
        }, [
            m('.card-body.p-4', [
                m('.flex.justify-between.items-start.mb-2', [
                    m('span.font-medium.text-sm.truncate.flex-1.mr-2', job.name),
                    m('span.badge.badge-sm', {
                        class: getJobStatusBadgeClass(displayState),
                        title: getJobStatusTooltip(displayState)
                    }, displayState)
                ]),
                m('.flex.items-center.gap-2.mb-2.flex-wrap', [
                    m('span.badge.badge-ghost.badge-sm', job.downloader || '-'),
                    m('span.badge.badge-ghost.badge-sm', job.category || 'uncategorized'),
                    m('span.text-xs.opacity-70', `${job.total_files} file${job.total_files !== 1 ? 's' : ''}`)
                ]),
                m('.flex.items-center.gap-2', [
                    m('progress.progress.flex-1', {
                        class: getJobStatusProgressClass(displayState),
                        value: progress,
                        max: 100
                    }),
                    m('span.text-xs.opacity-70.w-10.text-right', `${Math.round(progress)}%`)
                ]),
                m('.text-xs.opacity-70.mt-2', [
                    sizeDisplay,
                    job.bytes_per_sec && m('span', ` • ${formatSpeed(job.bytes_per_sec)}`)
                ])
            ])
        ]);
    }
};

// Desktop table row for a job
const JobTableRow = {
    view: (vnode) => {
        const { job } = vnode.attrs;
        const displayState = getDisplayState(job);
        const progressInfo = getJobProgress(job);
        const progress = progressInfo.progress;

        const sizeDisplay = progressInfo.type === 'seedbox'
            ? `${formatBytes(job.total_size * job.seedbox_progress)} / ${formatBytes(job.total_size)}`
            : `${formatBytes(job.completed_size)} / ${formatBytes(job.total_size)}`;

        return m('tr.cursor-pointer.hover:bg-base-300.transition-colors', {
            key: job.id,
            onclick: () => openJobModal(job)
        }, [
            m('td.truncate.max-w-xs', { title: job.name }, job.name),
            m('td.hidden.xl:table-cell', [
                m('span.badge.badge-ghost.badge-sm', job.downloader || '-')
            ]),
            m('td.hidden.lg:table-cell', [
                m('span.badge.badge-ghost.badge-sm', job.category || 'uncategorized')
            ]),
            m('td.text-center.hidden.md:table-cell', job.total_files),
            m('td', [
                m('span.badge.badge-sm', {
                    class: getJobStatusBadgeClass(displayState),
                    title: getJobStatusTooltip(displayState)
                }, displayState)
            ]),
            m('td', [
                m('.flex.items-center.gap-2', [
                    m('progress.progress.progress-sm.w-20', {
                        class: getJobStatusProgressClass(displayState),
                        value: progress,
                        max: 100
                    }),
                    m('span.text-xs.opacity-70.w-10', `${Math.round(progress)}%`)
                ])
            ]),
            m('td.text-right.text-sm.opacity-70.hidden.md:table-cell', formatSpeed(job.bytes_per_sec)),
            m('td.text-sm.hidden.lg:table-cell', sizeDisplay)
        ]);
    }
};

export { JobCard, JobTableRow };
