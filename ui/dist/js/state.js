// Application state management

import { THEMES, DEFAULT_THEME_PREFERENCE, STORAGE_KEYS } from './config.js';

// Theme state
// preference: 'system' | 'light' | 'dark'
// resolved: actual DaisyUI theme name applied
export const theme = {
    preference: localStorage.getItem(STORAGE_KEYS.themePreference) || DEFAULT_THEME_PREFERENCE,
    resolved: THEMES.dark, // Will be set by initTheme()
};

// Get the system's color scheme preference
function getSystemTheme() {
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

// Resolve preference to actual theme name
function resolveTheme(preference) {
    const mode = preference === 'system' ? getSystemTheme() : preference;
    return mode === 'dark' ? THEMES.dark : THEMES.light;
}

// Apply theme to document
function applyTheme(themeName) {
    document.documentElement.setAttribute('data-theme', themeName);
    theme.resolved = themeName;
}

// Initialize theme system
export function initTheme() {
    // Apply initial theme
    const resolved = resolveTheme(theme.preference);
    applyTheme(resolved);

    // Listen for system theme changes
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
        if (theme.preference === 'system') {
            applyTheme(resolveTheme('system'));
        }
    });
}

// Set theme preference
export function setThemePreference(preference) {
    theme.preference = preference;
    localStorage.setItem(STORAGE_KEYS.themePreference, preference);
    applyTheme(resolveTheme(preference));
}

// Connection status
export const connection = {
    status: 'connecting', // 'connecting', 'connected', 'disconnected'
};

// Jobs data
export const jobs = {
    list: [],
    sort: {
        column: 'name',
        direction: 'asc'
    },
    fileSorts: {}, // Per-job file sort state: { jobId: { column, direction } }
};

// Stats data
export const stats = {
    total: 0,
    downloading: 0,
    syncing: 0,
    complete: 0,
    error: 0,
};

// Apps, downloaders, and events (from API)
export const appsList = [];
export const downloadersList = [];
export const eventsList = {
    items: [],
    loading: false,
    filter: '', // Filter by event type, app, downloader, or download
};

// Filter state
export const filters = {
    search: '',
    status: new Set(JSON.parse(localStorage.getItem(STORAGE_KEYS.filterStatus) || '[]')),
    apps: new Set(JSON.parse(localStorage.getItem(STORAGE_KEYS.filterApps) || '[]')),
    categories: new Set(JSON.parse(localStorage.getItem(STORAGE_KEYS.filterCategories) || '[]')),
    downloaders: new Set(JSON.parse(localStorage.getItem(STORAGE_KEYS.filterDownloaders) || '[]')),
};

// Status order for sorting (lower = earlier in workflow)
export const jobStatusOrder = {
    discovered: 0,
    paused: 1,
    downloading: 2,
    pending: 3,
    syncing: 4,
    synced: 5,
    move_pending: 6,
    moving: 7,
    moved: 8,
    importing: 9,
    imported: 10,
    complete: 11,
    skipped: 12,
    cancelled: 13,
    sync_error: 14,
    move_error: 15,
    import_error: 16,
    error: 17
};

export const fileStatusOrder = {
    pending: 0,
    syncing: 1,
    complete: 2,
    skipped: 3,
    error: 4
};

// Filter persistence
export function saveFilters() {
    localStorage.setItem(STORAGE_KEYS.filterStatus, JSON.stringify([...filters.status]));
    localStorage.setItem(STORAGE_KEYS.filterApps, JSON.stringify([...filters.apps]));
    localStorage.setItem(STORAGE_KEYS.filterCategories, JSON.stringify([...filters.categories]));
    localStorage.setItem(STORAGE_KEYS.filterDownloaders, JSON.stringify([...filters.downloaders]));
}

// Clear all filters
export function clearAllFilters() {
    filters.search = '';
    filters.status.clear();
    filters.apps.clear();
    filters.categories.clear();
    filters.downloaders.clear();
    saveFilters();
}

// Get active filter count
export function getActiveFilterCount() {
    return (filters.search ? 1 : 0) +
        filters.status.size +
        filters.apps.size +
        filters.categories.size +
        filters.downloaders.size;
}

export function setJobSort(column) {
    if (jobs.sort.column === column) {
        jobs.sort.direction = jobs.sort.direction === 'asc' ? 'desc' : 'asc';
    } else {
        jobs.sort.column = column;
        jobs.sort.direction = 'asc';
    }
}

export function setFileSort(jobId, column) {
    if (!jobs.fileSorts[jobId]) {
        jobs.fileSorts[jobId] = { column: 'name', direction: 'asc' };
    }
    const fileSort = jobs.fileSorts[jobId];
    if (fileSort.column === column) {
        fileSort.direction = fileSort.direction === 'asc' ? 'desc' : 'asc';
    } else {
        fileSort.column = column;
        fileSort.direction = 'asc';
    }
}

// Get display state - combines sync state with seedbox state for better UX
export function getDisplayState(job) {
    if (job.seedbox_state === 'downloading' && job.seedbox_progress < 1.0) {
        return 'downloading';
    }
    if (job.seedbox_state === 'paused' && job.seedbox_progress < 1.0) {
        return 'paused';
    }
    return job.status;
}

// Get the appropriate progress for a job (seedbox download vs sync progress)
export function getJobProgress(job) {
    const isDownloadingOnSeedbox = job.seedbox_state === 'downloading' || job.seedbox_progress < 1.0;
    if (isDownloadingOnSeedbox && job.status !== 'syncing') {
        return { progress: job.seedbox_progress * 100, type: 'seedbox' };
    }
    const syncProgress = job.total_size > 0 ? (job.completed_size / job.total_size * 100) : 0;
    return { progress: syncProgress, type: 'sync' };
}

// Sort jobs
export function getSortedJobs(jobList) {
    const { column, direction } = jobs.sort;
    return [...jobList].sort((a, b) => {
        let aVal, bVal;
        switch (column) {
            case 'name':
                aVal = a.name.toLowerCase();
                bVal = b.name.toLowerCase();
                break;
            case 'category':
                aVal = (a.category || '').toLowerCase();
                bVal = (b.category || '').toLowerCase();
                break;
            case 'downloader':
                aVal = (a.downloader || '').toLowerCase();
                bVal = (b.downloader || '').toLowerCase();
                break;
            case 'files':
                aVal = a.total_files;
                bVal = b.total_files;
                break;
            case 'status':
                aVal = jobStatusOrder[getDisplayState(a)] ?? 99;
                bVal = jobStatusOrder[getDisplayState(b)] ?? 99;
                break;
            case 'progress':
                aVal = getJobProgress(a).progress;
                bVal = getJobProgress(b).progress;
                break;
            case 'speed':
                aVal = a.bytes_per_sec || 0;
                bVal = b.bytes_per_sec || 0;
                break;
            case 'size':
                aVal = a.total_size;
                bVal = b.total_size;
                break;
            default:
                return 0;
        }
        if (aVal < bVal) return direction === 'asc' ? -1 : 1;
        if (aVal > bVal) return direction === 'asc' ? 1 : -1;
        return 0;
    });
}

// Sort files within a job
export function getSortedFiles(jobId, files) {
    const fileSort = jobs.fileSorts[jobId] || { column: 'name', direction: 'asc' };
    const { column, direction } = fileSort;
    return [...files].sort((a, b) => {
        let aVal, bVal;
        switch (column) {
            case 'name':
                aVal = a.path.toLowerCase();
                bVal = b.path.toLowerCase();
                break;
            case 'status':
                aVal = fileStatusOrder[a.status] ?? 99;
                bVal = fileStatusOrder[b.status] ?? 99;
                break;
            case 'progress':
                aVal = a.size > 0 ? a.transferred / a.size : 0;
                bVal = b.size > 0 ? b.transferred / b.size : 0;
                break;
            case 'speed':
                aVal = a.bytes_per_sec || 0;
                bVal = b.bytes_per_sec || 0;
                break;
            case 'size':
                aVal = a.size;
                bVal = b.size;
                break;
            default:
                return 0;
        }
        if (aVal < bVal) return direction === 'asc' ? -1 : 1;
        if (aVal > bVal) return direction === 'asc' ? 1 : -1;
        return 0;
    });
}

// Filter jobs based on active filters
// Empty set = no filter (show all), items in set = items to show
export function getFilteredJobs(jobList) {
    return jobList.filter(job => {
        // Search filter
        if (filters.search) {
            const search = filters.search.toLowerCase();
            const name = (job.name || '').toLowerCase();
            const category = (job.category || '').toLowerCase();
            const app = (job.app || '').toLowerCase();
            const downloader = (job.downloader || '').toLowerCase();
            if (!name.includes(search) &&
                !category.includes(search) &&
                !app.includes(search) &&
                !downloader.includes(search)) {
                return false;
            }
        }
        // Status filter - if set has items, only show those statuses
        if (filters.status.size > 0) {
            const displayState = getDisplayState(job);
            if (!filters.status.has(displayState)) return false;
        }
        // App filter
        if (filters.apps.size > 0) {
            const app = job.app || 'Unknown';
            if (!filters.apps.has(app)) return false;
        }
        // Category filter
        if (filters.categories.size > 0) {
            const category = job.category || 'Unknown';
            if (!filters.categories.has(category)) return false;
        }
        // Downloader filter
        if (filters.downloaders.size > 0) {
            const downloader = job.downloader || 'Unknown';
            if (!filters.downloaders.has(downloader)) return false;
        }
        return true;
    });
}
