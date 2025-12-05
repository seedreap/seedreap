// Sparkline chart utility

const maxHistory = 100;
let history = [];

export function getHistory() {
    return history;
}

export function setHistory(newHistory) {
    history = newHistory;
}

export function addPoint(speed) {
    history.push(speed);
    if (history.length > maxHistory) {
        history.shift();
    }
}

export function getSparklinePaths() {
    if (history.length < 2) {
        return { linePath: '', areaPath: '' };
    }

    const width = 120;
    const height = 35;
    const padding = 2;
    const maxSpeed = Math.max(...history, 1);

    const points = history.map((val, i) => ({
        x: padding + (i / (maxHistory - 1)) * (width - 2 * padding),
        y: height - padding - ((val / maxSpeed) * (height - 2 * padding))
    }));

    const linePath = points
        .map((p, i) => `${i === 0 ? 'M' : 'L'} ${p.x.toFixed(1)} ${p.y.toFixed(1)}`)
        .join(' ');

    const areaPath = linePath +
        ` L ${points[points.length - 1].x.toFixed(1)} ${height - padding}` +
        ` L ${points[0].x.toFixed(1)} ${height - padding} Z`;

    return { linePath, areaPath };
}
