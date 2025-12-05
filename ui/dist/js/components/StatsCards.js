// StatsCards component - displays stat summary cards

import m from 'mithril';
import { stats } from '../state.js';

const StatCard = {
    view: (vnode) => {
        const { title, value, colorClass } = vnode.attrs;
        return m('.card.bg-base-200.border.border-base-300', [
            m('.card-body.p-4', [
                m('h3.text-xs.opacity-50.uppercase.tracking-wide.mb-2', title),
                m('.text-2xl.sm:text-3xl.font-semibold', {
                    class: colorClass || ''
                }, value !== undefined ? value : '-')
            ])
        ]);
    }
};

const StatsCards = {
    view: () => {
        return m('.grid.grid-cols-2.lg:grid-cols-5.gap-4.mb-6', [
            m(StatCard, {
                title: 'Total Tracked',
                value: stats.total
            }),
            m(StatCard, {
                title: 'Downloading',
                value: stats.downloading,
                colorClass: 'text-info'
            }),
            m(StatCard, {
                title: 'Syncing',
                value: stats.syncing,
                colorClass: 'text-warning'
            }),
            m(StatCard, {
                title: 'Complete',
                value: stats.complete,
                colorClass: 'text-success'
            }),
            m(StatCard, {
                title: 'Errors',
                value: stats.error,
                colorClass: 'text-error'
            })
        ]);
    }
};

export default StatsCards;
