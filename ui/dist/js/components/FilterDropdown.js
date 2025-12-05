// FilterDropdown - reusable filter dropdown component

import m from 'mithril';

// Toggle a value in a filter set
export function toggleFilter(filterSet, value, onFilterChange) {
    if (filterSet.has(value)) {
        filterSet.delete(value);
    } else {
        filterSet.add(value);
    }
    if (onFilterChange) onFilterChange();
}

// Generic filter dropdown component
// Props:
//   label: string - dropdown button label
//   values: array - list of values to filter by
//   filterSet: Set - the set tracking selected values
//   getLabel: (value) => string - optional function to get display label
//   getBadgeClass: (value) => string - optional function to get badge class for styling
//   onFilterChange: () => void - optional callback when filter changes (for persistence)
const FilterDropdown = {
    view: (vnode) => {
        const { label, values, filterSet, getLabel, getBadgeClass, onFilterChange } = vnode.attrs;
        const hasFilters = filterSet.size > 0;

        if (values.length === 0) {
            return m('span.text-xs.text-base-content/50', label);
        }

        // Using details/summary with backdrop for mobile close support
        return m('details.dropdown', {
            oncreate: (vnode) => {
                // Close dropdown when clicking outside
                vnode.state.closeHandler = (e) => {
                    if (vnode.dom.open && !vnode.dom.contains(e.target)) {
                        vnode.dom.open = false;
                    }
                };
                document.addEventListener('click', vnode.state.closeHandler);
            },
            onremove: (vnode) => {
                document.removeEventListener('click', vnode.state.closeHandler);
            }
        }, [
            // Dropdown button
            m('summary.btn.btn-xs.btn-ghost.gap-1', [
                label,
                hasFilters && m('span.badge.badge-xs.badge-primary', filterSet.size),
                m('svg.w-3.h-3', {
                    xmlns: 'http://www.w3.org/2000/svg',
                    viewBox: '0 0 24 24',
                    fill: 'none',
                    stroke: 'currentColor',
                    'stroke-width': '2'
                }, m('path', { d: 'M6 9l6 6 6-6' }))
            ]),
            // Dropdown content - flex-col ensures single column layout
            m('ul.dropdown-content.bg-base-100.rounded-box.z-50.w-56.p-2.shadow-xl.border.border-base-300.max-h-64.overflow-y-auto.flex.flex-col.gap-1', [
                // Clear button when filters are active
                hasFilters && m('li', [
                    m('button.btn.btn-xs.btn-block.btn-ghost', {
                        onclick: (e) => {
                            e.stopPropagation();
                            filterSet.clear();
                            if (onFilterChange) onFilterChange();
                        }
                    }, 'Clear filter')
                ]),
                // Value checkboxes - checked = in the set = will be shown
                values.map(value => {
                    const displayLabel = getLabel ? getLabel(value) : value;
                    const badgeClass = getBadgeClass ? getBadgeClass(value) : null;

                    return m('li', { key: value }, [
                        m('label.flex.items-center.gap-2.cursor-pointer.py-1.px-2.rounded.hover:bg-base-300', [
                            m('input.checkbox.checkbox-xs', {
                                type: 'checkbox',
                                checked: filterSet.has(value),
                                onchange: () => toggleFilter(filterSet, value, onFilterChange)
                            }),
                            badgeClass
                                ? m('span.badge.badge-sm.whitespace-nowrap', { class: badgeClass }, displayLabel)
                                : m('span.text-sm', displayLabel)
                        ])
                    ]);
                })
            ])
        ]);
    }
};

export default FilterDropdown;
