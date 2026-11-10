import { Injectable } from '@angular/core';
import { Observable } from 'rxjs';
import type { CodeCheck, SessionInfo, SignInRequest } from './session';
import { desktopConnection } from './connection';
import { HttpTransport } from './http-transport';
import { RemoteTransport } from './remote-transport';
import { WailsTransport } from './wails-transport';

/** Wails injects its bindings as window.go before the app's scripts run. */
export function isWails(): boolean {
    return typeof (window as { go?: unknown }).go === 'object';
}

function createTransport(): Transport {
    if (!isWails()) {
        return new HttpTransport();
    }
    return desktopConnection()?.server ? new RemoteTransport() : new WailsTransport();
}

/**
 * How the frontend reaches the backend. Services call backend methods by
 * name and listen to its events through this class only, so the same
 * services work in the desktop app (WailsTransport) and in a browser
 * talking to ytgrabber-server (HttpTransport). The desktop app connected
 * to a server is a mix of both (RemoteTransport).
 *
 * Used as the DI token too: one of the desktop transports inside Wails,
 * by the mode main.ts found out, the HTTP one everywhere else.
 */
@Injectable({
    providedIn: 'root',
    useFactory: createTransport,
})
export abstract class Transport {
    /**
     * True in the desktop app working with its local library. Native dialogs, the file manager, browser
     * profiles, app updates and "delete all data" exist only there.
     */
    public abstract readonly isDesktop: boolean;

    /**
     * True in the desktop app in either mode, connected to a server too:
     * the app's own updates and its link to the server.
     */
    public abstract readonly isDesktopApp: boolean;

    /**
     * Emits when the event stream (re)connects: events may have been
     * missed before it, so state has to be reloaded.
     */
    public abstract readonly reconnected$: Observable<void>;

    /** Calls a backend method (a method of internal/app.App) by name. */
    public abstract call<T>(method: string, args?: readonly unknown[]): Promise<T>;

    /** A backend event; unsubscribing stops listening. */
    public abstract events$<T>(name: string): Observable<T>;

    /** Who is signed in; the desktop is always its single local user. */
    public abstract session(): Promise<SessionInfo>;

    /** What a code would do on the login screen, without using it up. */
    public abstract checkCode(code: string): Promise<CodeCheck>;

    /** Uses a code up and signs this browser in as a new device. */
    public abstract signIn(request: SignInRequest): Promise<SessionInfo>;

    /** Signs out, revoking this device. */
    public abstract signOut(): Promise<void>;
}
