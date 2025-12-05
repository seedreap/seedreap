// TransferBar component - shows overall transfer status

import m from 'mithril';
import { formatBytes, formatSpeed, formatDuration, getSpeedUnit, toggleSpeedUnit } from '../utils/format.js';
import { getSparklinePaths } from '../utils/sparkline.js';
import { calculateTransferStatus } from '../api.js';

const Sparkline = {
    view: () => {
        const { linePath, areaPath } = getSparklinePaths();
        return m('svg.sparkline.w-20.sm:w-28.h-8.hidden.sm:block', {
            viewBox: '0 0 120 35',
            preserveAspectRatio: 'none'
        }, [
            m('path.area', { d: areaPath }),
            m('path.line', { d: linePath })
        ]);
    }
};

const TransferBar = {
    view: () => {
        const status = calculateTransferStatus();
        const {
            totalFiles,
            completedFiles,
            syncingFiles,
            syncingJobs,
            totalSpeed,
            downloadingOnSeedboxCount,
            pausedOnSeedboxCount,
            syncTransferred,
            syncTotalSize,
            etaSeconds
        } = status;

        // Determine icon and title
        let icon, iconTitle, iconClass = '';
        if (syncingJobs > 0) {
            icon = '\u2B07'; // Down arrow
            iconTitle = 'Syncing: Transferring files from seedbox';
            iconClass = 'animate-pulse-opacity';
        } else if (totalFiles > 0 && completedFiles === totalFiles) {
            icon = '\u2713'; // Checkmark
            iconTitle = 'Complete: All files synced';
        } else if (downloadingOnSeedboxCount > 0) {
            icon = '\u23F3'; // Hourglass
            iconTitle = 'Waiting: Downloads in progress on seedbox';
        } else if (pausedOnSeedboxCount > 0) {
            icon = '\u23F8'; // Pause
            iconTitle = 'Paused: Downloads paused on seedbox';
        } else {
            icon = '\u25CF'; // Bullet
            iconTitle = 'Idle: Monitoring for new downloads';
        }

        const speedUnitLabel = getSpeedUnit() === 'bytes' ? 'MB/s' : 'Mbps';
        const speedUnitTitle = `Click to switch to ${getSpeedUnit() === 'bytes' ? 'Mbps' : 'MB/s'}`;

        return m('.card.bg-base-200.border.border-base-300.mb-6', [
            m('.card-body.p-4', [
                m('.grid.grid-cols-2.sm:grid-cols-4.gap-4.sm:gap-6', [
                    // Files stat
                    m('.flex.items-center.gap-3', [
                        m('span.text-2xl.cursor-help', {
                            class: iconClass,
                            title: iconTitle
                        }, icon),
                        m('div', [
                            m('.text-xs.opacity-50.uppercase.tracking-wide', 'Files'),
                            m('.text-lg.sm:text-xl.font-semibold', [
                                m('span', completedFiles),
                                syncingFiles > 0 && m('span.text-warning', `(+${syncingFiles})`),
                                m('span.opacity-50', '/'),
                                m('span', totalFiles)
                            ])
                        ])
                    ]),

                    // Transfer rate
                    m('.flex.items-center.gap-3', [
                        m('.flex-1', [
                            m('.text-xs.opacity-50.uppercase.tracking-wide.flex.items-center.gap-2', [
                                'Transfer Rate',
                                m('button.btn.btn-xs.btn-ghost', {
                                    onclick: (e) => {
                                        e.preventDefault();
                                        toggleSpeedUnit();
                                    },
                                    title: speedUnitTitle
                                }, speedUnitLabel)
                            ]),
                            m('.text-lg.sm:text-xl.font-semibold',
                                syncingJobs > 0
                                    ? m('span.text-success', formatSpeed(totalSpeed))
                                    : m('span.opacity-50', 'Idle')
                            )
                        ]),
                        m(Sparkline)
                    ]),

                    // Progress
                    m('div', [
                        m('.text-xs.opacity-50.uppercase.tracking-wide', 'Progress'),
                        m('.text-lg.sm:text-xl.font-semibold', [
                            m('span', formatBytes(syncTransferred)),
                            m('span.opacity-50.text-sm.sm:text-base', ' / '),
                            m('span', formatBytes(syncTotalSize))
                        ])
                    ]),

                    // ETA
                    m('div', [
                        m('.text-xs.opacity-50.uppercase.tracking-wide', 'ETA'),
                        m('.text-lg.sm:text-xl.font-semibold',
                            syncingJobs > 0
                                ? formatDuration(etaSeconds)
                                : '--'
                        )
                    ])
                ])
            ])
        ]);
    }
};

export default TransferBar;
