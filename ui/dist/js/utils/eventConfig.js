// Shared event type configuration for timeline

export const eventTypeConfig = {
    system_started: { label: 'System Started', badgeClass: 'badge-info' },
    downloader_connected: { label: 'Downloader Connected', badgeClass: 'badge-success' },
    app_connected: { label: 'App Connected', badgeClass: 'badge-success' },
    discovered: { label: 'Discovered', badgeClass: 'badge-primary' },
    sync_started: { label: 'Sync Started', badgeClass: 'badge-info' },
    sync_progress: { label: 'Sync Progress', badgeClass: 'badge-info' },
    sync_complete: { label: 'Sync Complete', badgeClass: 'badge-success' },
    sync_cancelled: { label: 'Sync Cancelled', badgeClass: 'badge-warning' },
    moving_started: { label: 'Moving Started', badgeClass: 'badge-info' },
    move_complete: { label: 'Move Complete', badgeClass: 'badge-success' },
    import_started: { label: 'Import Started', badgeClass: 'badge-info' },
    import_complete: { label: 'Import Complete', badgeClass: 'badge-success' },
    import_failed: { label: 'Import Failed', badgeClass: 'badge-error' },
    category_changed: { label: 'Category Changed', badgeClass: 'badge-warning' },
    removed: { label: 'Removed', badgeClass: 'badge-warning' },
    error: { label: 'Error', badgeClass: 'badge-error' },
    complete: { label: 'Complete', badgeClass: 'badge-success' },
    cleanup: { label: 'Cleanup', badgeClass: 'badge-ghost' }
};

// Helper functions
export function getEventLabel(type) {
    return eventTypeConfig[type]?.label || type;
}

export function getEventBadgeClass(type) {
    return eventTypeConfig[type]?.badgeClass || 'badge-ghost';
}

export function getEventInfo(type) {
    const config = eventTypeConfig[type];
    return config
        ? { label: config.label, color: config.badgeClass }
        : { label: type, color: 'badge-ghost' };
}
