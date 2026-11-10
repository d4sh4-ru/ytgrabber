/** The desktop's link to a server, as internal/remote.Status reports it. */
export interface RemoteStatus {
    /** The event stream is open: the server is reachable and the device signed in. */
    connected: boolean;
    /** Why the server can't be reached. */
    error?: string;
    /** Something to know that doesn't stop the work. */
    warning?: string;
}

/** Which mode the desktop window works in (internal/app.Connection). */
export interface DesktopConnection {
    /** The server the window works with; empty in the local mode. */
    server: string;
    /** The server to offer when connecting again. */
    lastServer: string;
    status: RemoteStatus;
}

/** The event the desktop sends when the connection changes. */
export const REMOTE_STATUS_EVENT = 'remote-status';

/** The prefix of the server's events forwarded by the desktop. */
export const REMOTE_EVENT_PREFIX = 'remote:';

let connection: DesktopConnection | null = null;

/**
 * Asks the desktop which mode it works in, before the app starts: the
 * transport depends on it. Outside the desktop app there is nothing to ask.
 */
export async function loadDesktopConnection(): Promise<void> {
    const app = (
        window as { go?: { app?: { App?: { GetConnection?: () => Promise<DesktopConnection> } } } }
    ).go?.app?.App;
    if (!app?.GetConnection) {
        return;
    }
    try {
        connection = await app.GetConnection();
    } catch (err: unknown) {
        console.error('ytgrabber: GetConnection failed, starting in the local mode', err);
    }
}

/** What loadDesktopConnection found; null outside the desktop app. */
export function desktopConnection(): DesktopConnection | null {
    return connection;
}
