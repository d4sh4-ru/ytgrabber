import { Injectable } from '@angular/core';
import { Observable } from 'rxjs';
import { DownloadVideo, GetJobs, GetLibrary, RemoveJob } from '../wailsjs/wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/wailsjs/runtime';
import { main } from '../wailsjs/wailsjs/go/models';

export type DownloadQuality = 'best' | '1080p' | '720p' | 'audio';

@Injectable({ providedIn: 'root' })
export class DownloadService {
    public readonly progress$: Observable<main.DownloadJob> = new Observable((subscriber) => {
        return EventsOn('download-progress', (job: main.DownloadJob) => {
            subscriber.next(job);
        });
    });

    public readonly status$: Observable<main.DownloadJob> = new Observable((subscriber) => {
        return EventsOn('download-status', (job: main.DownloadJob) => {
            subscriber.next(job);
        });
    });

    public readonly jobRemoved$: Observable<string> = new Observable((subscriber) => {
        return EventsOn('job-removed', (id: string) => {
            subscriber.next(id);
        });
    });

    public download(url: string, quality: DownloadQuality): Promise<string> {
        return DownloadVideo(url, quality);
    }

    public getJobs(): Promise<main.DownloadJob[]> {
        return GetJobs();
    }

    public getLibrary(): Promise<main.VideoRecord[]> {
        return GetLibrary();
    }

    public removeJob(id: string): Promise<void> {
        return RemoveJob(id);
    }
}
