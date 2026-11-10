import { of } from 'rxjs';
import { vi } from 'vitest';
import { HttpTransport } from './http-transport';
import { RemoteTransport } from './remote-transport';
import { WailsTransport } from './wails-transport';

describe('RemoteTransport', () => {
    let httpCalls: string[];
    let wailsCalls: string[];
    let wailsEvents: string[];

    beforeEach(() => {
        httpCalls = [];
        wailsCalls = [];
        wailsEvents = [];
        vi.spyOn(HttpTransport.prototype, 'call').mockImplementation(async (method: string) => {
            httpCalls.push(method);
            return undefined as never;
        });
        vi.spyOn(WailsTransport.prototype, 'call').mockImplementation(async (method: string) => {
            wailsCalls.push(method);
            return undefined as never;
        });
        vi.spyOn(WailsTransport.prototype, 'events$').mockImplementation((name: string) => {
            wailsEvents.push(name);
            return name === 'remote-status'
                ? (of({ connected: false }, { connected: true }) as never)
                : (of() as never);
        });
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('sends the library to the server and keeps the app itself local', async () => {
        const transport = new RemoteTransport();
        await transport.call('GetLibrary');
        await transport.call('StartDownload', [{}]);
        await transport.call('CheckForUpdates');
        await transport.call('SaveMediaFile', ['/media/a', 'a.mp4']);
        await transport.call('UseLocal');

        expect(httpCalls).toEqual(['GetLibrary', 'StartDownload']);
        expect(wailsCalls).toEqual(['CheckForUpdates', 'SaveMediaFile', 'UseLocal']);
        expect(transport.isDesktop).toBe(false);
        expect(transport.isDesktopApp).toBe(true);
    });

    it("listens to the server's events forwarded by the desktop", () => {
        const transport = new RemoteTransport();
        wailsEvents = [];
        transport.events$('job-progress').subscribe();
        transport.events$('update-available').subscribe();
        transport.events$('remote-status').subscribe();

        expect(wailsEvents).toEqual(['remote:job-progress', 'update-available', 'remote-status']);
    });

    it('reports a reconnect for each "connected" status', () => {
        const transport = new RemoteTransport();
        let reconnects = 0;
        transport.reconnected$.subscribe(() => reconnects++);

        expect(reconnects).toBe(1);
    });
});
