import {
    ChangeDetectionStrategy,
    Component,
    EventEmitter,
    inject,
    Input,
    Output,
} from '@angular/core';
import { model } from '../../api/models';
import { Transport } from '../../api/transport';
import {
    deviceDownloadUrl,
    meteredDownloadWarning,
    startDeviceDownload,
} from '../../shared/device-download';
import { formatDuration } from '../../shared/format-duration';
import { ConfirmDialogComponent } from '../confirm-dialog/confirm-dialog.component';
import { IconComponent } from '../icon/icon.component';

/** A library tile for both videos and audio; audio shows its cover or a note icon. */
@Component({
    selector: 'app-media-card',
    standalone: true,
    imports: [IconComponent, ConfirmDialogComponent],
    templateUrl: './media-card.component.html',
    styleUrl: './media-card.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MediaCardComponent {
    @Input({ required: true })
    public item!: model.VideoRecord;

    @Output()
    public readonly opened = new EventEmitter<model.VideoRecord>();

    @Output()
    public readonly revealed = new EventEmitter<model.VideoRecord>();

    @Output()
    public readonly deleteRequested = new EventEmitter<model.VideoRecord>();

    /** The desktop shows the file in its folder; a browser saves it to the device. */
    protected readonly isDesktop = inject(Transport).isDesktop;

    /** The warning shown before a download over mobile data; null when none. */
    protected downloadWarning: string | null = null;

    protected get durationLabel(): string | null {
        return this.item.durationSeconds > 0 ? formatDuration(this.item.durationSeconds) : null;
    }

    /** How much was watched, 0–100; null when not started (or finished). */
    protected get watchedPercent(): number | null {
        const { positionSeconds, durationSeconds } = this.item;
        if (!positionSeconds || durationSeconds <= 0) {
            return null;
        }
        return Math.min(100, (positionSeconds / durationSeconds) * 100);
    }

    protected get watchedTitle(): string {
        return `Остановились на ${formatDuration(this.item.positionSeconds)}`;
    }

    /** The server sends the file as an attachment under its own name. */
    protected get downloadUrl(): string {
        return deviceDownloadUrl(this.item);
    }

    protected get isAudio(): boolean {
        return this.item.mediaKind === 'audio';
    }

    protected onOpen(): void {
        this.opened.emit(this.item);
    }

    protected onReveal(event: Event): void {
        event.stopPropagation();
        this.revealed.emit(this.item);
    }

    protected onDownload(event: Event): void {
        // Let the link save the file without opening the player.
        event.stopPropagation();
        const warning = meteredDownloadWarning(this.item);
        if (warning) {
            event.preventDefault();
            this.downloadWarning = warning;
        }
    }

    protected onDownloadConfirmed(): void {
        this.downloadWarning = null;
        startDeviceDownload(this.downloadUrl);
    }

    protected onDownloadCancelled(): void {
        this.downloadWarning = null;
    }

    protected onDelete(event: Event): void {
        event.stopPropagation();
        this.deleteRequested.emit(this.item);
    }
}
