import {
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    inject,
    OnDestroy,
    OnInit,
} from '@angular/core';
import { merge, Subject, takeUntil } from 'rxjs';
import { DownloadService } from './services/download.service';
import { NotificationService } from './services/notification.service';
import { SettingsService } from './services/settings.service';
import { model, tools } from './wailsjs/wailsjs/go/models';
import { DownloadFormComponent } from './components/download-form/download-form.component';
import { LibraryComponent } from './components/video-library/library.component';
import { VideoPlayerModalComponent } from './components/video-player-modal/video-player-modal.component';
import { AudioPlayerBarComponent } from './components/audio-player-bar/audio-player-bar.component';
import { SettingsComponent } from './components/settings/settings.component';
import { ToastHostComponent } from './components/toast-host/toast-host.component';

type AppTab = 'add' | 'library' | 'settings';

const ACTIVE_JOB_STATUSES: ReadonlySet<string> = new Set(['pending', 'downloading', 'processing']);

@Component({
    selector: 'app-root',
    standalone: true,
    imports: [
        DownloadFormComponent,
        LibraryComponent,
        VideoPlayerModalComponent,
        AudioPlayerBarComponent,
        SettingsComponent,
        ToastHostComponent,
    ],
    templateUrl: './app.html',
    styleUrl: './app.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class App implements OnInit, OnDestroy {
    // --- DI / lifecycle ---
    private readonly downloadService = inject(DownloadService);
    private readonly settingsService = inject(SettingsService);
    private readonly notificationService = inject(NotificationService);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);
    private readonly destroy$ = new Subject<void>();

    // --- Navigation ---
    protected activeTab: AppTab = 'add';

    // --- App state ---
    protected startupError = '';
    protected dependencies: tools.Report | null = null;

    // --- Jobs ---
    protected jobs: model.DownloadJob[] = [];
    private readonly removedJobIds = new Set<string>();

    // --- Library ---
    protected library: model.VideoRecord[] = [];
    protected selectedVideo: model.VideoRecord | null = null;

    protected get activeJobsCount(): number {
        return this.jobs.filter((job) => ACTIVE_JOB_STATUSES.has(job.status)).length;
    }

    protected get missingRequiredTools(): string[] {
        return (this.dependencies?.tools ?? [])
            .filter((tool) => tool.required && !tool.found)
            .map((tool) => tool.name);
    }

    // --- Lifecycle ---
    public async ngOnInit(): Promise<void> {
        // Subscribe before the initial load so no event emitted meanwhile is
        // lost; the load result is then merged with whatever arrived.
        this.subscribeToEvents();

        const info = await this.settingsService.getAppInfo();
        this.startupError = info.startupError;
        this.changeDetectorRef.markForCheck();
        if (this.startupError) {
            return;
        }

        await Promise.all([this.loadJobs(), this.refreshLibrary(), this.refreshDependencies()]);
    }

    public ngOnDestroy(): void {
        this.destroy$.next();
        this.destroy$.complete();
    }

    // --- Navigation ---
    protected setTab(tab: AppTab): void {
        this.activeTab = tab;
    }

    // --- Jobs ---
    private subscribeToEvents(): void {
        merge(this.downloadService.progress$, this.downloadService.status$)
            .pipe(takeUntil(this.destroy$))
            .subscribe((job: model.DownloadJob): void => this.onJobUpdated(job));

        this.downloadService.jobRemoved$
            .pipe(takeUntil(this.destroy$))
            .subscribe((id: string): void => {
                this.removedJobIds.add(id);
                this.jobs = this.jobs.filter((existing) => existing.id !== id);
                this.changeDetectorRef.markForCheck();
            });

        this.downloadService.libraryChanged$.pipe(takeUntil(this.destroy$)).subscribe((): void => {
            void this.refreshLibrary();
        });

        this.settingsService.dependencies$
            .pipe(takeUntil(this.destroy$))
            .subscribe((report: tools.Report | null): void => {
                this.dependencies = report;
                this.changeDetectorRef.markForCheck();
            });
    }

    private async loadJobs(): Promise<void> {
        try {
            const loaded = await this.downloadService.getJobs();
            const received = new Map(this.jobs.map((job) => [job.id, job]));
            const merged = loaded
                .filter((job) => !this.removedJobIds.has(job.id))
                .map((job) => received.get(job.id) ?? job);
            const loadedIds = new Set(loaded.map((job) => job.id));
            this.jobs = [...merged, ...this.jobs.filter((job) => !loadedIds.has(job.id))];
            this.changeDetectorRef.markForCheck();
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
    }

    private onJobUpdated(job: model.DownloadJob): void {
        if (job.status === 'done') {
            this.jobs = this.jobs.filter((existing) => existing.id !== job.id);
        } else if (!this.removedJobIds.has(job.id)) {
            this.upsertJob(job);
        }
        this.changeDetectorRef.markForCheck();
    }

    private upsertJob(job: model.DownloadJob): void {
        const index = this.jobs.findIndex((existing) => existing.id === job.id);
        if (index === -1) {
            this.jobs = [...this.jobs, job];
        } else {
            this.jobs = this.jobs.map((existing) => (existing.id === job.id ? job : existing));
        }
    }

    // --- Library ---
    protected onVideoSelected(video: model.VideoRecord): void {
        this.selectedVideo = video;
    }

    protected onModalClosed(): void {
        this.selectedVideo = null;
    }

    private async refreshLibrary(): Promise<void> {
        try {
            this.library = await this.downloadService.getLibrary();
            if (
                this.selectedVideo &&
                !this.library.some((video) => video.id === this.selectedVideo?.id)
            ) {
                this.selectedVideo = null;
            }
            this.changeDetectorRef.markForCheck();
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
    }

    // --- Dependencies ---
    private async refreshDependencies(): Promise<void> {
        try {
            await this.settingsService.refreshDependencies();
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
    }
}
