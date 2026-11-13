import {
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    ElementRef,
    inject,
    OnDestroy,
    OnInit,
    ViewChild,
} from '@angular/core';
import { Subject, takeUntil } from 'rxjs';
import { AudioPlayerService, AudioTrack } from '../../services/audio-player.service';
import { NotificationService } from '../../services/notification.service';
import { MediaSessionService } from '../../services/media-session.service';
import { PlaybackService } from '../../services/playback.service';
import { formatDuration } from '../../shared/format-duration';
import { CoverTone, coverTone } from '../../shared/cover-tone';
import { IconComponent } from '../icon/icon.component';

@Component({
    selector: 'app-audio-player-bar',
    standalone: true,
    imports: [IconComponent],
    templateUrl: './audio-player-bar.component.html',
    styleUrl: './audio-player-bar.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AudioPlayerBarComponent implements OnInit, OnDestroy {
    // --- DI / lifecycle ---
    private readonly audioPlayerService = inject(AudioPlayerService);
    private readonly notificationService = inject(NotificationService);
    private readonly playbackService = inject(PlaybackService);
    private readonly mediaSessionService = inject(MediaSessionService);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);
    private readonly destroy$ = new Subject<void>();

    // --- Playback ---
    @ViewChild('audioElement')
    public audioElementRef?: ElementRef<HTMLAudioElement>;

    protected currentTrack: AudioTrack | null = null;
    protected isPlaying = false;
    protected currentTime = 0;
    protected duration = 0;

    /** Colours for a track without a cover image. */
    protected get tone(): CoverTone {
        return coverTone(this.currentTrack?.title ?? '');
    }

    protected get progressPercent(): number {
        return this.duration > 0 ? (this.currentTime / this.duration) * 100 : 0;
    }

    protected get currentTimeLabel(): string {
        return formatDuration(this.currentTime);
    }

    protected get durationLabel(): string {
        return formatDuration(this.duration);
    }

    // --- Lifecycle ---
    public ngOnInit(): void {
        this.audioPlayerService.currentTrack$
            .pipe(takeUntil(this.destroy$))
            .subscribe((track: AudioTrack | null): void => {
                // Switching tracks or closing: remember where the old one stopped.
                this.saveStop();
                if (!track) {
                    this.mediaSessionService.detach(this.audioElementRef?.nativeElement);
                }
                this.currentTrack = track;
                this.isPlaying = false;
                this.currentTime = 0;
                this.duration = 0;
                this.changeDetectorRef.markForCheck();
            });
    }

    public ngOnDestroy(): void {
        this.saveStop();
        this.mediaSessionService.detach(this.audioElementRef?.nativeElement);
        this.destroy$.next();
        this.destroy$.complete();
    }

    // --- Playback ---
    protected onTogglePlay(): void {
        const audioElement = this.audioElementRef?.nativeElement;
        if (!audioElement) {
            return;
        }

        if (audioElement.paused) {
            void audioElement.play();
        } else {
            audioElement.pause();
        }
    }

    protected onClose(): void {
        this.audioPlayerService.stop();
    }

    protected onTimeUpdate(): void {
        const audioElement = this.audioElementRef?.nativeElement;
        this.currentTime = audioElement?.currentTime ?? 0;
        if (audioElement && this.currentTrack && !audioElement.seeking) {
            this.playbackService.progress(this.currentTrack.id, this.currentTime);
        }
        this.changeDetectorRef.markForCheck();
    }

    protected onLoadedMetadata(): void {
        const audioElement = this.audioElementRef?.nativeElement;
        if (audioElement) {
            this.duration = audioElement.duration;
            if (this.currentTrack) {
                this.mediaSessionService.attach(audioElement, {
                    title: this.currentTrack.title,
                    artworkSrc: this.currentTrack.coverSrc,
                });
                const position = this.playbackService.startPosition(
                    this.currentTrack.id,
                    this.currentTrack.positionSeconds,
                );
                if (position > 0 && position < this.duration) {
                    audioElement.currentTime = position;
                }
            }
        }
        this.changeDetectorRef.markForCheck();
    }

    protected onPlay(): void {
        this.isPlaying = true;
        this.updateMediaSession();
        this.changeDetectorRef.markForCheck();
    }

    protected onPause(): void {
        this.isPlaying = false;
        this.saveStop();
        this.updateMediaSession();
        this.changeDetectorRef.markForCheck();
    }

    protected onSeeked(): void {
        this.updateMediaSession();
    }

    /** Keeps the lock screen's play button and position in step. */
    private updateMediaSession(): void {
        const audioElement = this.audioElementRef?.nativeElement;
        if (audioElement) {
            this.mediaSessionService.update(audioElement);
        }
    }

    protected onError(): void {
        this.notificationService.error('Не удалось воспроизвести аудио: файл недоступен.');
        this.audioPlayerService.stop();
    }

    protected onSeek(event: Event): void {
        const audioElement = this.audioElementRef?.nativeElement;
        if (!audioElement || this.duration <= 0) {
            return;
        }

        const percent = Number((event.target as HTMLInputElement).value);
        audioElement.currentTime = (percent / 100) * this.duration;
    }

    private saveStop(): void {
        const audioElement = this.audioElementRef?.nativeElement;
        if (audioElement && this.currentTrack && this.duration > 0) {
            void this.playbackService.stop(this.currentTrack.id, audioElement.currentTime);
        }
    }
}
