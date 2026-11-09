import { inject, Injectable } from '@angular/core';
import { BehaviorSubject, Observable } from 'rxjs';
import { core, tools } from '../api/models';
import { Transport } from '../api/transport';

export type ConfigurableTool = 'yt-dlp' | 'ffmpeg';

/**
 * Settings, external tool status and app info. The dependency report is
 * cached in a subject because both the app shell (warning banner) and the
 * settings screen show it.
 */
@Injectable({ providedIn: 'root' })
export class SettingsService {
    public readonly dependencies$: Observable<tools.Report | null>;

    private readonly transport = inject(Transport);
    private readonly dependenciesSubject$ = new BehaviorSubject<tools.Report | null>(null);

    public constructor() {
        this.dependencies$ = this.dependenciesSubject$.asObservable();
    }

    public getAppInfo(): Promise<core.AppInfo> {
        return this.transport.call<core.AppInfo>('GetAppInfo');
    }

    public getSettings(): Promise<core.Settings> {
        return this.transport.call<core.Settings>('GetSettings');
    }

    public async refreshDependencies(): Promise<tools.Report> {
        const report = await this.transport.call<tools.Report>('GetDependencies');
        this.setDependencies(report);
        return report;
    }

    public setDependencies(report: tools.Report): void {
        this.dependenciesSubject$.next(report);
    }

    public chooseDownloadsDir(): Promise<core.Settings> {
        return this.transport.call<core.Settings>('ChooseDownloadsDir');
    }

    public openDownloadsDir(): Promise<void> {
        return this.transport.call<void>('OpenDownloadsDir');
    }

    public async chooseToolPath(tool: ConfigurableTool): Promise<core.Settings> {
        const settings = await this.transport.call<core.Settings>('ChooseToolPath', [tool]);
        await this.refreshDependencies();
        return settings;
    }

    public async resetToolPath(tool: ConfigurableTool): Promise<core.Settings> {
        const settings = await this.transport.call<core.Settings>('ResetToolPath', [tool]);
        await this.refreshDependencies();
        return settings;
    }

    public setMaxConcurrentDownloads(value: number): Promise<core.Settings> {
        return this.transport.call<core.Settings>('SetMaxConcurrentDownloads', [value]);
    }

    public setUpdateSettings(autoCheck: boolean, prerelease: boolean): Promise<core.Settings> {
        return this.transport.call<core.Settings>('SetUpdateSettings', [autoCheck, prerelease]);
    }

    public getStorageInfo(): Promise<core.StorageInfo> {
        return this.transport.call<core.StorageInfo>('GetStorageInfo');
    }

    /** Deletes everything the app created; the app quits right after. */
    public resetAllData(deleteMedia: boolean): Promise<void> {
        return this.transport.call<void>('ResetAllData', [deleteMedia]);
    }

    public async updateYtDlp(): Promise<string> {
        const output = await this.transport.call<string>('UpdateYtDlp');
        await this.refreshDependencies();
        return output;
    }

    /** Puts back the yt-dlp the last update replaced; a second call undoes it. */
    public async rollbackYtDlp(): Promise<tools.Report> {
        const report = await this.transport.call<tools.Report>('RollbackYtDlp');
        this.dependenciesSubject$.next(report);
        return report;
    }
}
