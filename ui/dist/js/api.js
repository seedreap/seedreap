// API module - all server communication

import m from 'mithril';
import { connection, stats, jobs, appsList, downloadersList, eventsList } from './state.js';
import { setHistory, addPoint } from './utils/sparkline.js';

// Fetch stats from server
export async function fetchStats() {
    try {
        const data = await m.request({ url: '/api/stats' });
        stats.total = data.total_tracked || 0;
        stats.downloading = data.downloading_on_seedbox || 0;
        stats.syncing = data.by_state?.syncing || 0;
        stats.complete = data.by_state?.complete || 0;
        stats.error = data.by_state?.error || 0;
        connection.status = 'connected';
    } catch (e) {
        console.error('Failed to fetch stats:', e);
        connection.status = 'disconnected';
    }
}

// Normalize job data from API to match UI expectations
function normalizeJob(job) {
    // Map high-level state
    job.status = job.state;

    // Use total_size from API (was previously called size)
    job.total_size = job.total_size || 0;
    job.total_files = job.total_files || 0;
    job.completed_size = job.completed_size || 0;
    job.bytes_per_sec = job.bytes_per_sec || 0;

    // Extract seedbox state from download_job
    if (job.download_job) {
        job.seedbox_state = job.download_job.status;
        job.seedbox_progress = job.download_job.progress || 0;
        job.seedbox_downloaded = job.download_job.downloaded || 0;
        job.seedbox_speed = job.download_job.download_speed || 0;
    }

    // Normalize files to use consistent field names
    if (job.files) {
        for (const file of job.files) {
            // Map sync_status to status for backward compatibility
            file.status = file.sync_status || 'pending';
            // Map synced_size to transferred for backward compatibility
            file.transferred = file.synced_size || 0;
        }
    }

    return job;
}

// Fetch all downloads
export async function fetchJobs() {
    try {
        const newJobs = await m.request({ url: '/api/downloads' });

        // Normalize and preserve files data from cached downloads
        for (const job of newJobs) {
            normalizeJob(job);
            // IDs are ULIDs (globally unique), no need to also match downloader
            const cached = jobs.list.find(j => j.id === job.id);
            if (cached && cached.files) {
                job.files = cached.files;
            }
        }

        // Fetch files for syncing downloads that don't have them yet
        for (const job of newJobs) {
            if (job.status === 'syncing' && !job.files) {
                try {
                    const detail = await m.request({ url: `/api/downloads/${job.id}` });
                    job.files = detail.files || [];
                } catch (e) {
                    console.error('Failed to fetch download details:', e);
                }
            }
        }

        jobs.list = newJobs;
    } catch (e) {
        console.error('Failed to fetch downloads:', e);
    }
}

// Fetch download details (files)
export async function fetchJobDetails(jobId) {
    try {
        const detail = await m.request({ url: `/api/downloads/${jobId}` });
        normalizeJob(detail);
        const job = jobs.list.find(j => j.id === jobId);
        if (job) {
            job.files = detail.files || [];
            // Update job with latest data from detail
            job.total_size = detail.total_size;
            job.completed_size = detail.completed_size;
            job.bytes_per_sec = detail.bytes_per_sec;
            job.total_files = detail.total_files || (detail.files ? detail.files.length : 0);
        }
        return detail;
    } catch (e) {
        console.error('Failed to fetch download details:', e);
        return null;
    }
}

// Refresh files for syncing downloads
export async function refreshSyncingJobFiles() {
    for (const job of jobs.list) {
        if (job.status === 'syncing') {
            await fetchJobDetails(job.id);
        }
    }
}

// Fetch speed history for sparkline
export async function fetchSpeedHistory() {
    try {
        const history = await m.request({ url: '/api/speed-history' });
        setHistory(history.map(h => h.speed));
    } catch (e) {
        console.error('Failed to fetch speed history:', e);
    }
}

// Fetch apps list
export async function fetchApps() {
    try {
        const apps = await m.request({ url: '/api/apps' });
        appsList.length = 0;
        appsList.push(...apps);
    } catch (e) {
        console.error('Failed to fetch apps:', e);
    }
}

// Fetch downloaders list
export async function fetchDownloaders() {
    try {
        const downloaders = await m.request({ url: '/api/downloaders' });
        downloadersList.length = 0;
        downloadersList.push(...downloaders);
    } catch (e) {
        console.error('Failed to fetch downloaders:', e);
    }
}

// Normalize event data from API
function normalizeEvent(event) {
    // Parse details JSON string into object
    if (event.details && typeof event.details === 'string') {
        try {
            event.details = JSON.parse(event.details);
        } catch {
            event.details = null;
        }
    }
    return event;
}

// Fetch events
export async function fetchEvents() {
    eventsList.loading = true;
    try {
        const events = await m.request({ url: '/api/events' });
        eventsList.items = events.map(normalizeEvent);
    } catch (e) {
        console.error('Failed to fetch events:', e);
    } finally {
        eventsList.loading = false;
    }
}

// Main refresh function
export async function refresh() {
    await fetchStats();
    await refreshSyncingJobFiles();
    await fetchJobs();

    // Update sparkline with current total speed
    let totalSpeed = 0;
    for (const job of jobs.list) {
        if (job.status === 'syncing') {
            totalSpeed += job.bytes_per_sec || 0;
        }
    }
    addPoint(totalSpeed);
}

// Initial fetch (includes apps/downloaders which don't change often)
export async function initialFetch() {
    await fetchApps();
    await fetchDownloaders();
    await fetchSpeedHistory();
    await refresh();
}

// Calculate transfer status from jobs
export function calculateTransferStatus() {
    let totalFiles = 0;
    let completedFiles = 0;
    let syncingFiles = 0;
    let syncingJobs = 0;
    let totalSpeed = 0;
    let downloadingOnSeedboxCount = 0;
    let pausedOnSeedboxCount = 0;
    let syncTransferred = 0;
    let syncTotalSize = 0;

    for (const job of jobs.list) {
        totalFiles += job.total_files || 0;

        const isDownloadingOnSeedbox = job.seedbox_state === 'downloading' && job.seedbox_progress < 1.0;
        const isPausedOnSeedbox = job.seedbox_state === 'paused' && job.seedbox_progress < 1.0;

        if (isDownloadingOnSeedbox) downloadingOnSeedboxCount++;
        if (isPausedOnSeedbox) pausedOnSeedboxCount++;

        if (job.status === 'syncing') {
            syncingJobs++;
            totalSpeed += job.bytes_per_sec || 0;
            syncTotalSize += job.total_size || 0;
            syncTransferred += job.completed_size || 0;
        } else if (job.status === 'complete' || job.status === 'importing') {
            syncTotalSize += job.total_size || 0;
            syncTransferred += job.completed_size || 0;
        } else if (!isDownloadingOnSeedbox && !isPausedOnSeedbox && job.status !== 'discovered') {
            syncTotalSize += job.total_size || 0;
        }

        if (job.files && job.files.length > 0) {
            for (const file of job.files) {
                if (file.status === 'complete' || file.status === 'skipped') {
                    completedFiles++;
                } else if (file.status === 'syncing') {
                    syncingFiles++;
                }
            }
        } else if (job.status === 'complete') {
            completedFiles += job.total_files || 0;
        }
    }

    const remainingBytes = syncTotalSize - syncTransferred;
    const etaSeconds = totalSpeed > 0 ? remainingBytes / totalSpeed : 0;

    return {
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
    };
}
