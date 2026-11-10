import { TestBed } from '@angular/core/testing';
import { model } from '../../api/models';
import { Transport } from '../../api/transport';
import { FakeTransport } from '../../testing/fake-transport';
import { VideoPlayerModalComponent } from './video-player-modal.component';

describe('VideoPlayerModalComponent', () => {
    let transport: FakeTransport;

    beforeEach(async () => {
        transport = new FakeTransport();
        await TestBed.configureTestingModule({
            imports: [VideoPlayerModalComponent],
            providers: [{ provide: Transport, useValue: transport }],
        }).compileComponents();
    });

    function render(): { element: HTMLElement; video: HTMLVideoElement } {
        const fixture = TestBed.createComponent(VideoPlayerModalComponent);
        fixture.componentRef.setInput(
            'item',
            model.VideoRecord.createFrom({
                id: 'v',
                title: 'clip',
                mediaKind: 'video',
                mediaUrl: '/media/v/file.mp4',
            }),
        );
        fixture.detectChanges();
        const element = fixture.nativeElement as HTMLElement;
        const video = element.querySelector('video')!;
        // jsdom lays nothing out and plays nothing: a 300×200 box at the
        // origin, a minute long, a third of the way in.
        video.getBoundingClientRect = () => new DOMRect(0, 0, 300, 200);
        Object.defineProperty(video, 'duration', { value: 60 });
        video.currentTime = 20;
        return { element, video };
    }

    function tap(video: HTMLVideoElement, x: number, at: number, y = 100): void {
        const event = new MouseEvent('pointerup', { clientX: x, clientY: y });
        Object.defineProperties(event, {
            pointerType: { value: 'touch' },
            timeStamp: { value: at },
        });
        video.dispatchEvent(event);
    }

    it('seeks back and forward on a double tap at the sides', () => {
        const { video } = render();
        tap(video, 30, 1000);
        expect(video.currentTime).toBe(20);
        tap(video, 30, 1200);
        expect(video.currentTime).toBe(10);
        // A third quick tap keeps going.
        tap(video, 30, 1400);
        expect(video.currentTime).toBe(0);

        tap(video, 270, 3000);
        tap(video, 270, 3200);
        expect(video.currentTime).toBe(10);
    });

    it('ignores slow taps, the middle and the control bar', () => {
        const { video } = render();
        tap(video, 30, 1000);
        tap(video, 30, 1500);
        tap(video, 150, 3000);
        tap(video, 150, 3100);
        tap(video, 270, 5000, 190);
        tap(video, 270, 5100, 190);
        expect(video.currentTime).toBe(20);
    });

    it('offers the file for download only in a browser', () => {
        expect(render().element.querySelector('a[download]')).toBeNull();

        transport.isDesktop = false;
        expect(render().element.querySelector('a[download]')?.getAttribute('href')).toBe(
            '/media/v/file.mp4?download=1',
        );
    });
});
