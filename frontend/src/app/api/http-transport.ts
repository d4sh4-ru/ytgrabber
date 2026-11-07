import { filter, map, Observable, Subject } from 'rxjs';
import type { Transport } from './transport';

const API = '/api/v1';

type HttpMethod = 'GET' | 'POST' | 'PUT' | 'DELETE';

/** How a backend method maps onto the server's REST API (internal/server). */
interface Route {
    method: HttpMethod;
    path: (args: readonly unknown[]) => string;
    body?: (args: readonly unknown[]) => unknown;
}

/** One message on the events WebSocket. */
interface ServerEvent {
    name: string;
    data: unknown;
}

interface ErrorBody {
    error?: string;
}

const id = (args: readonly unknown[]): string => encodeURIComponent(String(args[0]));

const ROUTES: Readonly<Record<string, Route>> = {
    GetAppInfo: { method: 'GET', path: () => '/info' },
    GetSettings: { method: 'GET', path: () => '/settings' },
    SetMaxConcurrentDownloads: {
        method: 'PUT',
        path: () => '/settings/max-concurrent-downloads',
        body: (args) => ({ value: args[0] }),
    },

    GetDependencies: { method: 'GET', path: () => '/tools' },
    InstallTools: {
        method: 'POST',
        path: () => '/tools/install',
        body: (args) => ({ names: args[0] }),
    },
    CancelToolInstall: { method: 'POST', path: () => '/tools/install/cancel' },
    UpdateYtDlp: { method: 'POST', path: () => '/tools/yt-dlp/update' },

    DownloadVideo: {
        method: 'POST',
        path: () => '/jobs',
        body: (args) => ({ url: args[0], quality: args[1], useAccount: args[2] }),
    },
    GetJobs: { method: 'GET', path: () => '/jobs' },
    RetryJob: {
        method: 'POST',
        path: (args) => `/jobs/${id(args)}/retry`,
        body: (args) => ({ useAccount: args[1] }),
    },
    RemoveJob: { method: 'DELETE', path: (args) => `/jobs/${id(args)}` },

    GetLibrary: { method: 'GET', path: () => '/library' },
    DeleteVideo: {
        method: 'DELETE',
        path: (args) => `/library/${id(args)}?deleteFiles=${args[1] === true}`,
    },
    SavePlaybackPosition: {
        method: 'PUT',
        path: (args) => `/library/${id(args)}/position`,
        body: (args) => ({ seconds: args[1] }),
    },

    GetAccounts: { method: 'GET', path: () => '/accounts' },
    CheckAccount: { method: 'POST', path: (args) => `/accounts/${id(args)}/check` },
    RemoveAccount: { method: 'DELETE', path: (args) => `/accounts/${id(args)}` },
};

/** The body as JSON; a proxy's HTML or plain-text error page is not. */
function parseJSON(text: string): unknown {
    if (!text) {
        return undefined;
    }
    try {
        return JSON.parse(text) as unknown;
    } catch {
        return undefined;
    }
}

const RECONNECT_MIN_MS = 1000;
const RECONNECT_MAX_MS = 30000;

/**
 * The browser transport: backend methods become REST calls to
 * ytgrabber-server, events arrive over one shared WebSocket that
 * reconnects by itself. Auth is the session cookie the login sets, which
 * <video> and <img> send too.
 */
export class HttpTransport implements Transport {
    public readonly isDesktop = false;
    public readonly reconnected$: Observable<void>;

    private readonly reconnectedSubject$ = new Subject<void>();
    private readonly eventsSubject$ = new Subject<ServerEvent>();
    private socket: WebSocket | null = null;
    private reconnectDelay = RECONNECT_MIN_MS;

    public constructor() {
        this.reconnected$ = this.reconnectedSubject$.asObservable();
    }

    public call<T>(method: string, args: readonly unknown[] = []): Promise<T> {
        if (method === 'ImportCookiesFile') {
            return this.importCookies<T>(args);
        }
        const route = ROUTES[method];
        if (!route) {
            return Promise.reject(new Error(`На сервере это недоступно (${method})`));
        }
        const body = route.body?.(args);
        return this.request<T>(route.method, route.path(args), {
            body: body === undefined ? undefined : JSON.stringify(body),
            headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
        });
    }

    public events$<T>(name: string): Observable<T> {
        this.connect();
        return this.eventsSubject$.pipe(
            filter((event) => event.name === name),
            map((event) => event.data as T),
        );
    }

    public async checkSession(): Promise<boolean> {
        const session = await this.request<{ authenticated: boolean }>('GET', '/session');
        return session.authenticated;
    }

    public async login(token: string): Promise<void> {
        await this.request<unknown>('POST', '/session', {
            body: JSON.stringify({ token }),
            headers: { 'Content-Type': 'application/json' },
        });
        // A socket opened before the login was refused; retry right away.
        this.reconnectDelay = RECONNECT_MIN_MS;
        this.socket?.close();
    }

    // --- Requests ---
    private async request<T>(method: HttpMethod, path: string, init: RequestInit = {}): Promise<T> {
        const response = await fetch(API + path, {
            ...init,
            method,
            credentials: 'same-origin',
        });
        if (response.status === 204) {
            return undefined as T;
        }
        const text = await response.text();
        const payload = parseJSON(text);
        if (!response.ok) {
            const message = (payload as ErrorBody | undefined)?.error;
            throw new Error(message || `Ошибка сервера ${response.status}`);
        }
        return payload as T;
    }

    /** ImportCookiesFile(site, file): the browser uploads the chosen file. */
    private importCookies<T>(args: readonly unknown[]): Promise<T> {
        const [site, file] = args as [string, File | undefined];
        if (!(file instanceof File)) {
            return Promise.reject(new Error('Не выбран файл cookies.txt'));
        }
        const form = new FormData();
        form.append('site', site);
        form.append('file', file);
        return this.request<T>('POST', '/accounts', { body: form });
    }

    // --- Events ---
    private connect(): void {
        if (this.socket) {
            return;
        }
        const scheme = location.protocol === 'https:' ? 'wss' : 'ws';
        const socket = new WebSocket(`${scheme}://${location.host}${API}/events`);
        this.socket = socket;

        socket.onopen = (): void => {
            this.reconnectDelay = RECONNECT_MIN_MS;
            // Also on the first open: events sent before it (while the
            // initial state was loading) never reached this client.
            this.reconnectedSubject$.next();
        };
        socket.onmessage = (message: MessageEvent<string>): void => {
            try {
                this.eventsSubject$.next(JSON.parse(message.data) as ServerEvent);
            } catch {
                // Not an event: ignore rather than break the stream.
            }
        };
        socket.onclose = (): void => {
            this.socket = null;
            const delay = this.reconnectDelay;
            this.reconnectDelay = Math.min(delay * 2, RECONNECT_MAX_MS);
            setTimeout((): void => this.connect(), delay);
        };
    }
}
