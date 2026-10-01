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
import { formatDuration } from '../../shared/format-duration';

const VOLUME_STORAGE_KEY = 'ytgrabber.volume';

@Component({
    selector: 'app-audio-player-bar',
    standalone: true,
    imports: [],
    templateUrl: './audio-player-bar.component.html',
    styleUrl: './audio-player-bar.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AudioPlayerBarComponent implements OnInit, OnDestroy {
    // --- DI / lifecycle ---
    private readonly audioPlayerService = inject(AudioPlayerService);
    private readonly notificationService = inject(NotificationService);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);
    private readonly destroy$ = new Subject<void>();

    // --- Playback ---
    @ViewChild('audioElement')
    public audioElementRef?: ElementRef<HTMLAudioElement>;

    protected currentTrack: AudioTrack | null = null;
    protected isPlaying = false;
    protected currentTime = 0;
    protected duration = 0;

    // --- Volume ---
    protected volume = readStoredVolume();

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
                this.currentTrack = track;
                this.isPlaying = false;
                this.currentTime = 0;
                this.duration = 0;
                this.changeDetectorRef.markForCheck();
            });
    }

    public ngOnDestroy(): void {
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
        this.currentTime = this.audioElementRef?.nativeElement.currentTime ?? 0;
        this.changeDetectorRef.markForCheck();
    }

    protected onLoadedMetadata(): void {
        const audioElement = this.audioElementRef?.nativeElement;
        if (audioElement) {
            audioElement.volume = this.volume;
            this.duration = audioElement.duration;
        }
        this.changeDetectorRef.markForCheck();
    }

    protected onPlay(): void {
        this.isPlaying = true;
        this.changeDetectorRef.markForCheck();
    }

    protected onPause(): void {
        this.isPlaying = false;
        this.changeDetectorRef.markForCheck();
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

    // --- Volume ---
    protected onVolumeChange(event: Event): void {
        this.volume = Number((event.target as HTMLInputElement).value);
        if (this.audioElementRef) {
            this.audioElementRef.nativeElement.volume = this.volume;
        }
        try {
            localStorage.setItem(VOLUME_STORAGE_KEY, String(this.volume));
        } catch {
            // Storage unavailable: the volume just isn't remembered.
        }
    }
}

function readStoredVolume(): number {
    try {
        const stored = Number(localStorage.getItem(VOLUME_STORAGE_KEY));
        return Number.isFinite(stored) && stored > 0 && stored <= 1 ? stored : 1;
    } catch {
        return 1;
    }
}
