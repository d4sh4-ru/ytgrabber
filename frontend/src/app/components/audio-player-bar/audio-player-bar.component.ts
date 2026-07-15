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
import { formatDuration } from '../../shared/format-duration';

@Component({
    selector: 'app-audio-player-bar',
    standalone: true,
    imports: [],
    templateUrl: './audio-player-bar.component.html',
    styleUrl: './audio-player-bar.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AudioPlayerBarComponent implements OnInit, OnDestroy {
    private readonly audioPlayerService = inject(AudioPlayerService);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);
    private readonly destroy$ = new Subject<void>();

    @ViewChild('audioElement')
    public audioElementRef?: ElementRef<HTMLAudioElement>;

    protected currentTrack: AudioTrack | null = null;
    protected isPlaying = false;
    protected currentTime = 0;
    protected duration = 0;

    protected get progressPercent(): number {
        return this.duration > 0 ? (this.currentTime / this.duration) * 100 : 0;
    }

    protected get currentTimeLabel(): string {
        return formatDuration(this.currentTime);
    }

    protected get durationLabel(): string {
        return formatDuration(this.duration);
    }

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

    protected onTimeUpdate(): void {
        this.currentTime = this.audioElementRef?.nativeElement.currentTime ?? 0;
        this.changeDetectorRef.markForCheck();
    }

    protected onLoadedMetadata(): void {
        this.duration = this.audioElementRef?.nativeElement.duration ?? 0;
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

    protected onSeek(event: Event): void {
        const audioElement = this.audioElementRef?.nativeElement;
        if (!audioElement || this.duration <= 0) {
            return;
        }

        const percent = Number((event.target as HTMLInputElement).value);
        audioElement.currentTime = (percent / 100) * this.duration;
    }
}
