import { inject, Injectable } from '@angular/core';
import { BehaviorSubject, Observable } from 'rxjs';
import { core } from '../api/models';
import { Transport } from '../api/transport';

export type UpdateStage = 'verifying' | 'downloading' | 'installing';

/** Mirrors core.UpdateProgress in Go (sent as an event, so Wails generates no class for it). */
export interface UpdateProgress {
    readonly stage: UpdateStage;
    readonly done: number;
    readonly total: number;
}

/**
 * New versions of the app. The state is kept here because the banner in
 * the app shell and the settings screen both show it; the backend checks
 * at startup and announces a found update with an event.
 */
@Injectable({ providedIn: 'root' })
export class UpdateService {
    public readonly info$: Observable<core.UpdateInfo | null>;
    public readonly progress$: Observable<UpdateProgress | null>;

    private readonly transport = inject(Transport);
    private readonly infoSubject$ = new BehaviorSubject<core.UpdateInfo | null>(null);
    private readonly progressSubject$ = new BehaviorSubject<UpdateProgress | null>(null);

    public constructor() {
        this.info$ = this.infoSubject$.asObservable();
        this.progress$ = this.progressSubject$.asObservable();

        // Root service: lives as long as the app, so the subscriptions do too.
        this.transport
            .events$<core.UpdateInfo>('update-available')
            .subscribe((info: core.UpdateInfo): void => this.infoSubject$.next(info));
        this.transport
            .events$<UpdateProgress>('update-progress')
            .subscribe((progress: UpdateProgress): void => this.progressSubject$.next(progress));
    }

    /** The last check's result, in case it finished before anyone listened. */
    public async load(): Promise<core.UpdateInfo> {
        const info = await this.transport.call<core.UpdateInfo>('GetUpdateInfo');
        this.infoSubject$.next(info);
        return info;
    }

    public async check(): Promise<core.UpdateInfo> {
        const info = await this.transport.call<core.UpdateInfo>('CheckForUpdates');
        this.infoSubject$.next(info);
        return info;
    }

    /** Downloads and installs the update; the app restarts once it resolves. */
    public async install(): Promise<void> {
        try {
            await this.transport.call<void>('InstallUpdate');
        } catch (error: unknown) {
            this.progressSubject$.next(null);
            throw error;
        }
    }

    public openReleasePage(): Promise<void> {
        return this.transport.call<void>('OpenReleasePage');
    }
}
