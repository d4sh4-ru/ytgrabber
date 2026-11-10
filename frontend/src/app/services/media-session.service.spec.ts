import { MediaSessionService } from './media-session.service';

/** navigator.mediaSession as a phone has it; jsdom has none. */
class FakeMediaSession {
    public metadata: { title: string; artwork: { src: string }[] } | null = null;
    public playbackState = 'none';
    public position: MediaPositionState | null = null;
    public readonly handlers = new Map<string, MediaSessionActionHandler>();

    public setActionHandler(action: string, handler: MediaSessionActionHandler | null): void {
        if (handler) {
            this.handlers.set(action, handler);
        } else {
            this.handlers.delete(action);
        }
    }

    public setPositionState(state: MediaPositionState): void {
        this.position = state;
    }
}

describe('MediaSessionService', () => {
    let session: FakeMediaSession;

    beforeEach(() => {
        session = new FakeMediaSession();
        Object.defineProperty(navigator, 'mediaSession', { value: session, configurable: true });
        vi.stubGlobal(
            'MediaMetadata',
            class {
                public constructor(init: object) {
                    Object.assign(this, init);
                }
            },
        );
    });

    afterEach(() => {
        delete (navigator as { mediaSession?: unknown }).mediaSession;
        vi.unstubAllGlobals();
    });

    function media(): HTMLMediaElement {
        const element = document.createElement('audio');
        Object.defineProperty(element, 'duration', { value: 100 });
        Object.defineProperty(element, 'paused', { value: false });
        element.currentTime = 50;
        return element;
    }

    it('shows what plays and lets the lock screen drive it', () => {
        const service = new MediaSessionService();
        const element = media();
        service.attach(element, { title: 'Песня', artworkSrc: '/media/a/thumbnail.jpg' });

        expect(session.metadata?.title).toBe('Песня');
        expect(session.metadata?.artwork[0].src).toBe(
            new URL('/media/a/thumbnail.jpg', location.href).href,
        );
        expect(session.playbackState).toBe('playing');
        expect(session.position).toEqual({ duration: 100, position: 50, playbackRate: 1 });

        session.handlers.get('seekbackward')!({ action: 'seekbackward' });
        expect(element.currentTime).toBe(40);
        session.handlers.get('seekforward')!({ action: 'seekforward', seekOffset: 30 });
        expect(element.currentTime).toBe(70);
        session.handlers.get('seekto')!({ action: 'seekto', seekTime: 5 });
        expect(element.currentTime).toBe(5);
    });

    it('forgets the player once it closes, but not a newer one', () => {
        const service = new MediaSessionService();
        const old = media();
        const current = media();
        service.attach(old, { title: 'old', artworkSrc: null });
        service.attach(current, { title: 'current', artworkSrc: null });

        service.detach(old);
        expect(session.metadata?.title).toBe('current');

        service.detach(current);
        expect(session.metadata).toBeNull();
        expect(session.playbackState).toBe('none');
        expect(session.handlers.size).toBe(0);
    });
});
