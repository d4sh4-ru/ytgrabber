import { guessDeviceName } from './device-name';

describe('guessDeviceName', () => {
    it('names the platform and the browser', () => {
        expect(
            guessDeviceName(
                'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1',
            ),
        ).toBe('iPhone · Safari');
        expect(
            guessDeviceName(
                'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36 Edg/130.0',
            ),
        ).toBe('Windows · Edge');
        expect(
            guessDeviceName(
                'Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36',
            ),
        ).toBe('Android · Chrome');
    });

    it('falls back to a generic name', () => {
        expect(guessDeviceName('curl/8.0')).toBe('Браузер');
    });
});
