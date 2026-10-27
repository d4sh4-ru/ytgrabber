import { Injectable } from '@angular/core';
import { Observable, Subject } from 'rxjs';
import { SavePlaybackPosition } from '../wailsjs/wailsjs/go/app/App';

/** How often a position is saved while playing; pausing or closing saves at once. */
const SAVE_INTERVAL_MS = 5000;

/**
 * Remembers where playback of each library entry stopped. The backend
 * keeps the positions (and decides when one counts as watched); this
 * caches what was saved in this session, since the library list only
 * learns about it on its next refresh.
 */
@Injectable({ providedIn: 'root' })
export class PlaybackService {
    /** A position was saved on pause or close: the library is worth refreshing. */
    public readonly saved$: Observable<void>;

    private readonly savedSubject$ = new Subject<void>();
    private readonly positions = new Map<string, number>();
    private readonly lastSaveAt = new Map<string, number>();

    public constructor() {
        this.saved$ = this.savedSubject$.asObservable();
    }

    /** Where playback of id should start; stored is the library's value. */
    public startPosition(id: string, stored: number): number {
        return this.positions.get(id) ?? stored;
    }

    /** Called on every time update; saves at most every few seconds. */
    public progress(id: string, seconds: number): void {
        const now = Date.now();
        if (now - (this.lastSaveAt.get(id) ?? 0) >= SAVE_INTERVAL_MS) {
            this.lastSaveAt.set(id, now);
            void this.save(id, seconds, false);
        }
    }

    /** Saves right away, on pause, end or close. */
    public stop(id: string, seconds: number): Promise<void> {
        this.lastSaveAt.set(id, Date.now());
        return this.save(id, seconds, true);
    }

    private async save(id: string, seconds: number, final: boolean): Promise<void> {
        try {
            const stored = await SavePlaybackPosition(id, seconds);
            const changed = this.positions.get(id) !== stored;
            this.positions.set(id, stored);
            if (final && changed) {
                this.savedSubject$.next();
            }
        } catch {
            // Not remembering a position isn't worth bothering the user about.
        }
    }
}
