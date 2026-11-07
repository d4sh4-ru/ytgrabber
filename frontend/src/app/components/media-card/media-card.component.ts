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
import { formatDuration } from '../../shared/format-duration';
import { IconComponent } from '../icon/icon.component';

/** A library tile for both videos and audio; audio shows its cover or a note icon. */
@Component({
    selector: 'app-media-card',
    standalone: true,
    imports: [IconComponent],
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
        return `${this.item.mediaUrl}?download=1`;
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
    }

    protected onDelete(event: Event): void {
        event.stopPropagation();
        this.deleteRequested.emit(this.item);
    }
}
