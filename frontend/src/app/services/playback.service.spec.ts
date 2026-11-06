import { TestBed } from '@angular/core/testing';
import { NEVER, Observable } from 'rxjs';
import { vi } from 'vitest';
import { Transport } from '../api/transport';
import { PlaybackService } from './playback.service';

/** Answers SavePlaybackPosition the way answer says; records every call. */
class FakeTransport extends Transport {
    public readonly saved: [string, number][] = [];
    public answer: (seconds: number) => number = (seconds: number): number => seconds;

    public call<T>(method: string, args: readonly unknown[] = []): Promise<T> {
        if (method !== 'SavePlaybackPosition') {
            return Promise.reject(new Error(`unexpected call ${method}`));
        }
        const [id, seconds] = args as [string, number];
        this.saved.push([id, seconds]);
        return Promise.resolve(this.answer(seconds) as T);
    }

    public events$<T>(): Observable<T> {
        return NEVER;
    }
}

describe('PlaybackService', () => {
    let saved: [string, number][];
    let transport: FakeTransport;
    let service: PlaybackService;

    // The backend rounds positions near the start or end down to 0.
    function installBackend(answer: (seconds: number) => number): void {
        transport.answer = answer;
    }

    beforeEach(() => {
        transport = new FakeTransport();
        saved = transport.saved;
        TestBed.configureTestingModule({
            providers: [{ provide: Transport, useValue: transport }],
        });
        service = TestBed.inject(PlaybackService);
    });

    afterEach(() => {
        vi.useRealTimers();
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
