import { inject, Injectable } from '@angular/core';
import { BehaviorSubject, Observable } from 'rxjs';
import type { ShareTerm, VideoShare } from '../api/session';
import { desktopConnection } from '../api/connection';
import { Transport } from '../api/transport';

/**
 * Links to single videos for people without an account. Server and
 * admins only. A video has at most one link that works; the list here is
 * what the library and the settings show.
 */
@Injectable({ providedIn: 'root' })
export class ShareService {
    private readonly transport = inject(Transport);
    private readonly sharesSubject$ = new BehaviorSubject<VideoShare[]>([]);

    /** The admin's links that work, as last loaded. */
    public readonly shares$: Observable<VideoShare[]> = this.sharesSubject$.asObservable();

    public async refresh(): Promise<void> {
        this.sharesSubject$.next(await this.transport.call<VideoShare[]>('GetShares'));
    }

    /**
     * Makes a link to a video. If it has one already, the server returns
     * that one as it was made, whatever is asked here.
     */
    public async createShare(
        videoId: string,
        term: ShareTerm,
        withCode: boolean,
        allowDownload: boolean,
    ): Promise<VideoShare> {
        const share = await this.transport.call<VideoShare>('CreateShare', [
            videoId,
            term,
            withCode,
            allowDownload,
        ]);
        const others = this.sharesSubject$.value.filter((known) => known.id !== share.id);
        this.sharesSubject$.next([share, ...others]);
        return share;
    }

    public async removeShare(id: string): Promise<void> {
        await this.transport.call<void>('RemoveShare', [id]);
        this.sharesSubject$.next(this.sharesSubject$.value.filter((share) => share.id !== id));
    }

    /** The video's link that still works, from a list of links. */
    public shareOf(shares: readonly VideoShare[], videoId: string): VideoShare | null {
        const now = Date.now();
        return (
            shares.find(
                (share) => share.videoId === videoId && new Date(share.expiresAt).getTime() > now,
            ) ?? null
        );
    }

    /** The full address of a link the server returned as /s/<token>. */
    public shareUrl(path: string): string {
        // The desktop connected to a server has an origin of its own (wails://).
        const base = desktopConnection()?.server || location.origin;
        return base.replace(/\/+$/, '') + path;
    }
}
