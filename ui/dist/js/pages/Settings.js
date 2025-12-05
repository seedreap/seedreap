// Settings page - user preferences

import m from 'mithril';
import { getSpeedUnit, setSpeedUnit } from '../utils/format.js';
import { theme, setThemePreference } from '../state.js';

// Section header component
const SectionHeader = {
    view: (vnode) => {
        const { title } = vnode.attrs;
        return m('h2.text-lg.font-semibold.mb-4', title);
    }
};

// Main Settings page
const Settings = {
    view: () => {
        const currentUnit = getSpeedUnit();

        return m('.flex-1.min-h-0.overflow-auto', [
            // Page header
            m('.px-4.sm:px-6.lg:px-8.py-4.border-b.border-base-300', [
                m('h1.text-xl.font-semibold', 'Settings')
            ]),

            // Content
            m('.px-4.sm:px-6.lg:px-8.py-6', [
                // Note about local storage
                m('.text-sm.text-base-content/50.mb-6',
                    'These settings are saved in your browser and only apply to this device.'
                ),

                // Display section
                m('section.mb-8', [
                    m(SectionHeader, { title: 'Display' }),

                    m('.card.bg-base-200.border.border-base-300', [
                        m('.card-body.p-4.space-y-4', [
                            // Theme setting
                            m('.flex.items-center.justify-between', [
                                m('div', [
                                    m('h3.font-medium', 'Theme'),
                                    m('p.text-sm.text-base-content/70', 'Choose your preferred color scheme')
                                ]),
                                m('select.select.select-bordered.select-sm.w-32', {
                                    value: theme.preference,
                                    onchange: (e) => {
                                        setThemePreference(e.target.value);
                                    }
                                }, [
                                    m('option', { value: 'system' }, 'System'),
                                    m('option', { value: 'light' }, 'Light'),
                                    m('option', { value: 'dark' }, 'Dark')
                                ])
                            ]),

                            // Speed unit setting
                            m('.flex.items-center.justify-between', [
                                m('div', [
                                    m('h3.font-medium', 'Speed Unit'),
                                    m('p.text-sm.text-base-content/70', 'Choose how transfer speeds are displayed')
                                ]),
                                m('select.select.select-bordered.select-sm.w-32', {
                                    value: currentUnit,
                                    onchange: (e) => {
                                        setSpeedUnit(e.target.value);
                                    }
                                }, [
                                    m('option', { value: 'bytes' }, 'MB/s'),
                                    m('option', { value: 'bits' }, 'Mbps')
                                ])
                            ])
                        ])
                    ])
                ])
            ])
        ]);
    }
};

export default Settings;
