import { ComponentFixture, TestBed } from '@angular/core/testing';
import { BehaviorSubject } from 'rxjs';
import type { RemoteStatus } from '../../api/connection';
import { ConnectionService } from '../../services/connection.service';
import { ServerConnectionComponent } from './server-connection.component';

class ConnectionServiceStub {
    public server = '';
    public lastServer = 'old.example.com';
    public readonly status$ = new BehaviorSubject<RemoteStatus>({ connected: false });
    public connected: string[] = [];
    public connectError: Error | null = null;
    public usedLocal = 0;

    public async connect(address: string): Promise<void> {
        this.connected.push(address);
        if (this.connectError) {
            throw this.connectError;
        }
    }

    public async useLocal(): Promise<void> {
        this.usedLocal++;
    }

    public async reconnect(): Promise<void> {
        // nothing to do in tests
    }
}

describe('ServerConnectionComponent', () => {
    let service: ConnectionServiceStub;
    let fixture: ComponentFixture<ServerConnectionComponent>;

    function element(): HTMLElement {
        return fixture.nativeElement as HTMLElement;
    }

    function button(label: string): HTMLButtonElement | undefined {
        return Array.from(element().querySelectorAll('button')).find((candidate) =>
            candidate.textContent?.includes(label),
        );
    }

    async function settle(): Promise<void> {
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();
    }

    async function create(): Promise<void> {
        await TestBed.configureTestingModule({
            imports: [ServerConnectionComponent],
            providers: [{ provide: ConnectionService, useValue: service }],
        }).compileComponents();
        fixture = TestBed.createComponent(ServerConnectionComponent);
        fixture.detectChanges();
    }

    beforeEach(() => {
        service = new ConnectionServiceStub();
    });

    it('offers the last server and shows why connecting failed', async () => {
        service.connectError = new Error('по адресу old.example.com не ytgrabber-server');
        await create();

        const input = element().querySelector('input') as HTMLInputElement;
        expect(input.value).toBe('old.example.com');

        button('Подключиться')?.click();
        await settle();

        expect(service.connected).toEqual(['old.example.com']);
        expect(element().textContent).toContain('не ytgrabber-server');
        expect(button('Подключиться')?.disabled).toBe(false);
    });

    it('shows the server and its state, and goes back to local', async () => {
        service.server = 'https://ytg.example.com';
        await create();

        expect(element().textContent).toContain('https://ytg.example.com');
        expect(element().textContent).toContain('Подключение…');
        expect(button('Повторить')).toBeTruthy();

        service.status$.next({ connected: true });
        fixture.detectChanges();
        expect(element().textContent).toContain('На связи');
        expect(button('Повторить')).toBeUndefined();

        button('Работать локально')?.click();
        await settle();
        expect(service.usedLocal).toBe(1);
    });
});
