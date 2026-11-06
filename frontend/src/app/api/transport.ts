import { Injectable } from '@angular/core';
import { Observable } from 'rxjs';
import { WailsTransport } from './wails-transport';

/**
 * How the frontend reaches the backend. Services call backend methods by
 * name and listen to its events through this class only, so the same
 * services work in the desktop app (WailsTransport) and in a browser
 * talking to the server (HttpTransport, to come).
 *
 * Used as the DI token too. The desktop transport is the default; a
 * browser build will provide HttpTransport in its app config instead.
 */
@Injectable({ providedIn: 'root', useFactory: (): Transport => new WailsTransport() })
export abstract class Transport {
    /** Calls a backend method (a method of internal/app.App) by name. */
    public abstract call<T>(method: string, args?: readonly unknown[]): Promise<T>;

    /** A backend event; unsubscribing stops listening. */
    public abstract events$<T>(name: string): Observable<T>;
}
