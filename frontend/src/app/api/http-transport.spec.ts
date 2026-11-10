import { vi } from 'vitest';
import { HttpTransport } from './http-transport';

interface Recorded {
    url: string;
    method: string;
    body: unknown;
}

describe('HttpTransport', () => {
    let requests: Recorded[];
    let reply: Response;

    beforeEach(() => {
        requests = [];
        reply = new Response(null, { status: 204 });
        vi.stubGlobal('fetch', async (request: Request): Promise<Response> => {
            const url = new URL(request.url);
            // A multipart body holds jsdom's File, which the test Request
            // can't always stream (it hung in CI): the upload test checks
            // the form itself instead.
            const isForm = request.headers.get('Content-Type')?.startsWith('multipart/');
            const body = isForm ? '' : await request.text();
            requests.push({
                url: url.pathname + url.search,
                method: request.method,
                body: body === '' ? undefined : body,
            });
            return reply;
        });
    });

    afterEach(() => {
        vi.unstubAllGlobals();
    });

    function json(status: number, body: unknown): Response {
        return new Response(JSON.stringify(body), {
            status,
            headers: { 'Content-Type': 'application/json' },
        });
    }

    it('maps backend methods onto the REST API', async () => {
        const transport = new HttpTransport();
        reply = json(200, 'job-1');
        expect(await transport.call<string>('DownloadVideo', ['https://x/v', 'audio', true])).toBe(
            'job-1',
        );
        expect(requests[0]).toEqual({
            url: '/api/v1/jobs',
            method: 'POST',
            body: JSON.stringify({ url: 'https://x/v', quality: 'audio', useAccount: true }),
        });

        reply = new Response(null, { status: 204 });
        await transport.call('DeleteVideo', ['a/b', true]);
        await transport.call('SavePlaybackPosition', ['v1', 42]);
        expect(requests.slice(1).map(({ url, method }) => `${method} ${url}`)).toEqual([
            'DELETE /api/v1/library/a%2Fb?deleteFiles=true',
            'PUT /api/v1/library/v1/position',
        ]);
    });

    it('turns the server error into the message', async () => {
        reply = json(400, { error: 'неизвестное качество "8k"' });
        await expect(new HttpTransport().call('GetJobs')).rejects.toThrow(
            'неизвестное качество "8k"',
        );
    });

    it('reports a non-JSON error page by its status', async () => {
        reply = new Response('<html>Bad Gateway</html>', { status: 502 });
        await expect(new HttpTransport().call('GetJobs')).rejects.toThrow('Ошибка сервера 502');
    });

    it('refuses desktop-only methods without a request', async () => {
        await expect(new HttpTransport().call('RevealVideo', ['v'])).rejects.toThrow(
            'На сервере это недоступно',
        );
        expect(requests).toEqual([]);
    });

    it('uploads cookies.txt as a form', async () => {
        // jsdom's File doesn't survive the test Request's multipart
        // encoding, so check what goes into the form instead.
        const append = vi.spyOn(FormData.prototype, 'append');
        reply = json(200, { domain: 'youtube.com' });
        const file = new File(['# Netscape HTTP Cookie File'], 'cookies.txt');
        await new HttpTransport().call('ImportCookiesFile', ['youtube.com', file]);

        expect(requests[0]).toMatchObject({ url: '/api/v1/accounts', method: 'POST' });
        expect(append.mock.calls).toEqual([
            ['site', 'youtube.com'],
            ['file', file],
        ]);
        append.mockRestore();
    });

    it('refuses an upload without a file', async () => {
        await expect(
            new HttpTransport().call('ImportCookiesFile', ['youtube.com', null]),
        ).rejects.toThrow('Не выбран файл');
        expect(requests).toEqual([]);
    });

    it('signs in with a code', async () => {
        const transport = new HttpTransport();
        reply = json(200, { authenticated: false, setupRequired: true });
        expect(await transport.session()).toEqual({ authenticated: false, setupRequired: true });

        reply = json(401, { error: 'код не подходит или истёк' });
        await expect(
            transport.signIn({ code: 'wrong', name: 'Анна', deviceName: 'Mac' }),
        ).rejects.toThrow('код не подходит');
        expect(requests[1]).toEqual({
            url: '/api/v1/session',
            method: 'POST',
            body: JSON.stringify({ code: 'wrong', name: 'Анна', deviceName: 'Mac' }),
        });
    });

    it('revokes a device by id', async () => {
        reply = new Response(null, { status: 204 });
        await new HttpTransport().call('RemoveDevice', ['d1']);
        expect(requests[0]).toEqual({
            url: '/api/v1/devices/d1',
            method: 'DELETE',
            body: undefined,
        });
    });
});
