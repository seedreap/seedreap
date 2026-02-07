// Shared event type configuration for timeline
// Event types match the backend events package (internal/events/bus.go)

export const eventTypeConfig = {
    // System events
    'system.started': { label: 'System Started', badgeClass: 'badge-info' },

    // Connection events
    'downloader.connected': { label: 'Downloader Connected', badgeClass: 'badge-success' },
    'app.connected': { label: 'App Connected', badgeClass: 'badge-success' },

    // Download lifecycle events
    'download.discovered': { label: 'Discovered', badgeClass: 'badge-primary' },
    'download.updated': { label: 'Updated', badgeClass: 'badge-ghost' },
    'download.paused': { label: 'Paused', badgeClass: 'badge-warning' },
    'download.resumed': { label: 'Resumed', badgeClass: 'badge-info' },
    'download.removed': { label: 'Removed', badgeClass: 'badge-warning' },
    'download.complete': { label: 'Download Complete', badgeClass: 'badge-success' },
    'download.error': { label: 'Download Error', badgeClass: 'badge-error' },
    'category.changed': { label: 'Category Changed', badgeClass: 'badge-warning' },

    // File events
    'file.completed': { label: 'File Completed', badgeClass: 'badge-success' },

    // Sync events
    'sync.job.created': { label: 'Sync Job Created', badgeClass: 'badge-ghost' },
    'sync.file.created': { label: 'Sync File Created', badgeClass: 'badge-ghost' },
    'sync.started': { label: 'Sync Started', badgeClass: 'badge-info' },
    'sync.file.started': { label: 'File Sync Started', badgeClass: 'badge-info' },
    'sync.file.complete': { label: 'File Synced', badgeClass: 'badge-success' },
    'sync.complete': { label: 'Sync Complete', badgeClass: 'badge-success' },
    'sync.failed': { label: 'Sync Failed', badgeClass: 'badge-error' },
    'sync.cancelled': { label: 'Sync Cancelled', badgeClass: 'badge-warning' },

    // Move events
    'move.started': { label: 'Move Started', badgeClass: 'badge-info' },
    'move.complete': { label: 'Move Complete', badgeClass: 'badge-success' },
    'move.failed': { label: 'Move Failed', badgeClass: 'badge-error' },

    // App notification events
    'app.notify.started': { label: 'Import Started', badgeClass: 'badge-info' },
    'app.notify.complete': { label: 'Import Complete', badgeClass: 'badge-success' },
    'app.notify.failed': { label: 'Import Failed', badgeClass: 'badge-error' },

    // Deprecated import events (kept for backwards compatibility)
    'import.started': { label: 'Import Started', badgeClass: 'badge-info' },
    'import.complete': { label: 'Import Complete', badgeClass: 'badge-success' },
    'import.failed': { label: 'Import Failed', badgeClass: 'badge-error' },

    // Cleanup
    'cleanup': { label: 'Cleanup', badgeClass: 'badge-ghost' }
};

// Helper functions
export function getEventLabel(type) {
    return eventTypeConfig[type]?.label || formatEventType(type);
}

export function getEventBadgeClass(type) {
    return eventTypeConfig[type]?.badgeClass || 'badge-ghost';
}

export function getEventInfo(type) {
    const config = eventTypeConfig[type];
    return config
        ? { label: config.label, color: config.badgeClass }
        : { label: formatEventType(type), color: 'badge-ghost' };
}

// Format unknown event types to be more readable
// e.g., "some.unknown.event" -> "Some Unknown Event"
function formatEventType(type) {
    if (!type) return 'Unknown';
    return type
        .split('.')
        .map(word => word.charAt(0).toUpperCase() + word.slice(1))
        .join(' ');
}
