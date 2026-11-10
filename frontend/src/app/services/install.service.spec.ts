import { TestBed } from '@angular/core/testing';
import { firstValueFrom } from 'rxjs';
import { Transport } from '../api/transport';
import { FakeTransport } from '../testing/fake-transport';
import { InstallOffer, InstallService } from './install.service';

describe('InstallService', () => {
    let transport: FakeTransport;

    beforeEach(() => {
        localStorage.clear();
        transport = new FakeTransport();
        transport.isDesktop = false;
        TestBed.configureTestingModule({
            providers: [{ provide: Transport, useValue: transport }],
        });
    });

    function offer(service: InstallService): Promise<InstallOffer | null> {
        return firstValueFrom(service.offer$);
    }

    function browserOffersInstall(): { prompt: ReturnType<typeof vi.fn>; event: Event } {
        const event = new Event('beforeinstallprompt', { cancelable: true });
        const prompt = vi.fn(async (): Promise<void> => undefined);
        Object.assign(event, { prompt });
        window.dispatchEvent(event);
        return { prompt, event };
    }

    it("keeps the browser's prompt for the button and shows it once", async () => {
        const service = TestBed.inject(InstallService);
        expect(await offer(service)).toBeNull();

        const { prompt, event } = browserOffersInstall();
        expect(event.defaultPrevented).toBe(true);
        expect(await offer(service)).toBe('prompt');

        await service.install();
        expect(prompt).toHaveBeenCalledTimes(1);
        expect(await offer(service)).toBeNull();
    });

    it('stays quiet once dismissed, on later visits too', async () => {
        TestBed.inject(InstallService);
        browserOffersInstall();
        TestBed.inject(InstallService).dismiss();
        expect(await offer(TestBed.inject(InstallService))).toBeNull();

        TestBed.resetTestingModule();
        TestBed.configureTestingModule({
            providers: [{ provide: Transport, useValue: transport }],
        });
        const later = TestBed.inject(InstallService);
        browserOffersInstall();
        expect(await offer(later)).toBeNull();
    });

    it('offers nothing in the desktop app', async () => {
        transport.isDesktop = true;
        const service = TestBed.inject(InstallService);
        browserOffersInstall();
        expect(await offer(service)).toBeNull();
    });
});
