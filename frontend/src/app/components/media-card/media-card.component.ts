import { ChangeDetectionStrategy, Component, EventEmitter, Input, Output } from '@angular/core';
import { model } from '../../wailsjs/wailsjs/go/models';
import { formatDuration } from '../../shared/format-duration';

/** A library tile for both videos and audio; audio shows its cover or a note icon. */
@Component({
    selector: 'app-media-card',
    standalone: true,
    imports: [],
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
