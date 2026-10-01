import { Injectable } from '@angular/core';
import {
    DeleteVideo,
    DownloadVideo,
    GetJobs,
    GetLibrary,
    RemoveJob,
    RetryJob,
    RevealVideo,
} from '../wailsjs/wailsjs/go/app/App';
import { model } from '../wailsjs/wailsjs/go/models';
import { fromWailsEvent } from '../shared/wails-event';

export type DownloadQuality = 'best' | '1080p' | '720p' | 'audio';

@Injectable({ providedIn: 'root' })
export class DownloadService {
    public readonly progress$ = fromWailsEvent<model.DownloadJob>('download-progress');
    public readonly status$ = fromWailsEvent<model.DownloadJob>('download-status');
    public readonly jobRemoved$ = fromWailsEvent<string>('job-removed');
    public readonly libraryChanged$ = fromWailsEvent<void>('library-changed');

    public download(url: string, quality: DownloadQuality): Promise<string> {
        return DownloadVideo(url, quality);
    }

    public getJobs(): Promise<model.DownloadJob[]> {
        return GetJobs();
    }

    public removeJob(id: string): Promise<void> {
        return RemoveJob(id);
    }

    public retryJob(id: string): Promise<void> {
        return RetryJob(id);
    }

    public getLibrary(): Promise<model.VideoRecord[]> {
        return GetLibrary();
    }

    public deleteVideo(id: string, deleteFiles: boolean): Promise<void> {
        return DeleteVideo(id, deleteFiles);
    }

    public revealVideo(id: string): Promise<void> {
        return RevealVideo(id);
    }
}
