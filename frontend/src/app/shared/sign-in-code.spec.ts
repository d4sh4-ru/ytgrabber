import { signInCodeFromScan } from './sign-in-code';

describe('signInCodeFromScan', () => {
    const origin = 'https://ytg.example.com';

    it('takes the code out of a link to this server', () => {
        expect(signInCodeFromScan('https://ytg.example.com/?code=KQ4XE-6E5BR', origin)).toEqual({
            kind: 'code',
            code: 'KQ4XE-6E5BR',
        });
    });

    it('takes a bare code as it is', () => {
        expect(signInCodeFromScan(' kq4xe-6e5br\n', origin)).toEqual({
            kind: 'code',
            code: 'KQ4XE-6E5BR',
        });
    });

    it('refuses a link to another server, naming it', () => {
        expect(signInCodeFromScan('https://other.example.com/?code=KQ4XE-6E5BR', origin)).toEqual({
            kind: 'other-server',
            server: 'other.example.com',
        });
        expect(signInCodeFromScan('http://ytg.example.com/?code=KQ4XE-6E5BR', origin).kind).toBe(
            'other-server',
        );
    });

    it('refuses what holds no code', () => {
        for (const text of [
            'https://ytg.example.com/',
            'wails://wails/?code=KQ4XE-6E5BR',
            'javascript:alert(1)',
            'просто текст',
        ]) {
            expect(signInCodeFromScan(text, origin).kind).toBe('not-a-code');
        }
    });
});
