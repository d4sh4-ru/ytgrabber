import {
    ChangeDetectionStrategy,
    Component,
    ElementRef,
    EventEmitter,
    HostListener,
    inject,
    Input,
    Output,
    ViewChild,
} from '@angular/core';
import { model } from '../../wailsjs/wailsjs/go/models';
import { IconComponent } from '../icon/icon.component';
import { PlaybackService } from '../../services/playback.service';
import { formatDuration } from '../../shared/format-duration';

@Component({
    selector: 'app-video-player-modal',
    standalone: true,
    imports: [IconComponent],
    templateUrl: './video-player-modal.component.html',
    styleUrl: './video-player-modal.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class VideoPlayerModalComponent {
    private readonly playbackService = inject(PlaybackService);

    @Input()
    public item: model.VideoRecord | null = null;

    @Output()
    public readonly closed = new EventEmitter<void>();

    @ViewChild('videoElement')
    public videoElementRef?: ElementRef<HTMLVideoElement>;

    protected playbackError = false;
    /** Where playback resumed, for the "start over" hint; null when from the start. */
    protected resumedFrom: number | null = null;

    protected get resumedFromLabel(): string {
        return formatDuration(this.resumedFrom ?? 0);
    }

    @HostListener('document:keydown.escape')
    public onEscape(): void {
        // In fullscreen, Escape belongs to the browser (it exits fullscreen).
        if (this.item && !document.fullscreenElement) {
            this.onClose();
        }
    }

    protected onClose(): void {
        const video = this.videoElementRef?.nativeElement;
        if (video && this.item) {
            void this.playbackService.stop(this.item.id, video.currentTime);
            video.pause();
        }
        this.playbackError = false;
        this.resumedFrom = null;
        this.closed.emit();
    }

    // --- Resuming ---
    protected onLoadedMetadata(): void {
        const video = this.videoElementRef?.nativeElement;
        if (!video || !this.item) {
            return;
        }
        const position = this.playbackService.startPosition(
            this.item.id,
            this.item.positionSeconds,
        );
        if (position > 0 && position < video.duration) {
            video.currentTime = position;
            this.resumedFrom = position;
        } else {
            this.resumedFrom = null;
        }
    }

    protected onTimeUpdate(): void {
        const video = this.videoElementRef?.nativeElement;
        if (video && this.item && !video.seeking) {
            this.playbackService.progress(this.item.id, video.currentTime);
        }
    }

    protected onPaused(): void {
        const video = this.videoElementRef?.nativeElement;
        if (video && this.item) {
            void this.playbackService.stop(this.item.id, video.currentTime);
        }
    }

    protected onStartOver(): void {
        const video = this.videoElementRef?.nativeElement;
        if (video) {
            video.currentTime = 0;
            void video.play();
        }
        this.resumedFrom = null;
    }

    protected onFullscreen(): void {
        void this.videoElementRef?.nativeElement.requestFullscreen();
    }

    protected onPlaybackError(): void {
        this.playbackError = true;
    }
}
