import {
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    inject,
    OnDestroy,
    OnInit,
} from '@angular/core';
import { Subject, takeUntil } from 'rxjs';
import type { VideoShare } from '../../api/session';
import { NotificationService } from '../../services/notification.service';
import { ShareService } from '../../services/share.service';
import { pluralRu } from '../../shared/format-relative-time';
import { ConfirmDialogComponent } from '../confirm-dialog/confirm-dialog.component';

const HOUR_MS = 60 * 60 * 1000;
const MINUTE_MS = 60 * 1000;

/** Settings: the admin's links to videos that still work, to revoke. Server only. */
@Component({
    selector: 'app-shares',
    standalone: true,
    imports: [ConfirmDialogComponent],
    templateUrl: './shares.component.html',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SharesComponent implements OnInit, OnDestroy {
    // --- DI / lifecycle ---
    private readonly shareService = inject(ShareService);
    private readonly notificationService = inject(NotificationService);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);
    private readonly destroy$ = new Subject<void>();

    // --- List ---
    protected shares: VideoShare[] = [];

    // --- Revoking ---
    protected shareToRevoke: VideoShare | null = null;
    protected isRevoking = false;

    // --- Lifecycle ---
    public ngOnInit(): void {
        this.shareService.changed$
            .pipe(takeUntil(this.destroy$))
            .subscribe((): void => void this.refresh());
        void this.refresh();
    }

    public ngOnDestroy(): void {
        this.destroy$.next();
        this.destroy$.complete();
    }

    // --- Labels ---
    protected detailsLabel(share: VideoShare): string {
        const parts = [this.leftLabel(share.expiresAt)];
        parts.push(share.hasCode ? 'с кодом' : 'без кода');
        if (share.allowDownload) {
            parts.push('можно скачать');
        }
        parts.push(
            share.views > 0
                ? `открыли ${share.views} ${pluralRu(share.views, 'раз', 'раза', 'раз')}`
                : 'ещё не открывали',
        );
        return parts.join(' · ');
    }

    private leftLabel(expiresAt: string): string {
        const left = new Date(expiresAt).getTime() - Date.now();
        if (left >= HOUR_MS) {
            const hours = Math.floor(left / HOUR_MS);
            return `ещё ${hours} ${pluralRu(hours, 'час', 'часа', 'часов')}`;
        }
        const minutes = Math.max(1, Math.floor(left / MINUTE_MS));
        return `ещё ${minutes} ${pluralRu(minutes, 'минуту', 'минуты', 'минут')}`;
    }

    // --- Revoking ---
    protected onRevoke(share: VideoShare): void {
        this.shareToRevoke = share;
    }

    protected onRevokeCancelled(): void {
        this.shareToRevoke = null;
    }

    protected async onRevokeConfirmed(): Promise<void> {
        const share = this.shareToRevoke;
        if (!share) {
            return;
        }
        this.isRevoking = true;
        try {
            await this.shareService.removeShare(share.id);
            this.shareToRevoke = null;
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.isRevoking = false;
        this.changeDetectorRef.markForCheck();
    }

    // --- List ---
    private async refresh(): Promise<void> {
        try {
            this.shares = await this.shareService.getShares();
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.changeDetectorRef.markForCheck();
    }
}
