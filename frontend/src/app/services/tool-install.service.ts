import { inject, Injectable } from '@angular/core';
import { BehaviorSubject, Observable } from 'rxjs';
import { CancelToolInstall, InstallTools } from '../wailsjs/wailsjs/go/app/App';
import { fromWailsEvent } from '../shared/wails-event';
import { SettingsService } from './settings.service';

export type ToolInstallStage = 'downloading' | 'verifying' | 'extracting' | 'done' | 'failed';

/** Mirrors installer.Progress in Go (sent as an event, so Wails generates no class for it). */
export interface ToolInstallProgress {
    readonly tool: string;
    readonly stage: ToolInstallStage;
    readonly downloaded: number;
    readonly total: number;
    readonly error?: string;
}

export interface ToolInstallState {
    readonly isRunning: boolean;
    /** Latest progress per tool, keyed by install name (ffprobe comes with ffmpeg). */
    readonly progress: Readonly<Record<string, ToolInstallProgress>>;
}

const IDLE_STATE: ToolInstallState = { isRunning: false, progress: {} };

/**
 * Downloads missing external tools through the backend. The state is kept
 * here because the app banner starts an install that the settings screen
 * then shows.
 */
@Injectable({ providedIn: 'root' })
export class ToolInstallService {
    public readonly state$: Observable<ToolInstallState>;

    private readonly settingsService = inject(SettingsService);
    private readonly stateSubject$ = new BehaviorSubject<ToolInstallState>(IDLE_STATE);

    public constructor() {
        this.state$ = this.stateSubject$.asObservable();

        // Root service: lives as long as the app, so the subscription does too.
        fromWailsEvent<ToolInstallProgress>('tool-install-progress').subscribe(
            (progress: ToolInstallProgress): void => {
                const state = this.stateSubject$.value;
                this.stateSubject$.next({
                    ...state,
                    progress: { ...state.progress, [progress.tool]: progress },
                });
            },
        );
    }

    /** Installs the given tools, or every missing one when the list is empty. */
    public async install(names: readonly string[] = []): Promise<void> {
        if (this.stateSubject$.value.isRunning) {
            return;
        }

        this.stateSubject$.next({ isRunning: true, progress: {} });
        try {
            this.settingsService.setDependencies(await InstallTools([...names]));
        } catch (error: unknown) {
            // Some tools may have been installed before the failure.
            await this.settingsService.refreshDependencies().catch((): void => undefined);
            throw error;
        } finally {
            this.stateSubject$.next({ ...this.stateSubject$.value, isRunning: false });
        }
    }

    public cancel(): Promise<void> {
        return CancelToolInstall();
    }
}
