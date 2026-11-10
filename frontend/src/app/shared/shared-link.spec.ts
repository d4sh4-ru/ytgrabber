import { sharedLink } from './shared-link';

describe('sharedLink', () => {
    function at(path: string): URL {
        return new URL(path, 'https://grabber.example.ts.net');
    }

    it('takes the link from whichever field the app put it in', () => {
        expect(sharedLink(at('/share?url=https%3A%2F%2Fyoutu.be%2Fabc'))).toBe(
            'https://youtu.be/abc',
        );
        // YouTube on Android: the title, then the link, in text.
        expect(
            sharedLink(at('/share?title=Clip&text=Clip%20https%3A%2F%2Fyoutu.be%2Fabc%3Fsi%3Dx')),
        ).toBe('https://youtu.be/abc?si=x');
        expect(sharedLink(at('/share?title=https%3A%2F%2Fexample.com%2Fv'))).toBe(
            'https://example.com/v',
        );
    });

    it('is null without a link or away from /share', () => {
        expect(sharedLink(at('/share?text=just%20words'))).toBeNull();
        expect(sharedLink(at('/?text=https%3A%2F%2Fyoutu.be%2Fabc'))).toBeNull();
    });
});
