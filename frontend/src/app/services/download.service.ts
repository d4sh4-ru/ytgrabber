import { inject, Injectable } from '@angular/core';
import { Observable } from 'rxjs';
import { model } from '../api/models';
import { Transport } from '../api/transport';

export type DownloadQuality = 'best' | '1080p' | '720p' | 'audio';

@Injectable({ providedIn: 'root' })
export class DownloadService {
    private readonly transport = inject(Transport);

    public readonly progress$: Observable<model.DownloadJob> =
        this.transport.events$<model.DownloadJob>('download-progress');
    public readonly status$: Observable<model.DownloadJob> =
        this.transport.events$<model.DownloadJob>('download-status');
    public readonly jobRemoved$: Observable<string> = this.transport.events$<string>('job-removed');
    public readonly libraryChanged$: Observable<void> =
        this.transport.events$<void>('library-changed');

    /** useAccount=false downloads without the site's connected account. */
    public download(url: string, quality: DownloadQuality, useAccount: boolean): Promise<string> {
        return this.transport.call<string>('DownloadVideo', [url, quality, useAccount]);
    }

    public getJobs(): Promise<model.DownloadJob[]> {
        return this.transport.call<model.DownloadJob[]>('GetJobs');
    }

    public removeJob(id: string): Promise<void> {
        return this.transport.call<void>('RemoveJob', [id]);
    }

    public retryJob(id: string, useAccount: boolean): Promise<void> {
        return this.transport.call<void>('RetryJob', [id, useAccount]);
    }

    public getLibrary(): Promise<model.VideoRecord[]> {
        return this.transport.call<model.VideoRecord[]>('GetLibrary');
    }

    public deleteVideo(id: string, deleteFiles: boolean): Promise<void> {
        return this.transport.call<void>('DeleteVideo', [id, deleteFiles]);
    }

    public revealVideo(id: string): Promise<void> {
        return this.transport.call<void>('RevealVideo', [id]);
    }
}
