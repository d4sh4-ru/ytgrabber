import {
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    EventEmitter,
    inject,
    Input,
    OnInit,
    Output,
} from '@angular/core';
import { AccountsService } from '../../services/accounts.service';
import { NotificationService } from '../../services/notification.service';
import { connectedMessage, cookieCountLabel } from '../../shared/account-summary';
import { formatRelativeTime } from '../../shared/format-relative-time';
import { browsers, model } from '../../wailsjs/wailsjs/go/models';
import { ConfirmDialogComponent } from '../confirm-dialog/confirm-dialog.component';

/**
 * Connects or refreshes a site's account: from a browser profile the scan
 * finds signed in, or from a cookies.txt file. It explains what is read
 * and where it is kept before anything is taken: the user hands over a
 * login, so the consent has to be informed.
 */
@Component({
    selector: 'app-account-connect-dialog',
    standalone: true,
    imports: [ConfirmDialogComponent],
    templateUrl: './account-connect-dialog.component.html',
    styleUrl: './account-connect-dialog.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AccountConnectDialogComponent implements OnInit {
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

    /** Scan results; null until a scan ran. */
    protected matches: browsers.Match[] | null = null;
    protected isScanning = false;
    protected connectingDir = '';

    protected get siteLabel(): string {
        return this.site || this.siteInput.trim() || 'этого сайта';
    }

    protected get title(): string {
        return this.site ? `Вход на ${this.site}` : 'Подключить сайт';
    }

    protected get isBusy(): boolean {
        return this.isImporting || this.connectingDir !== '';
    }

    public ngOnInit(): void {
        // A known site (refresh, or a failed download) is scanned right
        // away; the scan reads cookie names only.
        if (this.site) {
            void this.onScan();
        }
    }

    protected matchStateLabel(match: browsers.Match): string {
        if (match.error) {
            return match.error;
        }
        if (!match.loginKnown) {
            return cookieCountLabel(match.cookieCount);
        }
        return match.loggedIn ? 'вход найден' : 'вход не найден';
    }

    protected matchActivityLabel(match: browsers.Match): string {
        const activity = formatRelativeTime(match.lastAccess);
        return activity ? `активность ${activity}` : '';
    }

    protected async onScan(): Promise<void> {
        const site = this.site || this.siteInput.trim();
        if (!site) {
            this.notificationService.error('Укажите ссылку или домен сайта');
            return;
        }

        this.isScanning = true;
        this.changeDetectorRef.markForCheck();
        try {
            this.matches = await this.accountsService.scanBrowsers(site);
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.isScanning = false;
        this.changeDetectorRef.markForCheck();
    }

    protected async onConnectProfile(match: browsers.Match): Promise<void> {
        const site = this.site || this.siteInput.trim();
        this.connectingDir = match.profileDir;
        this.changeDetectorRef.markForCheck();
        try {
            const account = await this.accountsService.connectProfile(site, match);
            this.notificationService.info(connectedMessage(account));
            this.connected.emit(account);
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.connectingDir = '';
        this.changeDetectorRef.markForCheck();
    }

    protected onSiteInput(event: Event): void {
        this.siteInput = (event.target as HTMLInputElement).value;
        // Results were for the previous site.
        this.matches = null;
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
