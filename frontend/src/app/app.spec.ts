import { ComponentFixture, TestBed } from '@angular/core/testing';
import { BehaviorSubject, Observable, Subject } from 'rxjs';
import { App } from './app';
import { AccountsService } from './services/accounts.service';
import { UpdateProgress, UpdateService } from './services/update.service';
import { DownloadService } from './services/download.service';
import { SettingsService } from './services/settings.service';
import { ToolInstallService, ToolInstallState } from './services/tool-install.service';
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

class ToolInstallServiceStub {
    public readonly state$ = new BehaviorSubject<ToolInstallState>({
        isRunning: false,
        progress: {},
    });
    public readonly installCalls: (readonly string[])[] = [];

    public install(names: readonly string[] = []): Promise<void> {
        this.installCalls.push(names);
        return Promise.resolve();
    }

    public cancel(): Promise<void> {
        return Promise.resolve();
    }
}

class AccountsServiceStub {
    public readonly accounts$ = new BehaviorSubject<model.Account[]>([]);

    public refresh(): Promise<model.Account[]> {
        return Promise.resolve(this.accounts$.value);
    }
}

class UpdateServiceStub {
    public readonly info$ = new BehaviorSubject<app.UpdateInfo | null>(null);
    public readonly progress$ = new BehaviorSubject<UpdateProgress | null>(null);
    public installs = 0;
    public pageOpened = 0;

    public load(): Promise<app.UpdateInfo> {
        return Promise.resolve(this.info$.value ?? app.UpdateInfo.createFrom({}));
    }

    public install(): Promise<void> {
        this.installs++;
        return Promise.resolve();
    }

    public openReleasePage(): Promise<void> {
        this.pageOpened++;
        return Promise.resolve();
    }
}

function job(id: string, status: string): model.DownloadJob {
    return model.DownloadJob.createFrom({ id, url: `https://x/${id}`, status, tracks: [] });
}

describe('App', () => {
    let downloadService: DownloadServiceStub;
    let settingsService: SettingsServiceStub;
    let toolInstallService: ToolInstallServiceStub;
    let updateService: UpdateServiceStub;

    beforeEach(async () => {
        downloadService = new DownloadServiceStub();
        settingsService = new SettingsServiceStub();
        toolInstallService = new ToolInstallServiceStub();
        updateService = new UpdateServiceStub();

        await TestBed.configureTestingModule({
            imports: [App],
            providers: [
                { provide: DownloadService, useValue: downloadService },
                { provide: SettingsService, useValue: settingsService },
                { provide: ToolInstallService, useValue: toolInstallService },
                { provide: AccountsService, useValue: new AccountsServiceStub() },
                { provide: UpdateService, useValue: updateService },
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

    it('warns about missing required tools and installs them from the banner', async () => {
        settingsService.report = tools.Report.createFrom({
            ready: false,
            tools: [
                { name: 'yt-dlp', required: true, found: false, installable: true },
                { name: 'deno', required: false, found: false, installable: true },
            ],
        });
        const fixture = TestBed.createComponent(App);
        fixture.detectChanges();
        await settle(fixture);
        const compiled = fixture.nativeElement as HTMLElement;

        const banner = compiled.querySelector('.banner--warning');
        expect(banner?.textContent).toContain('yt-dlp');
        expect(banner?.textContent).not.toContain('deno');

        const install = Array.from(banner!.querySelectorAll('button')).find((button) =>
            button.textContent?.includes('Установить автоматически'),
        );
        install!.click();
        await settle(fixture);

        expect(toolInstallService.installCalls).toEqual([[]]);
        expect(compiled.querySelector('app-settings')).not.toBeNull();
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

    it('offers an available update and installs it', async () => {
        updateService.info$.next(
            app.UpdateInfo.createFrom({ available: true, version: '1.3.0', canInstall: true }),
        );
        const fixture = TestBed.createComponent(App);
        fixture.detectChanges();
        await settle(fixture);
        const compiled = fixture.nativeElement as HTMLElement;

        const banner = compiled.querySelector('.banner--info');
        expect(banner?.textContent).toContain('Доступна версия 1.3.0');
        Array.from(banner!.querySelectorAll('button'))
            .find((button) => button.textContent?.includes('Обновить и перезапустить'))!
            .click();
        await settle(fixture);
        expect(updateService.installs).toBe(1);
    });

    it('links to the release page when the update cannot be installed in place', async () => {
        updateService.info$.next(
            app.UpdateInfo.createFrom({
                available: true,
                version: '1.3.0',
                canInstall: false,
                installNote: 'нет прав на запись',
            }),
        );
        const fixture = TestBed.createComponent(App);
        fixture.detectChanges();
        await settle(fixture);
        const banner = (fixture.nativeElement as HTMLElement).querySelector('.banner--info')!;

        Array.from(banner.querySelectorAll('button'))
            .find((button) => button.textContent?.includes('Скачать'))!
            .click();
        expect(updateService.pageOpened).toBe(1);

        Array.from(banner.querySelectorAll('button'))
            .find((button) => button.textContent?.includes('Позже'))!
            .click();
        fixture.detectChanges();
        expect((fixture.nativeElement as HTMLElement).querySelector('.banner--info')).toBeNull();
    });
});
