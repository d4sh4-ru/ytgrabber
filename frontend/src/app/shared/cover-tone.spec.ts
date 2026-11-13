import { coverTone } from './cover-tone';

describe('coverTone', () => {
    it('gives a title the same tone every time', () => {
        expect(coverTone('Lo-fi beats to study')).toEqual(coverTone('Lo-fi beats to study'));
    });

    it('spreads titles over several tones', () => {
        const titles = ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'];
        const tones = new Set(titles.map((title) => coverTone(title).background));
        expect(tones.size).toBeGreaterThan(2);
    });

    it('copes with an empty title', () => {
        expect(coverTone('').background).toMatch(/^#/);
    });
});
