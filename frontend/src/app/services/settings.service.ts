import { Injectable } from '@angular/core';
import { BehaviorSubject, Observable } from 'rxjs';
import {
    ChooseDownloadsDir,
    ChooseToolPath,
    GetAppInfo,
    GetDependencies,
    GetSettings,
    GetStorageInfo,
    OpenDownloadsDir,
    ResetAllData,
    ResetToolPath,
    SetMaxConcurrentDownloads,
    UpdateYtDlp,
} from '../wailsjs/wailsjs/go/app/App';
import { app, tools } from '../wailsjs/wailsjs/go/models';

export type ConfigurableTool = 'yt-dlp' | 'ffmpeg';

/**
 * Settings, external tool status and app info. The dependency report is
 * cached in a subject because both the app shell (warning banner) and the
 * settings screen show it.
 */
@Injectable({ providedIn: 'root' })
export class SettingsService {
    public readonly dependencies$: Observable<tools.Report | null>;

    private readonly dependenciesSubject$ = new BehaviorSubject<tools.Report | null>(null);

    public constructor() {
        this.dependencies$ = this.dependenciesSubject$.asObservable();
    }

    public getAppInfo(): Promise<app.AppInfo> {
        return GetAppInfo();
    }

    public getSettings(): Promise<app.Settings> {
        return GetSettings();
    }

    public async refreshDependencies(): Promise<tools.Report> {
        const report = await GetDependencies();
        this.dependenciesSubject$.next(report);
        return report;
    }

    public chooseDownloadsDir(): Promise<app.Settings> {
        return ChooseDownloadsDir();
    }

    public openDownloadsDir(): Promise<void> {
        return OpenDownloadsDir();
    }

    public async chooseToolPath(tool: ConfigurableTool): Promise<app.Settings> {
        const settings = await ChooseToolPath(tool);
        await this.refreshDependencies();
        return settings;
    }

    public async resetToolPath(tool: ConfigurableTool): Promise<app.Settings> {
        const settings = await ResetToolPath(tool);
        await this.refreshDependencies();
        return settings;
    }

    public setMaxConcurrentDownloads(value: number): Promise<app.Settings> {
        return SetMaxConcurrentDownloads(value);
    }

    public getStorageInfo(): Promise<app.StorageInfo> {
        return GetStorageInfo();
    }

    /** Deletes everything the app created; the app quits right after. */
    public resetAllData(deleteMedia: boolean): Promise<void> {
        return ResetAllData(deleteMedia);
    }

    public async updateYtDlp(): Promise<string> {
        const output = await UpdateYtDlp();
        await this.refreshDependencies();
        return output;
    }
}
