// JobsList component - displays all jobs with inline filters

import m from 'mithril';
import {
    jobs,
    filters,
    saveFilters,
    setJobSort,
    getSortedJobs,
    getFilteredJobs,
    getActiveFilterCount,
    appsList,
    downloadersList
} from '../state.js';
import { JobCard, JobTableRow } from './JobRow.js';
import JobModal from './JobModal.js';
import FilterDropdown from './FilterDropdown.js';
import {
    jobStatusValues,
    getJobStatusLabel,
    getJobStatusBadgeClass
} from '../utils/statusConfig.js';

// Filter bar component
const FilterBar = {
    view: () => {
        // Get sorted lists from API data
        const allApps = appsList.map(a => a.name).sort();
        const allCategories = [...new Set(appsList.map(a => a.category))].sort();
        const allDownloaders = downloadersList.map(d => d.name).sort();

        return m('.flex.items-center.gap-2.flex-wrap.mb-4.bg-base-200.rounded-lg.p-2.border.border-base-300', [
            m(FilterDropdown, {
                label: 'Status',
                values: jobStatusValues,
                filterSet: filters.status,
                getLabel: getJobStatusLabel,
                getBadgeClass: getJobStatusBadgeClass,
                onFilterChange: saveFilters
            }),
            m(FilterDropdown, {
                label: 'App',
                values: allApps,
                filterSet: filters.apps,
                onFilterChange: saveFilters
            }),
            m(FilterDropdown, {
                label: 'Category',
                values: allCategories,
                filterSet: filters.categories,
                onFilterChange: saveFilters
            }),
            m(FilterDropdown, {
                label: 'Downloader',
                values: allDownloaders,
                filterSet: filters.downloaders,
                onFilterChange: saveFilters
            }),
            m('.flex-1'),
            m('input.input.input-xs.input-bordered.w-40', {
                type: 'text',
                placeholder: 'Search...',
                value: filters.search,
                oninput: (e) => { filters.search = e.target.value; }
            })
        ]);
    }
};

const JobsList = {
    view: () => {
        const filteredJobs = getFilteredJobs(jobs.list);
        const sortedJobs = getSortedJobs(filteredJobs);
        const hasActiveFilters = getActiveFilterCount() > 0;

        const sortIcon = (col) => {
            if (jobs.sort.column !== col) return '';
            return jobs.sort.direction === 'asc' ? ' ▲' : ' ▼';
        };

        const headerClass = 'cursor-pointer hover:bg-base-200 transition-colors';

        // Empty states
        if (sortedJobs.length === 0) {
            if (jobs.list.length === 0) {
                return m('section.mb-6', [
                    m('.flex.items-center.justify-between.mb-4', [
                        m('h2.text-lg.font-semibold.opacity-70', 'Sync Jobs')
                    ]),
                    m(FilterBar),
                    m('.text-center.py-10.opacity-50', 'No sync jobs')
                ]);
            }
            return m('section.mb-6', [
                m('.flex.items-center.justify-between.mb-4', [
                    m('h2.text-lg.font-semibold.opacity-70', 'Sync Jobs'),
                    m('span.text-sm.opacity-50', `Showing 0 of ${jobs.list.length}`)
                ]),
                m(FilterBar),
                m('.text-center.py-10.opacity-50', 'No jobs match the current filters')
            ]);
        }

        return m('section.mb-6', [
            m('.flex.items-center.justify-between.mb-4', [
                m('h2.text-lg.font-semibold.opacity-70', 'Sync Jobs'),
                hasActiveFilters && m('span.text-sm.opacity-50', `Showing ${sortedJobs.length} of ${jobs.list.length}`)
            ]),

            m(FilterBar),

            // Mobile card view
            m('.sm:hidden.space-y-3', sortedJobs.map(job =>
                m(JobCard, { key: job.id, job })
            )),

            // Desktop table view
            m('.hidden.sm:block.overflow-x-auto', [
                m('table.table.w-full.bg-base-200.rounded-lg.border.border-base-300', [
                    m('thead', [
                        m('tr.bg-base-300', [
                            m('th', {
                                class: headerClass,
                                onclick: () => setJobSort('name')
                            }, `Name${sortIcon('name')}`),
                            m('th.hidden.xl:table-cell', {
                                class: headerClass,
                                onclick: () => setJobSort('downloader')
                            }, `Downloader${sortIcon('downloader')}`),
                            m('th.hidden.lg:table-cell', {
                                class: headerClass,
                                onclick: () => setJobSort('category')
                            }, `Category${sortIcon('category')}`),
                            m('th.text-center.hidden.md:table-cell', {
                                class: headerClass,
                                onclick: () => setJobSort('files')
                            }, `Files${sortIcon('files')}`),
                            m('th', {
                                class: headerClass,
                                onclick: () => setJobSort('status')
                            }, `Status${sortIcon('status')}`),
                            m('th', {
                                class: headerClass,
                                onclick: () => setJobSort('progress')
                            }, `Progress${sortIcon('progress')}`),
                            m('th.text-right.hidden.md:table-cell', {
                                class: headerClass,
                                onclick: () => setJobSort('speed')
                            }, `Speed${sortIcon('speed')}`),
                            m('th.hidden.lg:table-cell', {
                                class: headerClass,
                                onclick: () => setJobSort('size')
                            }, `Size${sortIcon('size')}`)
                        ])
                    ]),
                    m('tbody', sortedJobs.map(job =>
                        m(JobTableRow, { key: job.id, job })
                    ))
                ])
            ]),

            // Job detail modal
            m(JobModal)
        ]);
    }
};

export default JobsList;
