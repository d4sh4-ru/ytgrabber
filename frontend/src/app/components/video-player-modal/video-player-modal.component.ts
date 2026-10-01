import {
    ChangeDetectionStrategy,
    Component,
    ElementRef,
    EventEmitter,
    HostListener,
    Input,
    Output,
    ViewChild,
} from '@angular/core';
import { model } from '../../wailsjs/wailsjs/go/models';

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
    public item: model.VideoRecord | null = null;

    @Output()
    public readonly closed = new EventEmitter<void>();

    @ViewChild('videoElement')
    public videoElementRef?: ElementRef<HTMLVideoElement>;

    protected playbackError = false;

    @HostListener('document:keydown.escape')
    public onEscape(): void {
        // In fullscreen, Escape belongs to the browser (it exits fullscreen).
        if (this.item && !document.fullscreenElement) {
            this.onClose();
        }
    }

    protected onClose(): void {
        this.videoElementRef?.nativeElement.pause();
        this.playbackError = false;
        this.closed.emit();
    }

    protected onFullscreen(): void {
        void this.videoElementRef?.nativeElement.requestFullscreen();
    }

    protected onPlaybackError(): void {
        this.playbackError = true;
    }
}
