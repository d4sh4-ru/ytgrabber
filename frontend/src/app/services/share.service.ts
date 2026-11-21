import { inject, Injectable } from '@angular/core';
import { Observable, Subject } from 'rxjs';
import type { NewShare, VideoShare } from '../api/session';
import { desktopConnection } from '../api/connection';
import { Transport } from '../api/transport';

/** Links to single videos for people without an account. Server and admins only. */
@Injectable({ providedIn: 'root' })
export class ShareService {
    private readonly transport = inject(Transport);
    private readonly changedSubject$ = new Subject<void>();

    /** Emits when a link is made or revoked, so lists reload. */
    public readonly changed$: Observable<void> = this.changedSubject$.asObservable();

    public getShares(): Promise<VideoShare[]> {
        return this.transport.call<VideoShare[]>('GetShares');
    }

    public async createShare(
        videoId: string,
        withCode: boolean,
        allowDownload: boolean,
    ): Promise<NewShare> {
        const created = await this.transport.call<NewShare>('CreateShare', [
            videoId,
            withCode,
            allowDownload,
        ]);
        this.changedSubject$.next();
        return created;
    }

    public async removeShare(id: string): Promise<void> {
        await this.transport.call<void>('RemoveShare', [id]);
        this.changedSubject$.next();
    }

    /** The full address of a link the server returned as /s/<token>. */
    public shareUrl(path: string): string {
        // The desktop connected to a server has an origin of its own (wails://).
        const base = desktopConnection()?.server || location.origin;
        return base.replace(/\/+$/, '') + path;
    }
}
