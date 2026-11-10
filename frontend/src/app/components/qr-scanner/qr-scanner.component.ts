import {
    AfterViewInit,
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    ElementRef,
    EventEmitter,
    inject,
    OnDestroy,
    Output,
    ViewChild,
} from '@angular/core';
import { IconComponent } from '../icon/icon.component';

/** The browser's own QR reader (Chrome on Android); Safari has none. */
interface BarcodeDetectorLike {
    detect(source: CanvasImageSource): Promise<{ rawValue: string }[]>;
}

interface BarcodeDetectorClass {
    new (options: { formats: string[] }): BarcodeDetectorLike;
    getSupportedFormats(): Promise<string[]>;
}

type Decode = (video: HTMLVideoElement) => Promise<string | null>;

const SCAN_INTERVAL_MS = 250;
/** Frames are scaled down to this for jsQR: enough for a code on screen, and fast. */
const MAX_FRAME_SIDE = 640;

/** Whether this browser can show the camera at all (needs HTTPS). */
export function canScanQr(): boolean {
    return typeof navigator.mediaDevices?.getUserMedia === 'function';
}

/**
 * Full-screen camera that reads one QR code and reports its text. Uses
 * the browser's BarcodeDetector where there is one, and jsQR (loaded
 * only when needed) elsewhere, as on iPhones.
 */
@Component({
    selector: 'app-qr-scanner',
    standalone: true,
    imports: [IconComponent],
    templateUrl: './qr-scanner.component.html',
    styleUrl: './qr-scanner.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class QrScannerComponent implements AfterViewInit, OnDestroy {
    // --- Outputs ---
    @Output()
    public readonly scanned = new EventEmitter<string>();

    @Output()
    public readonly closed = new EventEmitter<void>();

    // --- DI / view ---
    private readonly changeDetectorRef = inject(ChangeDetectorRef);

    @ViewChild('video')
    public videoRef?: ElementRef<HTMLVideoElement>;

    // --- Camera ---
    protected error = '';
    private stream: MediaStream | null = null;
    private timer: ReturnType<typeof setInterval> | null = null;
    private isDecoding = false;
    private isDone = false;

    // --- Lifecycle ---
    public async ngAfterViewInit(): Promise<void> {
        try {
            this.stream = await navigator.mediaDevices.getUserMedia({
                video: { facingMode: 'environment' },
                audio: false,
            });
        } catch {
            this.fail('Нет доступа к камере. Разрешите его в настройках браузера или введите код.');
            return;
        }
        if (this.isDone) {
            this.stopCamera(); // closed while asking for the camera
            return;
        }
        const video = this.videoRef?.nativeElement;
        if (!video) {
            return;
        }
        video.srcObject = this.stream;
        await video.play().catch((): void => undefined);
        const decode = await this.createDecoder();
        this.timer = setInterval((): void => void this.tick(video, decode), SCAN_INTERVAL_MS);
    }

    public ngOnDestroy(): void {
        this.isDone = true;
        this.stopCamera();
    }

    // --- Actions ---
    protected onClose(): void {
        this.closed.emit();
    }

    // --- Scanning ---
    private async tick(video: HTMLVideoElement, decode: Decode): Promise<void> {
        if (
            this.isDecoding ||
            this.isDone ||
            video.readyState < HTMLMediaElement.HAVE_CURRENT_DATA
        ) {
            return;
        }
        this.isDecoding = true;
        try {
            const text = await decode(video);
            if (text && !this.isDone) {
                this.isDone = true;
                this.stopCamera();
                this.scanned.emit(text);
            }
        } catch {
            // A frame that can't be read: try the next one.
        } finally {
            this.isDecoding = false;
        }
    }

    private async createDecoder(): Promise<Decode> {
        const Detector = (window as { BarcodeDetector?: BarcodeDetectorClass }).BarcodeDetector;
        if (Detector && (await Detector.getSupportedFormats()).includes('qr_code')) {
            const detector = new Detector({ formats: ['qr_code'] });
            return async (video: HTMLVideoElement): Promise<string | null> =>
                (await detector.detect(video))[0]?.rawValue ?? null;
        }

        const { default: jsQR } = await import('jsqr');
        const canvas = document.createElement('canvas');
        const context = canvas.getContext('2d', { willReadFrequently: true });
        return async (video: HTMLVideoElement): Promise<string | null> => {
            const scale = Math.min(
                1,
                MAX_FRAME_SIDE / Math.max(video.videoWidth, video.videoHeight),
            );
            canvas.width = Math.round(video.videoWidth * scale);
            canvas.height = Math.round(video.videoHeight * scale);
            if (!context || !canvas.width || !canvas.height) {
                return null;
            }
            context.drawImage(video, 0, 0, canvas.width, canvas.height);
            const frame = context.getImageData(0, 0, canvas.width, canvas.height);
            return jsQR(frame.data, frame.width, frame.height)?.data ?? null;
        };
    }

    private stopCamera(): void {
        if (this.timer) {
            clearInterval(this.timer);
            this.timer = null;
        }
        this.stream?.getTracks().forEach((track: MediaStreamTrack): void => track.stop());
        this.stream = null;
    }

    private fail(message: string): void {
        this.error = message;
        this.changeDetectorRef.markForCheck();
    }
}
