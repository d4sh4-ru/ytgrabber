import { filter, map, Observable } from 'rxjs';
import { REMOTE_EVENT_PREFIX, REMOTE_STATUS_EVENT, RemoteStatus } from './connection';
import { HttpTransport } from './http-transport';
import type { CodeCheck, SessionInfo, SignInRequest } from './session';
import type { Transport } from './transport';
import { WailsTransport } from './wails-transport';

/**
 * What stays with the desktop app itself: its updates, its link to the
 * server and the sites connected on this computer.
 */
const LOCAL_METHODS: ReadonlySet<string> = new Set([
    'GetUpdateInfo',
    'CheckForUpdates',
    'InstallUpdate',
    'OpenReleasePage',
    'GetConnection',
    'ConnectServer',
    'UseLocal',
    'ReconnectServer',
    'SaveMediaFile',
    'GetLocalAccounts',
    'SendAccountToServer',
]);

const LOCAL_EVENTS: ReadonlySet<string> = new Set([
    'update-available',
    'update-progress',
    REMOTE_STATUS_EVENT,
]);

/**
 * The desktop app connected to a server: the window is a web client of
 * it. Calls go over HTTP to the window's own origin, which the desktop
 * forwards to the server with the device's token (internal/remote); the
 * server's events arrive through the desktop too, since a WebView's
 * origin can't hold a WebSocket. The app's own updates and the link to
 * the server stay local.
 */
export class RemoteTransport implements Transport {
    public readonly isDesktop = false;
    public readonly isDesktopApp = true;
    public readonly reconnected$: Observable<void>;

    private readonly http = new HttpTransport();
    private readonly wails = new WailsTransport();

    public constructor() {
        // The desktop reports only changes: each "connected" is a
        // reconnect, after which missed events have to be made up for.
        this.reconnected$ = this.wails.events$<RemoteStatus>(REMOTE_STATUS_EVENT).pipe(
            filter((status: RemoteStatus) => status.connected),
            map((): void => undefined),
        );
    }

    public call<T>(method: string, args: readonly unknown[] = []): Promise<T> {
        return LOCAL_METHODS.has(method)
            ? this.wails.call<T>(method, args)
            : this.http.call<T>(method, args);
    }

    public events$<T>(name: string): Observable<T> {
        return this.wails.events$<T>(LOCAL_EVENTS.has(name) ? name : REMOTE_EVENT_PREFIX + name);
    }

    public session(): Promise<SessionInfo> {
        return this.http.session();
    }

    public checkCode(code: string): Promise<CodeCheck> {
        return this.http.checkCode(code);
    }

    public signIn(request: SignInRequest): Promise<SessionInfo> {
        return this.http.signIn(request);
    }

    public signOut(): Promise<void> {
        return this.http.signOut();
    }
}
