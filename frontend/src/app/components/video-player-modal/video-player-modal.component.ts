import {
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    ElementRef,
    EventEmitter,
    HostListener,
    inject,
    Input,
    OnDestroy,
    Output,
    ViewChild,
} from '@angular/core';
import { model } from '../../api/models';
import { ConnectionService } from '../../services/connection.service';
import { Transport } from '../../api/transport';
import { ConfirmDialogComponent } from '../confirm-dialog/confirm-dialog.component';
import { IconComponent } from '../icon/icon.component';
import { MediaSessionService } from '../../services/media-session.service';
import { PlaybackService } from '../../services/playback.service';
import {
    deviceDownloadUrl,
    meteredDownloadWarning,
    startDeviceDownload,
} from '../../shared/device-download';
import { formatDuration } from '../../shared/format-duration';
import { bypassServiceWorker } from '../../shared/media-url';

/** How far a double tap seeks. */
const SEEK_STEP_SECONDS = 10;
/** Two taps closer than this are a double tap. */
const DOUBLE_TAP_MS = 300;
/** Roughly how tall the video's own control bar is, at its bottom edge. */
const CONTROLS_HEIGHT = 56;
/** How long the "−10 с" hint stays up. */
const SEEK_HINT_MS = 600;

/** Safari on iOS can't put an element in fullscreen, only the video itself. */
interface WebKitVideoElement extends HTMLVideoElement {
    webkitEnterFullscreen?: () => void;
}

/** Locking the orientation is in Chrome on Android but not in TypeScript's DOM types. */
interface LockableOrientation extends ScreenOrientation {
    lock?: (orientation: 'landscape') => Promise<void>;
}

export type SeekSide = 'back' | 'forward';

@Component({
    selector: 'app-video-player-modal',
    standalone: true,
    imports: [IconComponent, ConfirmDialogComponent],
    templateUrl: './video-player-modal.component.html',
    styleUrl: './video-player-modal.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class VideoPlayerModalComponent implements OnDestroy {
    private readonly playbackService = inject(PlaybackService);
    private readonly mediaSessionService = inject(MediaSessionService);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);
    private readonly connectionService = inject(ConnectionService);

    @Input()
    public item: model.VideoRecord | null = null;

    @Output()
    public readonly closed = new EventEmitter<void>();

    @ViewChild('videoElement')
    public videoElementRef?: ElementRef<HTMLVideoElement>;

    /** The desktop opens files from their folder; a browser saves them to the device. */
    protected readonly isDesktop = inject(Transport).isDesktop;
    /** The desktop connected to a server saves files to the computer itself. */
    protected readonly isDesktopApp = inject(Transport).isDesktopApp;

    protected playbackError = false;
    /** Where playback resumed, for the "start over" hint; null when from the start. */
    protected resumedFrom: number | null = null;

    // --- Double-tap seeking ---
    protected seekHint: SeekSide | null = null;
    private lastTap: { at: number; side: SeekSide } | null = null;
    private seekHintTimer: ReturnType<typeof setTimeout> | undefined;

    // --- Downloading ---
    /** The warning shown before a download over mobile data; null when none. */
    protected downloadWarning: string | null = null;

    protected get resumedFromLabel(): string {
        return formatDuration(this.resumedFrom ?? 0);
    }

    protected get mediaSrc(): string {
        return this.item ? bypassServiceWorker(this.item.mediaUrl) : '';
    }

    protected get downloadUrl(): string {
        return this.item ? deviceDownloadUrl(this.item) : '';
    }

    protected get seekHintLabel(): string {
        return this.seekHint === 'back' ? `−${SEEK_STEP_SECONDS} с` : `+${SEEK_STEP_SECONDS} с`;
    }

    @HostListener('document:keydown.escape')
    public onEscape(): void {
        // In fullscreen, Escape belongs to the browser (it exits fullscreen);
        // with the download warning up, it closes the warning.
        if (this.item && !document.fullscreenElement && !this.downloadWarning) {
            this.onClose();
        }
    }

    public ngOnDestroy(): void {
        clearTimeout(this.seekHintTimer);
    }

    protected onClose(): void {
        const video = this.videoElementRef?.nativeElement;
        if (video && this.item) {
            void this.playbackService.stop(this.item.id, video.currentTime);
            video.pause();
        }
        this.mediaSessionService.detach(video);
        this.playbackError = false;
        this.resumedFrom = null;
        this.downloadWarning = null;
        this.lastTap = null;
        this.closed.emit();
    }

    // --- Resuming ---
    protected onLoadedMetadata(): void {
        const video = this.videoElementRef?.nativeElement;
        if (!video || !this.item) {
            return;
        }
        this.mediaSessionService.attach(video, {
            title: this.item.title,
            artworkSrc: this.item.thumbnailUrl ? bypassServiceWorker(this.item.thumbnailUrl) : null,
        });
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
        this.onPlayStateChanged();
    }

    /** Keeps the lock screen's play button and position in step. */
    protected onPlayStateChanged(): void {
        const video = this.videoElementRef?.nativeElement;
        if (video) {
            this.mediaSessionService.update(video);
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

    // --- Fullscreen ---
    protected onFullscreen(): void {
        const video = this.videoElementRef?.nativeElement as WebKitVideoElement | undefined;
        if (!video) {
            return;
        }
        if (!video.requestFullscreen && video.webkitEnterFullscreen) {
            // iOS: the system player, which turns with the phone by itself.
            video.webkitEnterFullscreen();
            return;
        }
        void video
            .requestFullscreen()
            .then(() => this.lockLandscape(video))
            .catch(() => undefined);
    }

    /** A wide video on a phone held upright: turn it sideways, as video apps do. */
    private lockLandscape(video: HTMLVideoElement): void {
        const orientation = screen.orientation as LockableOrientation | undefined;
        if (video.videoWidth > video.videoHeight && orientation?.lock) {
            // Refused on desktops and where the browser doesn't allow it.
            orientation.lock('landscape').catch(() => undefined);
        }
    }

    // --- Double-tap seeking ---
    /**
     * A double tap on the left or right part of the video seeks back or
     * forward, as in video apps. A single tap is left to the video's own
     * controls.
     */
    protected onVideoPointerUp(event: PointerEvent): void {
        const video = this.videoElementRef?.nativeElement;
        if (!video || event.pointerType !== 'touch') {
            return;
        }
        const rect = video.getBoundingClientRect();
        if (event.clientY > rect.bottom - CONTROLS_HEIGHT) {
            // Taps on the video's own control bar (the scrubber) aren't gestures.
            this.lastTap = null;
            return;
        }
        const x = (event.clientX - rect.left) / rect.width;
        const side: SeekSide | null = x < 1 / 3 ? 'back' : x > 2 / 3 ? 'forward' : null;
        const now = event.timeStamp;
        const isDoubleTap =
            side !== null &&
            this.lastTap !== null &&
            this.lastTap.side === side &&
            now - this.lastTap.at < DOUBLE_TAP_MS;
        this.lastTap = side ? { at: now, side } : null;
        // lastTap stays set, so a third quick tap seeks again, as in video apps.
        if (isDoubleTap && side) {
            this.seek(video, side);
        }
    }

    private seek(video: HTMLVideoElement, side: SeekSide): void {
        const step = side === 'back' ? -SEEK_STEP_SECONDS : SEEK_STEP_SECONDS;
        const end = Number.isFinite(video.duration) ? video.duration : Infinity;
        video.currentTime = Math.min(end, Math.max(0, video.currentTime + step));

        this.seekHint = side;
        clearTimeout(this.seekHintTimer);
        this.seekHintTimer = setTimeout(() => {
            this.seekHint = null;
            this.changeDetectorRef.markForCheck();
        }, SEEK_HINT_MS);
    }

    // --- Downloading ---
    protected onDownload(event: Event): void {
        if (!this.item) {
            return;
        }
        if (this.isDesktopApp) {
            event.preventDefault();
            void this.connectionService.saveToComputer(this.item);
            return;
        }
        const warning = meteredDownloadWarning(this.item);
        if (warning) {
            event.preventDefault();
            this.downloadWarning = warning;
        }
    }

    protected onDownloadConfirmed(): void {
        this.downloadWarning = null;
        startDeviceDownload(this.downloadUrl);
    }

    protected onDownloadCancelled(): void {
        this.downloadWarning = null;
    }

    protected onPlaybackError(): void {
        this.playbackError = true;
    }
}
