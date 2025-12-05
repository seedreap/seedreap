// Icon utilities - uses selfh.st icon CDN for known apps

const SELFHST_CDN = 'https://cdn.jsdelivr.net/gh/selfhst/icons@main/svg';

// Map of known app/downloader types to their icon names
const iconMap = {
    // Apps
    'sonarr': 'sonarr',
    'radarr': 'radarr',
    'lidarr': 'lidarr',
    'readarr': 'readarr',
    'prowlarr': 'prowlarr',
    'bazarr': 'bazarr',
    'whisparr': 'whisparr',

    // Downloaders
    'qbittorrent': 'qbittorrent',
    'deluge': 'deluge',
    'transmission': 'transmission',
    'rtorrent': 'rtorrent',
    'nzbget': 'nzbget',
    'sabnzbd': 'sabnzbd',

    // Transfer backends
    'rclone': 'rclone',

    // Generic
    'passthrough': null
};

// Get icon URL for a given app/downloader type
export function getIconUrl(type, variant = 'default') {
    const normalizedType = (type || '').toLowerCase();
    const iconName = iconMap[normalizedType];

    if (!iconName) {
        return null;
    }

    // Variants: default, light, dark
    if (variant === 'light') {
        return `${SELFHST_CDN}/${iconName}-light.svg`;
    } else if (variant === 'dark') {
        return `${SELFHST_CDN}/${iconName}-dark.svg`;
    }

    return `${SELFHST_CDN}/${iconName}.svg`;
}

