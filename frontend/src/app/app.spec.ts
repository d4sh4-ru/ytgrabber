import { ComponentFixture, TestBed } from '@angular/core/testing';
import { BehaviorSubject, Observable, Subject } from 'rxjs';
import { App } from './app';
import { DownloadService } from './services/download.service';
import { SettingsService } from './services/settings.service';
import { app, model, tools } from './wailsjs/wailsjs/go/models';

class DownloadServiceStub {
    public readonly progress$ = new Subject<model.DownloadJob>();
    public readonly status$ = new Subject<model.DownloadJob>();
    public readonly jobRemoved$ = new Subject<string>();
    public readonly libraryChanged$ = new Subject<void>();

    public jobs: model.DownloadJob[] = [];

    public getJobs(): Promise<model.DownloadJob[]> {
        return Promise.resolve(this.jobs);
    }

    public getLibrary(): Promise<model.VideoRecord[]> {
        return Promise.resolve([]);
    }
}

class SettingsServiceStub {
    public readonly dependencies$: Observable<tools.Report | null>;

    public startupError = '';
    public report = tools.Report.createFrom({
        ready: true,
        tools: [{ name: 'yt-dlp', required: true, found: true }],
    });

    private readonly dependenciesSubject$ = new BehaviorSubject<tools.Report | null>(null);

    public constructor() {
        this.dependencies$ = this.dependenciesSubject$.asObservable();
    }

    public getAppInfo(): Promise<app.AppInfo> {
        return Promise.resolve(app.AppInfo.createFrom({ startupError: this.startupError }));
    }

    public getSettings(): Promise<app.Settings> {
        return Promise.resolve(app.Settings.createFrom({ downloadsDir: '/d' }));
    }

    public getStorageInfo(): Promise<app.StorageInfo> {
        return Promise.resolve(app.StorageInfo.createFrom({}));
    }

    public refreshDependencies(): Promise<tools.Report> {
        this.dependenciesSubject$.next(this.report);
        return Promise.resolve(this.report);
    }
}

function job(id: string, status: string): model.DownloadJob {
    return model.DownloadJob.createFrom({ id, url: `https://x/${id}`, status, tracks: [] });
}

describe('App', () => {
    let downloadService: DownloadServiceStub;
    let settingsService: SettingsServiceStub;

    beforeEach(async () => {
        downloadService = new DownloadServiceStub();
        settingsService = new SettingsServiceStub();

        await TestBed.configureTestingModule({
            imports: [App],
            providers: [
                { provide: DownloadService, useValue: downloadService },
                { provide: SettingsService, useValue: settingsService },
            ],
        }).compileComponents();
    });

    // ngOnInit chains several awaited backend calls; a macrotask lets them all settle.
    async function settle(fixture: ComponentFixture<App>): Promise<void> {
        await new Promise((resolve) => setTimeout(resolve));
        await fixture.whenStable();
        fixture.detectChanges();
    }

    async function render(): Promise<HTMLElement> {
        const fixture = TestBed.createComponent(App);
        fixture.detectChanges();
        await settle(fixture);
        return fixture.nativeElement as HTMLElement;
    }

    it('renders the title', async () => {
        const compiled = await render();
        expect(compiled.querySelector('h1')?.textContent).toContain('YT Grabber');
    });

    it('shows the startup error instead of the UI', async () => {
        settingsService.startupError = 'database is locked';
        const compiled = await render();
        expect(compiled.querySelector('.banner--error')?.textContent).toContain(
            'database is locked',
        );
        expect(compiled.querySelector('app-download-form')).toBeNull();
    });

    it('warns about missing required tools', async () => {
        settingsService.report = tools.Report.createFrom({
            ready: false,
            tools: [
                { name: 'yt-dlp', required: true, found: false },
                { name: 'deno', required: false, found: false },
            ],
        });
        const compiled = await render();
        const banner = compiled.querySelector('.banner--warning')?.textContent ?? '';
        expect(banner).toContain('yt-dlp');
        expect(banner).not.toContain('deno');
    });

    it('lists loaded jobs and drops removed ones', async () => {
        downloadService.jobs = [job('a', 'error'), job('b', 'downloading')];
        const fixture = TestBed.createComponent(App);
        fixture.detectChanges();
        await settle(fixture);

        const compiled = fixture.nativeElement as HTMLElement;
        expect(compiled.querySelectorAll('app-job-card').length).toBe(2);
        expect(compiled.querySelector('.app__tab-badge')?.textContent?.trim()).toBe('1');

        downloadService.jobRemoved$.next('a');
        fixture.detectChanges();
        expect(compiled.querySelectorAll('app-job-card').length).toBe(1);
    });
});
