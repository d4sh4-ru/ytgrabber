import { ChangeDetectionStrategy, Component, EventEmitter, Input, Output } from '@angular/core';
import { model } from '../../wailsjs/wailsjs/go/models';
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

    protected onDelete(event: Event): void {
        event.stopPropagation();
        this.deleteRequested.emit(this.item);
    }
}
