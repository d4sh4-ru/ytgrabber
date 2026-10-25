import { TestBed } from '@angular/core/testing';
import { vi } from 'vitest';
import { PlaybackService } from './playback.service';

type Saver = (id: string, seconds: number) => Promise<number>;

describe('PlaybackService', () => {
    let saved: [string, number][];
    let service: PlaybackService;

    // The backend rounds positions near the start or end down to 0.
    function installBackend(answer: (seconds: number) => number): void {
        const save: Saver = (id: string, seconds: number): Promise<number> => {
            saved.push([id, seconds]);
            return Promise.resolve(answer(seconds));
        };
        (window as unknown as { go: unknown }).go = {
            app: { App: { SavePlaybackPosition: save } },
        };
    }

    beforeEach(() => {
        saved = [];
        installBackend((seconds: number): number => seconds);
        TestBed.configureTestingModule({});
        service = TestBed.inject(PlaybackService);
    });

    afterEach(() => {
        vi.useRealTimers();
        delete (window as unknown as { go?: unknown }).go;
    });

    it('saves progress at most every few seconds', () => {
        vi.useFakeTimers();
        service.progress('a', 1);
        service.progress('a', 2);
        vi.advanceTimersByTime(5000);
        service.progress('a', 7);
        expect(saved).toEqual([
            ['a', 1],
            ['a', 7],
        ]);
    });

    it('resumes from what was saved in this session', async () => {
        expect(service.startPosition('a', 40)).toBe(40);
        await service.stop('a', 125);
        expect(service.startPosition('a', 40)).toBe(125);

        installBackend((): number => 0); // watched to the end
        await service.stop('a', 598);
        expect(service.startPosition('a', 125)).toBe(0);
    });

    it('announces stops that changed a position', async () => {
        let announcements = 0;
        service.saved$.subscribe((): void => {
            announcements++;
        });
        await service.stop('a', 125);
        await service.stop('a', 125);
        expect(announcements).toBe(1);
    });
});
