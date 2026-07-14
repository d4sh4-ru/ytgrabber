import { ChangeDetectionStrategy, Component, EventEmitter, Input, Output } from '@angular/core';
import { main } from '../../wailsjs/wailsjs/go/models';
import { formatDuration } from '../../shared/format-duration';
import { buildAssetSrc } from '../../shared/asset-src';

@Component({
  selector: 'app-video-card',
  standalone: true,
  imports: [],
  templateUrl: './video-card.component.html',
  styleUrl: './video-card.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class VideoCardComponent {
  @Input({ required: true })
  public item!: main.VideoRecord;

  @Output()
  public readonly opened = new EventEmitter<main.VideoRecord>();

  protected get durationLabel(): string | null {
    return this.item.durationSeconds > 0 ? formatDuration(this.item.durationSeconds) : null;
  }

  protected get thumbnailSrc(): string | null {
    return this.item.thumbnailPath ? buildAssetSrc(this.item.thumbnailPath) : null;
  }

  protected onClick(): void {
    this.opened.emit(this.item);
  }
}
