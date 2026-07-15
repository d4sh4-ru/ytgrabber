import { ChangeDetectionStrategy, Component, inject, Input } from '@angular/core';
import { main } from '../../wailsjs/wailsjs/go/models';
import { formatDuration } from '../../shared/format-duration';
import { buildAssetSrc } from '../../shared/asset-src';
import { AudioPlayerService } from '../../services/audio-player.service';

@Component({
    selector: 'app-audio-card',
    standalone: true,
    imports: [],
    templateUrl: './audio-card.component.html',
    styleUrl: './audio-card.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AudioCardComponent {
    private readonly audioPlayerService = inject(AudioPlayerService);

    @Input({ required: true })
    public item!: main.VideoRecord;

    protected get durationLabel(): string | null {
        return this.item.durationSeconds > 0 ? formatDuration(this.item.durationSeconds) : null;
    }

    protected onClick(): void {
        this.audioPlayerService.play({
            id: this.item.id,
            title: this.item.title,
            src: buildAssetSrc(this.item.filename),
        });
    }
}
