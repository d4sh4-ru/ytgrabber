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
        vi.stubGlobal('fetch', (url: string, init: RequestInit): Promise<Response> => {
            requests.push({ url, method: init.method ?? 'GET', body: init.body });
            return Promise.resolve(reply);
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
        reply = json(200, { domain: 'youtube.com' });
        const file = new File(['# Netscape HTTP Cookie File'], 'cookies.txt');
        await new HttpTransport().call('ImportCookiesFile', ['youtube.com', file]);

        const form = requests[0].body as FormData;
        expect(requests[0].url).toBe('/api/v1/accounts');
        expect(form.get('site')).toBe('youtube.com');
        expect((form.get('file') as File).name).toBe('cookies.txt');
    });

    it('signs in with the token', async () => {
        const transport = new HttpTransport();
        reply = json(200, { authenticated: false });
        expect(await transport.checkSession()).toBe(false);

        reply = json(401, { error: 'неверный токен' });
        await expect(transport.login('wrong')).rejects.toThrow('неверный токен');
        expect(requests[1]).toEqual({
            url: '/api/v1/session',
            method: 'POST',
            body: JSON.stringify({ token: 'wrong' }),
        });
    });
});
