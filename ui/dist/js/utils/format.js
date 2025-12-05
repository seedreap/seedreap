// Formatting utility functions

// Speed unit preference: 'bytes' (MB/s) or 'bits' (Mbps)
let speedUnit = localStorage.getItem('speedUnit') || 'bytes';

export function getSpeedUnit() {
    return speedUnit;
}

export function setSpeedUnit(unit) {
    speedUnit = unit;
    localStorage.setItem('speedUnit', unit);
}

export function toggleSpeedUnit() {
    speedUnit = speedUnit === 'bytes' ? 'bits' : 'bytes';
    localStorage.setItem('speedUnit', speedUnit);
    return speedUnit;
}

export function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

export function formatSpeed(bytesPerSec) {
    if (!bytesPerSec || bytesPerSec === 0) return '-';
    if (speedUnit === 'bits') {
        // Convert bytes to bits (multiply by 8), use 1000-based units for bits
        const bitsPerSec = bytesPerSec * 8;
        const k = 1000;
        const sizes = ['bps', 'Kbps', 'Mbps', 'Gbps'];
        const i = Math.floor(Math.log(bitsPerSec) / Math.log(k));
        return parseFloat((bitsPerSec / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    }
    return formatBytes(bytesPerSec) + '/s';
}

export function formatDuration(seconds) {
    if (!seconds || seconds <= 0 || !isFinite(seconds)) return '--';
    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const secs = Math.floor(seconds % 60);
    if (hours > 24) {
        const days = Math.floor(hours / 24);
        return `${days}d ${hours % 24}h`;
    } else if (hours > 0) {
        return `${hours}h ${minutes}m`;
    } else if (minutes > 0) {
        return `${minutes}m ${secs}s`;
    }
    return `${secs}s`;
}

export function formatRelativeTime(timestamp) {
    const date = new Date(timestamp);
    const now = new Date();
    const diffMs = now - date;
    const diffSec = Math.floor(diffMs / 1000);
    const diffMin = Math.floor(diffSec / 60);
    const diffHour = Math.floor(diffMin / 60);
    const diffDay = Math.floor(diffHour / 24);

    if (diffSec < 60) return 'just now';
    if (diffMin < 60) return `${diffMin}m ago`;
    if (diffHour < 24) return `${diffHour}h ago`;
    if (diffDay < 7) return `${diffDay}d ago`;

    // For older events, show the date
    return date.toLocaleDateString();
}

export function formatTimestamp(timestamp) {
    const date = new Date(timestamp);
    return date.toLocaleString();
}
