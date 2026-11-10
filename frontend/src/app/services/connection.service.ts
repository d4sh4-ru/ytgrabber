import { inject, Injectable } from '@angular/core';
import { BehaviorSubject, Observable } from 'rxjs';
import {
    desktopConnection,
    DesktopConnection,
    REMOTE_STATUS_EVENT,
    RemoteStatus,
} from '../api/connection';
import { model } from '../api/models';
import { Transport } from '../api/transport';
import { NotificationService } from './notification.service';

/**
 * The desktop app's link to a server: which mode the window works in, how
 * the connection is doing, and switching between the modes. Switching
 * reloads the window, since every service works through a transport
 * chosen at startup.
 */
@Injectable({ providedIn: 'root' })
export class ConnectionService {
    public readonly status$: Observable<RemoteStatus>;

    private readonly transport = inject(Transport);
    private readonly notificationService = inject(NotificationService);
    private readonly statusSubject$: BehaviorSubject<RemoteStatus>;

    /** The server the window works with; empty in the local mode and outside the desktop app. */
    public get server(): string {
        return desktopConnection()?.server ?? '';
    }

    /** The server to offer when connecting. */
    public get lastServer(): string {
        return desktopConnection()?.lastServer ?? '';
    }

    public constructor() {
        this.statusSubject$ = new BehaviorSubject<RemoteStatus>(
            desktopConnection()?.status ?? { connected: false },
        );
        this.status$ = this.statusSubject$.asObservable();
        if (this.transport.isDesktopApp) {
            // Root service: lives as long as the window.
            this.transport
                .events$<RemoteStatus>(REMOTE_STATUS_EVENT)
                .subscribe((status: RemoteStatus): void => this.statusSubject$.next(status));
        }
    }

    /** Switches the window to the server at address, then reloads it. */
    public async connect(address: string): Promise<void> {
        await this.transport.call<DesktopConnection>('ConnectServer', [address]);
        location.reload();
    }

    /** Switches the window back to the local library, then reloads it. */
    public async useLocal(): Promise<void> {
        await this.transport.call<DesktopConnection>('UseLocal');
        location.reload();
    }

    /** Tries the server now; the window reloads to pick up what it missed. */
    public async reconnect(): Promise<void> {
        await this.transport.call<void>('ReconnectServer');
        location.reload();
    }

    /**
     * Saves a library file from the server to this computer, asking where:
     * a WebView doesn't save a link's file the way a browser does.
     */
    public async saveToComputer(item: model.VideoRecord): Promise<void> {
        try {
            const saved = await this.transport.call<boolean>('SaveMediaFile', [
                item.mediaUrl,
                item.filePath,
            ]);
            if (saved) {
                this.notificationService.info(`«${item.title}» сохранено на компьютер`);
            }
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
    }
}
