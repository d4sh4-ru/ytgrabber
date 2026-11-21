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
import type { ShareTerm, VideoShare } from '../../api/session';
import { NotificationService } from '../../services/notification.service';
import { PeopleService } from '../../services/people.service';
import { ShareService } from '../../services/share.service';
import { IconComponent } from '../icon/icon.component';

interface TermOption {
    readonly term: ShareTerm;
    readonly label: string;
}

/**
 * A video's link for someone without an account. A video without one
 * gets the options first (term, code, downloading); a video that has one
 * shows it as it was made, to send again or to delete.
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

    /** The video's link that works, if it has one. */
    @Input()
    public share: VideoShare | null = null;

    @Output()
    public readonly closed = new EventEmitter<void>();

    // --- Options ---
    protected readonly terms: readonly TermOption[] = [
        { term: 'hour', label: 'Час' },
        { term: 'day', label: 'Сутки' },
        { term: 'week', label: 'Неделя' },
        { term: 'month', label: 'Месяц' },
    ];
    protected term: ShareTerm = 'day';
    protected withCode = true;
    protected allowDownload = false;
    protected isCreating = false;

    // --- Link ---
    protected readonly canCopy = typeof navigator.clipboard?.writeText === 'function';

    // --- Deleting ---
    protected isConfirmingDelete = false;
    protected isDeleting = false;

    /** The link's full address; empty for a link made before it could be shown again. */
    protected get url(): string {
        return this.share?.path ? this.shareService.shareUrl(this.share.path) : '';
    }

    protected get qrUrl(): string {
        return this.peopleService.qrUrl(this.url);
    }

    // --- Options ---
    protected onTermChange(term: ShareTerm): void {
        this.term = term;
    }

    protected onWithCodeChange(event: Event): void {
        this.withCode = (event.target as HTMLInputElement).checked;
    }

    protected onAllowDownloadChange(event: Event): void {
        this.allowDownload = (event.target as HTMLInputElement).checked;
    }

    protected async onCreate(): Promise<void> {
        this.isCreating = true;
        try {
            this.share = await this.shareService.createShare(
                this.video.id,
                this.term,
                this.withCode,
                this.allowDownload,
            );
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.isCreating = false;
        this.changeDetectorRef.markForCheck();
    }

    // --- Link ---
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

    // --- Deleting ---
    protected onDelete(): void {
        this.isConfirmingDelete = true;
    }

    protected onDeleteCancelled(): void {
        this.isConfirmingDelete = false;
    }

    protected async onDeleteConfirmed(): Promise<void> {
        if (!this.share) {
            return;
        }
        this.isDeleting = true;
        try {
            await this.shareService.removeShare(this.share.id);
            this.notificationService.info('Ссылка удалена');
            this.closed.emit();
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.isDeleting = false;
        this.changeDetectorRef.markForCheck();
    }

    protected onClose(): void {
        this.closed.emit();
    }
}
