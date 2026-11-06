import { errorMessage } from './error-message';

describe('errorMessage', () => {
    it('keeps a backend error string whole, line breaks included', () => {
        const text = 'ERROR: [youtube] abc: Video unavailable\n' + 'x'.repeat(1000);
        expect(errorMessage(text)).toBe(text);
    });

    it('uses the message of an Error', () => {
        expect(errorMessage(new Error('boom'))).toBe('boom');
    });

    it('uses the message of an error-like object', () => {
        expect(errorMessage({ message: 'нет сети' })).toBe('нет сети');
    });

    it('shows other values as they are instead of a generic phrase', () => {
        expect(errorMessage({ code: 42 })).toBe('{"code":42}');
        expect(errorMessage(404)).toBe('404');
    });

    it('falls back to a generic phrase only when there is nothing to show', () => {
        expect(errorMessage(undefined)).toBe('Неизвестная ошибка');
        expect(errorMessage(null)).toBe('Неизвестная ошибка');
        expect(errorMessage({})).toBe('Неизвестная ошибка');
    });
});
