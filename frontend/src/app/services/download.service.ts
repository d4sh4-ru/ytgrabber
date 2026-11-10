import { inject, Injectable } from '@angular/core';
import { merge, Observable, Subject } from 'rxjs';
import { model } from '../api/models';
import { Transport } from '../api/transport';

export type DownloadQuality = 'best' | '1080p' | '720p' | 'audio';

@Injectable({ providedIn: 'root' })
export class DownloadService {
    private readonly transport = inject(Transport);
    /** Jobs removed from here: the event may never come over a dead socket. */
    private readonly removedHere$ = new Subject<string>();
    private readonly libraryChangedHere$ = new Subject<void>();

    public readonly progress$: Observable<model.DownloadJob> =
        this.transport.events$<model.DownloadJob>('download-progress');
    public readonly status$: Observable<model.DownloadJob> =
        this.transport.events$<model.DownloadJob>('download-status');
    /**
     * A job is gone: the backend says so, or it was removed from here.
     * A phone's socket dies in the background without noticing, so the
     * event alone left removed jobs on screen.
     */
    public readonly jobRemoved$: Observable<string> = merge(
        this.transport.events$<string>('job-removed'),
        this.removedHere$,
    );
    /** The library changed: the backend says so, or something was deleted from here. */
    public readonly libraryChanged$: Observable<void> = merge(
        this.transport.events$<void>('library-changed'),
        this.libraryChangedHere$,
    );

    /** useAccount=false downloads without the site's connected account. */
    public download(url: string, quality: DownloadQuality, useAccount: boolean): Promise<string> {
        return this.transport.call<string>('DownloadVideo', [url, quality, useAccount]);
    }

    public getJobs(): Promise<model.DownloadJob[]> {
        return this.transport.call<model.DownloadJob[]>('GetJobs');
    }

    public async removeJob(id: string): Promise<void> {
        await this.transport.call<void>('RemoveJob', [id]);
        this.removedHere$.next(id);
    }

    public retryJob(id: string, useAccount: boolean): Promise<void> {
        return this.transport.call<void>('RetryJob', [id, useAccount]);
    }

    public getLibrary(): Promise<model.VideoRecord[]> {
        return this.transport.call<model.VideoRecord[]>('GetLibrary');
    }

    public async deleteVideo(id: string, deleteFiles: boolean): Promise<void> {
        await this.transport.call<void>('DeleteVideo', [id, deleteFiles]);
        this.libraryChangedHere$.next();
    }

    public revealVideo(id: string): Promise<void> {
        return this.transport.call<void>('RevealVideo', [id]);
    }
}
