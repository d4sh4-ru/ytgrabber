import createClient, { Client } from 'openapi-fetch';
import { filter, map, Observable, Subject } from 'rxjs';
import type { paths } from './openapi';
import type { CodeCheck, SessionInfo, SignInRequest } from './session';
import type { Transport } from './transport';

/** Typed client for the server's API, generated from internal/server/openapi.json. */
type Api = Client<paths>;

/** What every openapi-fetch call resolves to. */
interface ApiResult {
    data?: unknown;
    error?: unknown;
    response: Response;
}

/** How one backend method is called over HTTP; args as the desktop binding takes them. */
type Call = (api: Api, args: readonly unknown[]) => Promise<ApiResult>;

type Quality = paths['/jobs']['post']['requestBody']['content']['application/json']['quality'];
type ErrorBody = paths['/jobs']['post']['responses'][400]['content']['application/json'];

/** One message on the events WebSocket. */
interface ServerEvent {
    name: string;
    data: unknown;
}

const API_PREFIX = '/api/v1';

const idOf = (args: readonly unknown[]): { path: { id: string } } => ({
    path: { id: String(args[0]) },
});

type Role = NonNullable<
    paths['/users/invites']['post']['requestBody']['content']['application/json']['role']
>;

/**
 * The backend methods the server has (internal/server/routes.go). The
 * rest exist only in the desktop app; the UI hides them on a server.
 */
const CALLS: Readonly<Record<string, Call>> = {
    GetAppInfo: (api) => api.GET('/info'),
    GetSettings: (api) => api.GET('/settings'),
    SetMaxConcurrentDownloads: (api, [value]) =>
        api.PUT('/settings/max-concurrent-downloads', { body: { value: Number(value) } }),

    GetDependencies: (api) => api.GET('/tools'),
    InstallTools: (api, [names]) =>
        api.POST('/tools/install', { body: { names: [...(names as readonly string[])] } }),
    CancelToolInstall: (api) => api.POST('/tools/install/cancel'),
    UpdateYtDlp: (api) => api.POST('/tools/yt-dlp/update'),
    RollbackYtDlp: (api) => api.POST('/tools/yt-dlp/rollback'),

    DownloadVideo: (api, [url, quality, useAccount]) =>
        api.POST('/jobs', {
            body: {
                url: String(url),
                quality: quality as Quality,
                useAccount: useAccount === true,
            },
        }),
    GetJobs: (api) => api.GET('/jobs'),
    RetryJob: (api, args) =>
        api.POST('/jobs/{id}/retry', {
            params: idOf(args),
            body: { useAccount: args[1] === true },
        }),
    RemoveJob: (api, args) => api.DELETE('/jobs/{id}', { params: idOf(args) }),

    GetLibrary: (api) => api.GET('/library'),
    DeleteVideo: (api, args) =>
        api.DELETE('/library/{id}', {
            params: { ...idOf(args), query: { deleteFiles: String(args[1] === true) } },
        }),
    SavePlaybackPosition: (api, args) =>
        api.PUT('/library/{id}/position', {
            params: idOf(args),
            body: { seconds: Number(args[1]) },
        }),

    // Devices and users: server only.
    GetDevices: (api) => api.GET('/devices'),
    CreateApiToken: (api, [name]) => api.POST('/devices', { body: { name: String(name) } }),
    RemoveDevice: (api, args) => api.DELETE('/devices/{id}', { params: idOf(args) }),
    CreateDeviceLink: (api) => api.POST('/devices/link'),
    GetUsers: (api) => api.GET('/users'),
    InviteUser: (api, [role]) => api.POST('/users/invites', { body: { role: role as Role } }),
    CreateUserLink: (api, args) => api.POST('/users/{id}/link', { params: idOf(args) }),
    RemoveUser: (api, args) => api.DELETE('/users/{id}', { params: idOf(args) }),

    GetAccounts: (api) => api.GET('/accounts'),
    CheckAccount: (api, args) => api.POST('/accounts/{id}/check', { params: idOf(args) }),
    RemoveAccount: (api, args) => api.DELETE('/accounts/{id}', { params: idOf(args) }),
    // ImportCookiesFile(site, file): the browser uploads the file the user picked.
    ImportCookiesFile: (api, [site, file]) => {
        if (!(file instanceof File)) {
            return Promise.reject(new Error('Не выбран файл cookies.txt'));
        }
        return api.POST('/accounts', {
            // The spec types a binary field as string; the serializer sends the File.
            body: { site: String(site), file: file as unknown as string },
            bodySerializer: (): FormData => {
                const form = new FormData();
                form.append('site', String(site));
                form.append('file', file);
                return form;
            },
        });
    },
};

const RECONNECT_MIN_MS = 1000;
const RECONNECT_MAX_MS = 30000;

/**
 * The browser transport: backend methods become calls to the REST API of
 * ytgrabber-server, events arrive over one shared WebSocket that
 * reconnects by itself. Auth is the session cookie the login sets, which
 * <video> and <img> send too.
 */
export class HttpTransport implements Transport {
    public readonly isDesktop = false;
    public readonly reconnected$: Observable<void>;

    private readonly api: Api = createClient<paths>({
        baseUrl: location.origin + API_PREFIX,
        credentials: 'same-origin',
        // Looked up per call, so tests can stub the global.
        fetch: (request: Request): Promise<Response> => fetch(request),
    });

    private readonly reconnectedSubject$ = new Subject<void>();
    private readonly eventsSubject$ = new Subject<ServerEvent>();
    private socket: WebSocket | null = null;
    private reconnectDelay = RECONNECT_MIN_MS;

    public constructor() {
        this.reconnected$ = this.reconnectedSubject$.asObservable();
    }

    public async call<T>(method: string, args: readonly unknown[] = []): Promise<T> {
        const call = CALLS[method];
        if (!call) {
            throw new Error(`На сервере это недоступно (${method})`);
        }
        return unwrap<T>(await call(this.api, args));
    }

    public events$<T>(name: string): Observable<T> {
        this.connect();
        return this.eventsSubject$.pipe(
            filter((event) => event.name === name),
            map((event) => event.data as T),
        );
    }

    public async session(): Promise<SessionInfo> {
        return unwrap<SessionInfo>(await this.api.GET('/session'));
    }

    public async checkCode(code: string): Promise<CodeCheck> {
        return unwrap<CodeCheck>(await this.api.POST('/session/code', { body: { code } }));
    }

    public async signIn(request: SignInRequest): Promise<SessionInfo> {
        const session = unwrap<SessionInfo>(await this.api.POST('/session', { body: request }));
        // A socket opened before the login was refused; retry right away.
        this.reconnectDelay = RECONNECT_MIN_MS;
        this.socket?.close();
        return session;
    }

    public async signOut(): Promise<void> {
        unwrap(await this.api.DELETE('/session'));
    }

    // --- Events ---
    private connect(): void {
        if (this.socket) {
            return;
        }
        const scheme = location.protocol === 'https:' ? 'wss' : 'ws';
        const socket = new WebSocket(`${scheme}://${location.host}${API_PREFIX}/events`);
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

/** The response data, or an Error with the server's message. */
function unwrap<T>(result: ApiResult): T {
    if (result.response.ok) {
        return result.data as T;
    }
    // A proxy's HTML error page parses to a string, not an ErrorBody.
    const message = (result.error as Partial<ErrorBody> | undefined)?.error;
    throw new Error(
        typeof message === 'string' && message
            ? message
            : `Ошибка сервера ${result.response.status}`,
    );
}
