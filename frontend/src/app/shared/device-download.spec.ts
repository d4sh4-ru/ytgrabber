import { model } from '../api/models';
import { deviceDownloadUrl, meteredDownloadWarning } from './device-download';

describe('device downloads', () => {
    const item = model.VideoRecord.createFrom({
        id: 'v',
        mediaUrl: '/media/v/file.mp4',
        sizeBytes: 300 * 1024 * 1024,
    });

    function connect(connection: object | undefined): void {
        Object.defineProperty(navigator, 'connection', { value: connection, configurable: true });
    }

    afterEach(() => connect(undefined));

    it('asks the server for an attachment', () => {
        expect(deviceDownloadUrl(item)).toBe('/media/v/file.mp4?download=1&ngsw-bypass=1');
    });

    it('warns about the size on mobile data or with data saver on', () => {
        connect({ type: 'cellular' });
        expect(meteredDownloadWarning(item)).toContain('300 МБ');
        connect({ type: 'wifi', saveData: true });
        expect(meteredDownloadWarning(item)).toContain('300 МБ');
    });

    it("doesn't warn on wi-fi, without connection info or without a size", () => {
        connect({ type: 'wifi' });
        expect(meteredDownloadWarning(item)).toBeNull();
        connect(undefined);
        expect(meteredDownloadWarning(item)).toBeNull();
        connect({ type: 'cellular' });
        expect(meteredDownloadWarning({ ...item, sizeBytes: 0 } as model.VideoRecord)).toBeNull();
    });
});
