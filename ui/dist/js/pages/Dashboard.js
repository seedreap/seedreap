// Dashboard page - main sync jobs view

import m from 'mithril';
import StatsCards from '../components/StatsCards.js';
import JobsList from '../components/JobsList.js';

const Dashboard = {
    view: () => {
        return m('.flex-1.min-h-0.overflow-auto', [
            // Page header
            m('.px-4.sm:px-6.lg:px-8.py-4.border-b.border-base-300', [
                m('h1.text-xl.font-semibold', 'Dashboard')
            ]),

            // Dashboard content
            m('.px-4.sm:px-6.lg:px-8.py-4.sm:py-6', [
                m(StatsCards),
                m(JobsList)
            ])
        ]);
    }
};

export default Dashboard;
