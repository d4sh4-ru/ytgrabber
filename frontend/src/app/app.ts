import { ChangeDetectionStrategy, ChangeDetectorRef, Component, inject, OnDestroy, OnInit } from '@angular/core';
import { merge, Subject, takeUntil } from 'rxjs';
import { DownloadService } from './services/download.service';
import { main } from './wailsjs/wailsjs/go/models';
import { DownloadFormComponent, DownloadRequest } from './components/download-form/download-form.component';
import { LibraryComponent } from './components/video-library/library.component';
import { VideoPlayerModalComponent } from './components/video-player-modal/video-player-modal.component';
import { AudioPlayerBarComponent } from './components/audio-player-bar/audio-player-bar.component';

type AppTab = 'add' | 'library';

const ACTIVE_JOB_STATUSES: ReadonlySet<string> = new Set(['pending', 'downloading', 'processing']);

@Component({
    selector: 'app-root',
    standalone: true,
    imports: [
        DownloadFormComponent,
        LibraryComponent,
        VideoPlayerModalComponent,
        AudioPlayerBarComponent,
    ],
    templateUrl: './app.html',
    styleUrl: './app.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class App implements OnInit, OnDestroy {
    private readonly downloadService = inject(DownloadService);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);
    private readonly destroy$ = new Subject<void>();

    protected activeTab: AppTab = 'add';
    protected jobs: main.DownloadJob[] = [];
    protected library: main.VideoRecord[] = [];
    protected selectedVideo: main.VideoRecord | null = null;

    protected get activeJobsCount(): number {
        return this.jobs.filter((job) => ACTIVE_JOB_STATUSES.has(job.status)).length;
    }

    public async ngOnInit(): Promise<void> {
        const [jobs, library] = await Promise.all([
            this.downloadService.getJobs(),
            this.downloadService.getLibrary(),
        ]);
        this.jobs = jobs;
        this.library = library;
        this.changeDetectorRef.markForCheck();

        merge(this.downloadService.progress$, this.downloadService.status$)
            .pipe(takeUntil(this.destroy$))
            .subscribe((updatedJob: main.DownloadJob): void => {
                this.onJobUpdated(updatedJob);
            });

        this.downloadService.jobRemoved$
            .pipe(takeUntil(this.destroy$))
            .subscribe((id: string): void => {
                this.jobs = this.jobs.filter((existing) => existing.id !== id);
                this.changeDetectorRef.markForCheck();
            });
    }

    public ngOnDestroy(): void {
        this.destroy$.next();
        this.destroy$.complete();
    }

    protected setTab(tab: AppTab): void {
        this.activeTab = tab;
    }

    protected async onDownload(request: DownloadRequest): Promise<void> {
        await this.downloadService.download(request.url, request.quality);
    }

    protected onVideoSelected(video: main.VideoRecord): void {
        this.selectedVideo = video;
    }

    protected onModalClosed(): void {
        this.selectedVideo = null;
    }

    private onJobUpdated(job: main.DownloadJob): void {
        if (job.status === 'done') {
            this.jobs = this.jobs.filter((existing) => existing.id !== job.id);
            void this.refreshLibrary();
        } else {
            this.upsertJob(job);
        }

        this.changeDetectorRef.markForCheck();
    }

    private upsertJob(job: main.DownloadJob): void {
        const index = this.jobs.findIndex((existing) => existing.id === job.id);
        if (index === -1) {
            this.jobs = [...this.jobs, job];
        } else {
            this.jobs = this.jobs.map((existing) => (existing.id === job.id ? job : existing));
        }
    }

    private async refreshLibrary(): Promise<void> {
        this.library = await this.downloadService.getLibrary();
        this.changeDetectorRef.markForCheck();
    }
}
