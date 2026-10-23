import { formatRelativeTime, pluralRu } from './format-relative-time';

describe('formatRelativeTime', () => {
    const now = new Date('2026-10-23T12:00:00Z');

    function ago(ms: number): string {
        return new Date(now.getTime() - ms).toISOString();
    }

    it('formats elapsed time in Russian', () => {
        expect(formatRelativeTime(ago(10 * 1000), now)).toBe('только что');
        expect(formatRelativeTime(ago(5 * 60 * 1000), now)).toBe('5 минут назад');
        expect(formatRelativeTime(ago(21 * 60 * 1000), now)).toBe('21 минуту назад');
        expect(formatRelativeTime(ago(3 * 3600 * 1000), now)).toBe('3 часа назад');
        expect(formatRelativeTime(ago(30 * 3600 * 1000), now)).toBe('вчера');
        expect(formatRelativeTime(ago(40 * 24 * 3600 * 1000), now)).toBe('40 дней назад');
    });

    it('returns an empty string for missing dates', () => {
        expect(formatRelativeTime('', now)).toBe('');
        expect(formatRelativeTime('not a date', now)).toBe('');
    });

    it('picks Russian plural forms', () => {
        expect([1, 2, 5, 11, 12, 21, 22, 25, 111].map((n) => pluralRu(n, 'a', 'b', 'c'))).toEqual([
            'a',
            'b',
            'c',
            'c',
            'c',
            'a',
            'b',
            'c',
            'c',
        ]);
    });
});
