import {
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    inject,
    OnDestroy,
    OnInit,
} from '@angular/core';
import { Subject, takeUntil } from 'rxjs';
import { model } from '../../api/models';
import { AccountsService } from '../../services/accounts.service';
import { NotificationService } from '../../services/notification.service';

/**
 * The desktop app connected to a server: the sites connected on this
 * computer, to send their cookies to the server, which has no browsers
 * of its own to take them from.
 */
@Component({
    selector: 'app-local-accounts',
    standalone: true,
    templateUrl: './local-accounts.component.html',
    styleUrl: './local-accounts.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class LocalAccountsComponent implements OnInit, OnDestroy {
    // --- DI / lifecycle ---
    private readonly accountsService = inject(AccountsService);
    private readonly notificationService = inject(NotificationService);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);
    private readonly destroy$ = new Subject<void>();

    // --- Accounts ---
    protected localAccounts: model.Account[] = [];
    /** Sites the server already has an account of. */
    private serverDomains = new Set<string>();
    protected sendingId = '';

    // --- Lifecycle ---
    public ngOnInit(): void {
        this.accountsService.accounts$
            .pipe(takeUntil(this.destroy$))
            .subscribe((accounts: model.Account[]): void => {
                this.serverDomains = new Set(accounts.map((account) => account.domain));
                this.changeDetectorRef.markForCheck();
            });
        void this.load();
    }

    public ngOnDestroy(): void {
        this.destroy$.next();
        this.destroy$.complete();
    }

    // --- Accounts ---
    protected isOnServer(account: model.Account): boolean {
        return this.serverDomains.has(account.domain);
    }

    protected async onSend(account: model.Account): Promise<void> {
        this.sendingId = account.id;
        this.changeDetectorRef.markForCheck();
        try {
            const sent = await this.accountsService.sendToServer(account.id);
            this.notificationService.info(`${sent.domain}: cookies отправлены на сервер`);
        } catch (error: unknown) {
            this.notificationService.error(error);
        } finally {
            this.sendingId = '';
            this.changeDetectorRef.markForCheck();
        }
    }

    private async load(): Promise<void> {
        try {
            this.localAccounts = await this.accountsService.localAccounts();
        } catch {
            // The local library may not have started: nothing to offer.
            this.localAccounts = [];
        }
        this.changeDetectorRef.markForCheck();
    }
}
