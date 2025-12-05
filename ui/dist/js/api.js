// API module - all server communication

import m from 'mithril';
import { connection, stats, jobs, appsList, downloadersList, timeline } from './state.js';
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

// Fetch all downloads
export async function fetchJobs() {
    try {
        const newJobs = await m.request({ url: '/api/downloads' });

        // Preserve files data from cached downloads
        for (const job of newJobs) {
            const cached = jobs.list.find(j => j.id === job.id && j.downloader === job.downloader);
            if (cached && cached.files) {
                job.files = cached.files;
            }
        }

        // Fetch files for syncing downloads that don't have them yet
        for (const job of newJobs) {
            if (job.status === 'syncing' && !job.files) {
                try {
                    const detail = await m.request({ url: `/api/downloaders/${job.downloader}/downloads/${job.id}` });
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
export async function fetchJobDetails(jobId, downloader) {
    try {
        // If downloader is not provided, try to find it from the jobs list
        if (!downloader) {
            const job = jobs.list.find(j => j.id === jobId);
            downloader = job?.downloader;
        }
        if (!downloader) {
            console.error('No downloader found for job:', jobId);
            return null;
        }
        const detail = await m.request({ url: `/api/downloaders/${downloader}/downloads/${jobId}` });
        const job = jobs.list.find(j => j.id === jobId && j.downloader === downloader);
        if (job) {
            job.files = detail.files || [];
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
            await fetchJobDetails(job.id, job.downloader);
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

// Fetch timeline events
export async function fetchTimeline() {
    timeline.loading = true;
    try {
        const events = await m.request({ url: '/api/timeline' });
        timeline.events = events;
    } catch (e) {
        console.error('Failed to fetch timeline:', e);
    } finally {
        timeline.loading = false;
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
