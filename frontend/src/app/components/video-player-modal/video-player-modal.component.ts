import {
    ChangeDetectionStrategy,
    Component,
    ElementRef,
    EventEmitter,
    Input,
    Output,
    ViewChild,
} from '@angular/core';
import { main } from '../../wailsjs/wailsjs/go/models';
import { buildAssetSrc } from '../../shared/asset-src';

@Component({
    selector: 'app-video-player-modal',
    standalone: true,
    imports: [],
    templateUrl: './video-player-modal.component.html',
    styleUrl: './video-player-modal.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class VideoPlayerModalComponent {
    @Input()
    public item: main.VideoRecord | null = null;

    @Output()
    public readonly closed = new EventEmitter<void>();

    @ViewChild('videoElement')
    public videoElementRef?: ElementRef<HTMLVideoElement>;

    protected get videoSrc(): string {
        return this.item ? buildAssetSrc(this.item.filename) : '';
    }

    protected onClose(): void {
        this.closed.emit();
    }

    protected onFullscreen(): void {
        void this.videoElementRef?.nativeElement.requestFullscreen();
    }
}
