import { ChangeDetectionStrategy, Component, EventEmitter, Input, Output } from '@angular/core';
import { main } from '../../wailsjs/wailsjs/go/models';
import { isAudioFilename } from '../../shared/classify-media';
import { VideoCardComponent } from '../video-card/video-card.component';
import { AudioCardComponent } from '../audio-card/audio-card.component';

type LibraryGroup = 'video' | 'audio';

@Component({
  selector: 'app-video-library',
  standalone: true,
  imports: [VideoCardComponent, AudioCardComponent],
  templateUrl: './video-library.component.html',
  styleUrl: './video-library.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class VideoLibraryComponent {
  @Input()
  public items: main.VideoRecord[] = [];

  @Output()
  public readonly videoSelected = new EventEmitter<main.VideoRecord>();

  protected activeGroup: LibraryGroup = 'video';

  protected get videoItems(): main.VideoRecord[] {
    return this.items.filter((item) => !isAudioFilename(item.filename));
  }

  protected get audioItems(): main.VideoRecord[] {
    return this.items.filter((item) => isAudioFilename(item.filename));
  }

  protected setGroup(group: LibraryGroup): void {
    this.activeGroup = group;
  }
}
