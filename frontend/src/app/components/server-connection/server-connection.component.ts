import {
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    inject,
    OnDestroy,
    OnInit,
} from '@angular/core';
import { Subject, takeUntil } from 'rxjs';
import type { RemoteStatus } from '../../api/connection';
import { ConnectionService } from '../../services/connection.service';
import { NotificationService } from '../../services/notification.service';

/**
 * Desktop only: whether the app works with its own library or with a
 * ytgrabber server, and switching between the two.
 */
@Component({
    selector: 'app-server-connection',
    standalone: true,
    templateUrl: './server-connection.component.html',
    styleUrl: './server-connection.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ServerConnectionComponent implements OnInit, OnDestroy {
    // --- DI / lifecycle ---
    private readonly connectionService = inject(ConnectionService);
    private readonly notificationService = inject(NotificationService);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);
    private readonly destroy$ = new Subject<void>();

    // --- Connection ---
    protected readonly server = this.connectionService.server;
    protected status: RemoteStatus = { connected: false };
    protected isSwitching = false;

    // --- Connecting ---
    protected address = this.connectionService.lastServer;
    protected connectError = '';

    // --- Lifecycle ---
    public ngOnInit(): void {
        this.connectionService.status$
            .pipe(takeUntil(this.destroy$))
            .subscribe((status: RemoteStatus): void => {
                this.status = status;
                this.changeDetectorRef.markForCheck();
            });
    }

    public ngOnDestroy(): void {
        this.destroy$.next();
        this.destroy$.complete();
    }

    // --- Connecting ---
    protected onAddressInput(event: Event): void {
        this.address = (event.target as HTMLInputElement).value;
        this.connectError = '';
    }

    protected async onConnect(event: Event): Promise<void> {
        event.preventDefault();
        if (!this.address.trim() || this.isSwitching) {
            return;
        }
        this.isSwitching = true;
        this.connectError = '';
        try {
            await this.connectionService.connect(this.address);
        } catch (error: unknown) {
            this.connectError = error instanceof Error ? error.message : String(error);
            this.isSwitching = false;
            this.changeDetectorRef.markForCheck();
        }
    }

    // --- Connected ---
    protected async onUseLocal(): Promise<void> {
        await this.switch((): Promise<void> => this.connectionService.useLocal());
    }

    protected async onReconnect(): Promise<void> {
        await this.switch((): Promise<void> => this.connectionService.reconnect());
    }

    private async switch(action: () => Promise<void>): Promise<void> {
        this.isSwitching = true;
        try {
            await action();
        } catch (error: unknown) {
            this.notificationService.error(error);
            this.isSwitching = false;
            this.changeDetectorRef.markForCheck();
        }
    }
}
