import { inject, Injectable } from '@angular/core';
import { BehaviorSubject, Observable } from 'rxjs';
import { Transport } from '../api/transport';

/**
 * How the web app can be put on the home screen: through the browser's
 * own prompt (Chrome, Edge, Android), or by hand on iOS, where Safari
 * has no prompt and the way is Share → Add to Home Screen.
 */
export type InstallOffer = 'prompt' | 'ios';

/** Chrome's install prompt; not in TypeScript's DOM types. */
interface BeforeInstallPromptEvent extends Event {
    prompt(): Promise<void>;
}

const DISMISSED_KEY = 'ytgrabber.installOfferDismissed';

/** Already running from the home screen. */
function isInstalled(): boolean {
    return (
        window.matchMedia?.('(display-mode: standalone)').matches === true ||
        (navigator as Navigator & { standalone?: boolean }).standalone === true
    );
}

function isIos(): boolean {
    // iPads ask for the desktop site and say they're a Mac with touch.
    return (
        /iPhone|iPad|iPod/.test(navigator.userAgent) ||
        (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
    );
}

function isDismissed(): boolean {
    try {
        return localStorage.getItem(DISMISSED_KEY) === '1';
    } catch {
        return false;
    }
}

/**
 * Offers to install the web app. Created with the app, before the login
 * screen, since the browser's prompt comes once per page load.
 */
@Injectable({ providedIn: 'root' })
export class InstallService {
    public readonly offer$: Observable<InstallOffer | null>;

    private readonly transport = inject(Transport);
    private readonly offerSubject$ = new BehaviorSubject<InstallOffer | null>(null);
    private deferredPrompt: BeforeInstallPromptEvent | null = null;

    public constructor() {
        this.offer$ = this.offerSubject$.asObservable();
        if (this.transport.isDesktopApp || isInstalled() || isDismissed()) {
            return;
        }
        window.addEventListener('beforeinstallprompt', (event: Event): void => {
            // Keep it for the button instead of the browser's own mini-bar.
            event.preventDefault();
            this.deferredPrompt = event as BeforeInstallPromptEvent;
            this.offerSubject$.next('prompt');
        });
        window.addEventListener('appinstalled', (): void => {
            this.deferredPrompt = null;
            this.offerSubject$.next(null);
        });
        if (isIos()) {
            this.offerSubject$.next('ios');
        }
    }

    /** Shows the browser's install prompt; it can be shown only once. */
    public async install(): Promise<void> {
        const prompt = this.deferredPrompt;
        this.deferredPrompt = null;
        this.offerSubject$.next(null);
        await prompt?.prompt();
    }

    /** Not now, and not on later visits either. */
    public dismiss(): void {
        try {
            localStorage.setItem(DISMISSED_KEY, '1');
        } catch {
            // Private mode: it comes back next time, which is fine.
        }
        this.offerSubject$.next(null);
    }
}
