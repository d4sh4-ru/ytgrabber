const MINUTE_MS = 60 * 1000;
const HOUR_MS = 60 * MINUTE_MS;
const DAY_MS = 24 * HOUR_MS;

/** Russian plural form for n: one (1, 21), few (2–4, 22–24) or many. */
export function pluralRu(n: number, one: string, few: string, many: string): string {
    const lastTwo = Math.abs(n) % 100;
    const last = lastTwo % 10;
    if (lastTwo >= 11 && lastTwo <= 14) {
        return many;
    }
    if (last === 1) {
        return one;
    }
    if (last >= 2 && last <= 4) {
        return few;
    }
    return many;
}

/** "только что", "5 минут назад", "вчера", "40 дней назад"; '' for a missing date. */
export function formatRelativeTime(iso: string, now: Date = new Date()): string {
    const time = Date.parse(iso);
    if (!iso || Number.isNaN(time)) {
        return '';
    }

    const elapsed = Math.max(0, now.getTime() - time);
    if (elapsed < MINUTE_MS) {
        return 'только что';
    }
    if (elapsed < HOUR_MS) {
        const minutes = Math.floor(elapsed / MINUTE_MS);
        return `${minutes} ${pluralRu(minutes, 'минуту', 'минуты', 'минут')} назад`;
    }
    if (elapsed < DAY_MS) {
        const hours = Math.floor(elapsed / HOUR_MS);
        return `${hours} ${pluralRu(hours, 'час', 'часа', 'часов')} назад`;
    }
    const days = Math.floor(elapsed / DAY_MS);
    if (days === 1) {
        return 'вчера';
    }
    return `${days} ${pluralRu(days, 'день', 'дня', 'дней')} назад`;
}
