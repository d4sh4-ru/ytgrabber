import {
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    inject,
    OnDestroy,
    OnInit,
} from '@angular/core';
import { Subject, takeUntil } from 'rxjs';
import { ConfigurableTool, SettingsService } from '../../services/settings.service';
import { NotificationService } from '../../services/notification.service';
import { app, tools } from '../../wailsjs/wailsjs/go/models';
import { formatBytes } from '../../shared/format-bytes';
import {
    ConfirmDialogComponent,
    ConfirmDialogResult,
} from '../confirm-dialog/confirm-dialog.component';

const CONFIGURABLE_TOOLS: ReadonlySet<string> = new Set<ConfigurableTool>(['yt-dlp', 'ffmpeg']);
const CONCURRENCY_OPTIONS: readonly number[] = [1, 2, 3, 4, 5];

/** "Delete all data" is a two-step flow: choose what to delete, then confirm. */
type ResetStep = 'idle' | 'choose' | 'confirm';

@Component({
    selector: 'app-settings',
    standalone: true,
    imports: [ConfirmDialogComponent],
    templateUrl: './settings.component.html',
    styleUrl: './settings.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SettingsComponent implements OnInit, OnDestroy {
    // --- DI / lifecycle ---
    private readonly settingsService = inject(SettingsService);
    private readonly notificationService = inject(NotificationService);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);
    private readonly destroy$ = new Subject<void>();

    // --- Settings ---
    protected settings: app.Settings | null = null;
    protected readonly concurrencyOptions = CONCURRENCY_OPTIONS;

    // --- Dependencies ---
    protected dependencies: tools.Report | null = null;
    protected isCheckingDependencies = false;
    protected isUpdatingYtDlp = false;
    protected updateOutput = '';

    // --- App info ---
    protected appInfo: app.AppInfo | null = null;

    // --- Delete all data ---
    protected storage: app.StorageInfo | null = null;
    protected resetStep: ResetStep = 'idle';
    protected resetDeleteMedia = false;
    protected isResetting = false;

    protected get dataSizeLabel(): string {
        return formatBytes(this.storage?.dataBytes ?? 0);
    }

    protected get mediaSizeLabel(): string {
        return formatBytes(this.storage?.mediaBytes ?? 0);
    }

    protected get mediaOptionLabel(): string {
        const count = this.storage?.mediaFiles ?? 0;
        return `Также удалить скачанные файлы (${count} шт., ${this.mediaSizeLabel})`;
    }

    protected get resetConfirmMessage(): string {
        const media = this.resetDeleteMedia
            ? `Скачанные файлы (${this.storage?.mediaFiles ?? 0} шт., ${this.mediaSizeLabel}) будут удалены с диска.`
            : `Скачанные файлы останутся в папке ${this.settings?.downloadsDir ?? 'загрузок'}.`;
        return `${media} Отменить это нельзя. Приложение закроется.`;
    }

    // --- Lifecycle ---
    public ngOnInit(): void {
        this.settingsService.dependencies$
            .pipe(takeUntil(this.destroy$))
            .subscribe((report: tools.Report | null): void => {
                this.dependencies = report;
                this.changeDetectorRef.markForCheck();
            });

        void this.load();
    }

    public ngOnDestroy(): void {
        this.destroy$.next();
        this.destroy$.complete();
    }

    // --- Settings ---
    protected async onChooseDownloadsDir(): Promise<void> {
        await this.applySettings(this.settingsService.chooseDownloadsDir());
    }

    protected async onOpenDownloadsDir(): Promise<void> {
        try {
            await this.settingsService.openDownloadsDir();
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
    }

    protected async onConcurrencyChange(event: Event): Promise<void> {
        const value = Number((event.target as HTMLSelectElement).value);
        await this.applySettings(this.settingsService.setMaxConcurrentDownloads(value));
    }

    private async load(): Promise<void> {
        try {
            const [settings, appInfo, storage] = await Promise.all([
                this.settingsService.getSettings(),
                this.settingsService.getAppInfo(),
                this.settingsService.getStorageInfo(),
            ]);
            this.settings = settings;
            this.appInfo = appInfo;
            this.storage = storage;
            this.changeDetectorRef.markForCheck();
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
    }

    private async applySettings(request: Promise<app.Settings>): Promise<void> {
        try {
            this.settings = await request;
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.changeDetectorRef.markForCheck();
    }

    // --- Dependencies ---
    protected isConfigurable(tool: tools.Status): boolean {
        return CONFIGURABLE_TOOLS.has(tool.name);
    }

    protected async onRecheck(): Promise<void> {
        this.isCheckingDependencies = true;
        this.changeDetectorRef.markForCheck();
        try {
            await this.settingsService.refreshDependencies();
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.isCheckingDependencies = false;
        this.changeDetectorRef.markForCheck();
    }

    protected async onChooseTool(tool: tools.Status): Promise<void> {
        await this.applySettings(
            this.settingsService.chooseToolPath(tool.name as ConfigurableTool),
        );
    }

    protected async onResetTool(tool: tools.Status): Promise<void> {
        await this.applySettings(this.settingsService.resetToolPath(tool.name as ConfigurableTool));
    }

    protected async onUpdateYtDlp(): Promise<void> {
        this.isUpdatingYtDlp = true;
        this.updateOutput = '';
        this.changeDetectorRef.markForCheck();
        try {
            this.updateOutput = await this.settingsService.updateYtDlp();
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.isUpdatingYtDlp = false;
        this.changeDetectorRef.markForCheck();
    }

    // --- Delete all data ---
    protected async onResetRequested(): Promise<void> {
        try {
            // Fresh numbers: downloads may have finished since the tab opened.
            this.storage = await this.settingsService.getStorageInfo();
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.resetDeleteMedia = false;
        this.resetStep = 'choose';
        this.changeDetectorRef.markForCheck();
    }

    protected onResetChosen(result: ConfirmDialogResult): void {
        this.resetDeleteMedia = result.option;
        this.resetStep = 'confirm';
    }

    protected onResetCancelled(): void {
        this.resetStep = 'idle';
    }

    protected async onResetConfirmed(): Promise<void> {
        this.isResetting = true;
        this.changeDetectorRef.markForCheck();
        try {
            // The backend quits the app once it's done.
            await this.settingsService.resetAllData(this.resetDeleteMedia);
        } catch (error: unknown) {
            this.notificationService.error(error);
            this.isResetting = false;
            this.resetStep = 'idle';
            this.changeDetectorRef.markForCheck();
        }
    }
}
