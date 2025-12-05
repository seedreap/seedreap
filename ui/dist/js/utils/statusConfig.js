// Shared status configuration for jobs and files

// Job status configuration
export const jobStatusConfig = {
    downloading: { label: 'Downloading', badgeClass: 'badge-info', progressClass: 'progress-info', tooltip: 'Downloading on seedbox' },
    paused: { label: 'Paused', badgeClass: 'badge-warning', progressClass: 'progress-warning', tooltip: 'Paused on seedbox' },
    discovered: { label: 'Discovered', badgeClass: 'badge-accent', progressClass: 'progress-primary', tooltip: 'Discovered, waiting to sync' },
    pending: { label: 'Pending', badgeClass: 'badge-ghost', progressClass: 'progress-primary', tooltip: 'Pending sync' },
    syncing: { label: 'Syncing', badgeClass: 'badge-warning', progressClass: 'progress-warning', tooltip: 'Syncing files from seedbox' },
    importing: { label: 'Importing', badgeClass: 'badge-secondary', progressClass: 'progress-primary', tooltip: 'Triggering import in app' },
    complete: { label: 'Complete', badgeClass: 'badge-success', progressClass: 'progress-success', tooltip: 'Fully synced and imported' },
    skipped: { label: 'Skipped', badgeClass: 'badge-ghost', progressClass: 'progress-primary', tooltip: 'Skipped' },
    error: { label: 'Error', badgeClass: 'badge-error', progressClass: 'progress-error', tooltip: 'Error occurred' }
};

// File status configuration
export const fileStatusConfig = {
    pending: { label: 'Pending', badgeClass: 'badge-ghost', progressClass: 'progress-primary' },
    syncing: { label: 'Syncing', badgeClass: 'badge-warning', progressClass: 'progress-info' },
    complete: { label: 'Complete', badgeClass: 'badge-success', progressClass: 'progress-success' },
    skipped: { label: 'Skipped', badgeClass: 'badge-ghost', progressClass: 'progress-primary' },
    error: { label: 'Error', badgeClass: 'badge-error', progressClass: 'progress-error' }
};

// Helper functions
export function getJobStatusLabel(status) {
    return jobStatusConfig[status]?.label || status;
}

export function getJobStatusBadgeClass(status) {
    return jobStatusConfig[status]?.badgeClass || 'badge-ghost';
}

export function getJobStatusProgressClass(status) {
    return jobStatusConfig[status]?.progressClass || 'progress-primary';
}

export function getJobStatusTooltip(status) {
    return jobStatusConfig[status]?.tooltip || status;
}

export function getFileStatusBadgeClass(status) {
    return fileStatusConfig[status]?.badgeClass || 'badge-ghost';
}

export function getFileStatusProgressClass(status) {
    return fileStatusConfig[status]?.progressClass || 'progress-primary';
}

// List of all job statuses for filter dropdowns
export const jobStatusValues = Object.keys(jobStatusConfig);
