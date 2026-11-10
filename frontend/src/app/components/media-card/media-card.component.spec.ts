import { TestBed } from '@angular/core/testing';
import { MediaCardComponent } from './media-card.component';
import { model } from '../../api/models';
import { Transport } from '../../api/transport';
import { FakeTransport } from '../../testing/fake-transport';

describe('MediaCardComponent', () => {
    let transport: FakeTransport;

    beforeEach(async () => {
        transport = new FakeTransport();
        await TestBed.configureTestingModule({
            imports: [MediaCardComponent],
            providers: [{ provide: Transport, useValue: transport }],
        }).compileComponents();
    });

    function render(item: Partial<model.VideoRecord>): HTMLElement {
        const fixture = TestBed.createComponent(MediaCardComponent);
        fixture.componentRef.setInput(
            'item',
            model.VideoRecord.createFrom({ id: 'v', title: 'clip', mediaKind: 'video', ...item }),
        );
        fixture.detectChanges();
        return fixture.nativeElement as HTMLElement;
    }

    it('shows how much was watched', () => {
        const element = render({ durationSeconds: 600, positionSeconds: 150 });
        const bar = element.querySelector<HTMLElement>('.media-card__watched-bar');
        expect(bar?.style.width).toBe('25%');
        expect(element.querySelector('.media-card__watched')?.getAttribute('title')).toBe(
            'Остановились на 2:30',
        );
    });

    it('shows no bar for unwatched or finished entries', () => {
        expect(
            render({ durationSeconds: 600, positionSeconds: 0 }).querySelector(
                '.media-card__watched',
            ),
        ).toBeNull();
        expect(
            render({ durationSeconds: 0, positionSeconds: 30 }).querySelector(
                '.media-card__watched',
            ),
        ).toBeNull();
    });

    it('reveals the file on the desktop and downloads it from a browser', () => {
        const desktop = render({ mediaUrl: '/media/v/file.mp4' });
        expect(desktop.querySelector('[aria-label="Показать в папке"]')).not.toBeNull();
        expect(desktop.querySelector('a[download]')).toBeNull();

        transport.isDesktop = false;
        const browser = render({ mediaUrl: '/media/v/file.mp4' });
        expect(browser.querySelector('[aria-label="Показать в папке"]')).toBeNull();
        expect(browser.querySelector('a[download]')?.getAttribute('href')).toBe(
            '/media/v/file.mp4?download=1',
        );
        expect(
            render({ mediaUrl: '/media/v/file.mp4', fileMissing: true }).querySelector('a'),
        ).toBeNull();
    });

    it('asks before downloading over mobile data, without opening the player', () => {
        Object.defineProperty(navigator, 'connection', {
            value: { type: 'cellular' },
            configurable: true,
        });
        try {
            transport.isDesktop = false;
            const fixture = TestBed.createComponent(MediaCardComponent);
            fixture.componentRef.setInput(
                'item',
                model.VideoRecord.createFrom({
                    id: 'v',
                    title: 'clip',
                    mediaKind: 'video',
                    mediaUrl: '/media/v/file.mp4',
                    sizeBytes: 2 * 1024 * 1024 * 1024,
                }),
            );
            const opened = vi.fn();
            fixture.componentInstance.opened.subscribe(opened);
            fixture.detectChanges();
            const element = fixture.nativeElement as HTMLElement;

            const link = element.querySelector<HTMLAnchorElement>('a[download]')!;
            const click = new MouseEvent('click', { bubbles: true, cancelable: true });
            link.dispatchEvent(click);
            fixture.detectChanges();

            expect(click.defaultPrevented).toBe(true);
            expect(element.querySelector('app-confirm-dialog')?.textContent).toContain('2.0 ГБ');

            element.querySelector<HTMLButtonElement>('app-confirm-dialog .btn')!.click();
            fixture.detectChanges();
            expect(element.querySelector('app-confirm-dialog')).toBeNull();
            expect(opened).not.toHaveBeenCalled();
        } finally {
            Object.defineProperty(navigator, 'connection', {
                value: undefined,
                configurable: true,
            });
        }
    });
});
