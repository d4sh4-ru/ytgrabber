import {
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    EventEmitter,
    inject,
    Input,
    Output,
} from '@angular/core';
import { AccountsService } from '../../services/accounts.service';
import { NotificationService } from '../../services/notification.service';
import { connectedMessage } from '../../shared/account-summary';
import { model } from '../../wailsjs/wailsjs/go/models';
import { ConfirmDialogComponent } from '../confirm-dialog/confirm-dialog.component';

/**
 * Connects or refreshes a site's account from a cookies.txt file. It
 * explains what is read and where it is kept before the file dialog
 * opens: the user hands over a login, so the consent has to be informed.
 */
@Component({
    selector: 'app-account-connect-dialog',
    standalone: true,
    imports: [ConfirmDialogComponent],
    templateUrl: './account-connect-dialog.component.html',
    styleUrl: './account-connect-dialog.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AccountConnectDialogComponent {
    private readonly accountsService = inject(AccountsService);
    private readonly notificationService = inject(NotificationService);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);

    /** The site to connect; when empty the user types a link or domain. */
    @Input()
    public site = '';

    @Output()
    public readonly connected = new EventEmitter<model.Account>();

    @Output()
    public readonly cancelled = new EventEmitter<void>();

    protected siteInput = '';
    protected isImporting = false;

    protected get siteLabel(): string {
        return this.site || this.siteInput.trim() || 'этого сайта';
    }

    protected get title(): string {
        return this.site ? `Вход на ${this.site}` : 'Подключить сайт';
    }

    protected onSiteInput(event: Event): void {
        this.siteInput = (event.target as HTMLInputElement).value;
    }

    protected async onConfirm(): Promise<void> {
        const site = this.site || this.siteInput.trim();
        if (!site) {
            this.notificationService.error('Укажите ссылку или домен сайта');
            return;
        }

        this.isImporting = true;
        this.changeDetectorRef.markForCheck();
        try {
            const account = await this.accountsService.importCookiesFile(site);
            if (account) {
                this.notificationService.info(connectedMessage(account));
                this.connected.emit(account);
            }
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.isImporting = false;
        this.changeDetectorRef.markForCheck();
    }

    protected onCancel(): void {
        this.cancelled.emit();
    }
}
