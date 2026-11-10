import { Injectable } from '@angular/core';

/** What the lock screen and the notification show for what's playing. */
export interface NowPlaying {
    readonly title: string;
    /** The cover or the video's thumbnail; the app icon when null. */
    readonly artworkSrc: string | null;
}

/** How far the lock screen's skip buttons go when they don't say. */
const DEFAULT_SEEK_SECONDS = 10;

const ACTIONS: readonly MediaSessionAction[] = [
    'play',
    'pause',
    'stop',
    'seekbackward',
    'seekforward',
    'seekto',
];

/**
 * The Media Session API: the title and cover on the lock screen, in the
 * notification shade and on headphones, with their buttons driving the
 * player that plays. One player at a time: the last one to attach.
 */
@Injectable({ providedIn: 'root' })
export class MediaSessionService {
    private readonly session: MediaSession | null =
        typeof navigator !== 'undefined' && 'mediaSession' in navigator
            ? navigator.mediaSession
            : null;
    private element: HTMLMediaElement | null = null;

    /** element now plays nowPlaying: the system's controls drive it. */
    public attach(element: HTMLMediaElement, nowPlaying: NowPlaying): void {
        const session = this.session;
        if (!session || typeof MediaMetadata === 'undefined') {
            return;
        }
        this.element = element;
        session.metadata = new MediaMetadata({
            title: nowPlaying.title,
            artist: 'YT Grabber',
            artwork: nowPlaying.artworkSrc
                ? [{ src: new URL(nowPlaying.artworkSrc, location.href).href, type: 'image/jpeg' }]
                : [{ src: new URL('icons/icon-512.png', document.baseURI).href, sizes: '512x512' }],
        });
        for (const action of ACTIONS) {
            this.setHandler(action, (details: MediaSessionActionDetails): void =>
                this.handle(element, details),
            );
        }
        this.update(element);
    }

    /** Keeps the play/pause state and the position on the lock screen current. */
    public update(element: HTMLMediaElement): void {
        const session = this.session;
        if (!session || element !== this.element) {
            return;
        }
        session.playbackState = element.paused ? 'paused' : 'playing';
        if (Number.isFinite(element.duration) && element.duration > 0) {
            try {
                session.setPositionState({
                    duration: element.duration,
                    position: Math.min(element.currentTime, element.duration),
                    playbackRate: element.playbackRate || 1,
                });
            } catch {
                // Older browsers have no position state; the rest still works.
            }
        }
    }

    /** element stopped for good (closed): the lock screen forgets it. */
    public detach(element: HTMLMediaElement | undefined): void {
        const session = this.session;
        if (!session || !element || element !== this.element) {
            return;
        }
        this.element = null;
        session.metadata = null;
        session.playbackState = 'none';
        for (const action of ACTIONS) {
            this.setHandler(action, null);
        }
    }

    private handle(element: HTMLMediaElement, details: MediaSessionActionDetails): void {
        switch (details.action) {
            case 'play':
                void element.play();
                break;
            case 'pause':
            case 'stop':
                element.pause();
                break;
            case 'seekbackward':
                element.currentTime = Math.max(
                    0,
                    element.currentTime - (details.seekOffset ?? DEFAULT_SEEK_SECONDS),
                );
                break;
            case 'seekforward':
                element.currentTime = Math.min(
                    Number.isFinite(element.duration) ? element.duration : Infinity,
                    element.currentTime + (details.seekOffset ?? DEFAULT_SEEK_SECONDS),
                );
                break;
            case 'seekto':
                if (details.seekTime !== undefined) {
                    element.currentTime = details.seekTime;
                }
                break;
        }
        this.update(element);
    }

    private setHandler(
        action: MediaSessionAction,
        handler: MediaSessionActionHandler | null,
    ): void {
        try {
            this.session?.setActionHandler(action, handler);
        } catch {
            // An action this browser doesn't know: its button just isn't shown.
        }
    }
}
