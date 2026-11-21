import {
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    EventEmitter,
    inject,
    Input,
    Output,
} from '@angular/core';
import { model } from '../../api/models';
import { NotificationService } from '../../services/notification.service';
import { PeopleService } from '../../services/people.service';
import { ShareService } from '../../services/share.service';
import { IconComponent } from '../icon/icon.component';

/** A link made, as the dialog shows it: once, since the server keeps only hashes. */
interface ShownShare {
    readonly url: string;
    readonly qrUrl: string;
    readonly code: string;
    readonly expiresAt: string;
}

/**
 * Makes a link to one video for someone without an account: first the
 * options (a code, downloading), then the link, its QR code and the code.
 */
@Component({
    selector: 'app-share-dialog',
    standalone: true,
    imports: [IconComponent],
    templateUrl: './share-dialog.component.html',
    styleUrl: './share-dialog.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ShareDialogComponent {
    // --- DI ---
    private readonly shareService = inject(ShareService);
    private readonly peopleService = inject(PeopleService);
    private readonly notificationService = inject(NotificationService);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);

    // --- Video ---
    @Input({ required: true })
    public video!: model.VideoRecord;

    @Output()
    public readonly closed = new EventEmitter<void>();

    // --- Options ---
    protected withCode = true;
    protected allowDownload = false;
    protected isCreating = false;

    // --- Result ---
    protected shown: ShownShare | null = null;
    protected readonly canCopy = typeof navigator.clipboard?.writeText === 'function';

    // --- Options ---
    protected onWithCodeChange(event: Event): void {
        this.withCode = (event.target as HTMLInputElement).checked;
    }

    protected onAllowDownloadChange(event: Event): void {
        this.allowDownload = (event.target as HTMLInputElement).checked;
    }

    protected async onCreate(): Promise<void> {
        this.isCreating = true;
        try {
            const created = await this.shareService.createShare(
                this.video.id,
                this.withCode,
                this.allowDownload,
            );
            const url = this.shareService.shareUrl(created.path);
            this.shown = {
                url,
                qrUrl: this.peopleService.qrUrl(url),
                code: created.code ?? '',
                expiresAt: created.share.expiresAt,
            };
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.isCreating = false;
        this.changeDetectorRef.markForCheck();
    }

    // --- Result ---
    protected expiresLabel(iso: string): string {
        return new Date(iso).toLocaleString('ru-RU', {
            day: 'numeric',
            month: 'long',
            hour: '2-digit',
            minute: '2-digit',
        });
    }

    protected async onCopy(text: string): Promise<void> {
        try {
            await navigator.clipboard.writeText(text);
            this.notificationService.info('Скопировано');
        } catch {
            this.notificationService.error(
                new Error('Не удалось скопировать: выделите текст в поле и скопируйте вручную'),
            );
        }
    }

    protected onSelectAll(event: Event): void {
        (event.target as HTMLInputElement).select();
    }

    protected onClose(): void {
        this.closed.emit();
    }
}
