import { ComponentFixture, TestBed } from '@angular/core/testing';
import type { Device, UserInfo } from '../../api/session';
import { Transport } from '../../api/transport';
import { SessionService } from '../../services/session.service';
import { FakeTransport } from '../../testing/fake-transport';
import { PeopleComponent } from './people.component';

/** A server transport answering the device and user calls. */
class PeopleTransport extends FakeTransport {
    public readonly calls: string[] = [];
    public devices: Device[] = [
        { id: 'd1', name: 'Mac · Safari', createdAt: '', lastSeenAt: '', current: true },
        { id: 'd2', name: 'iPhone · Safari', createdAt: '', lastSeenAt: '', current: false },
    ];
    public users: UserInfo[] = [
        {
            id: 'local',
            name: 'Анна',
            role: 'admin',
            createdAt: '',
            devices: 2,
            lastSeenAt: '',
            current: true,
        },
        {
            id: 'u2',
            name: 'Борис',
            role: 'user',
            createdAt: '',
            devices: 1,
            lastSeenAt: '',
            current: false,
        },
    ];

    public constructor() {
        super();
        this.isDesktop = false;
    }

    public override call<T>(method: string, args: readonly unknown[] = []): Promise<T> {
        this.calls.push([method, ...args].join(' '));
        switch (method) {
            case 'GetDevices':
                return Promise.resolve(this.devices as T);
            case 'GetUsers':
                return Promise.resolve(this.users as T);
            case 'InviteUser':
                return Promise.resolve({
                    code: 'ABCDE-FGHJK',
                    expiresAt: '2026-11-11T10:00:00Z',
                } as T);
            case 'RemoveDevice':
                this.devices = this.devices.filter((device) => device.id !== args[0]);
                return Promise.resolve(undefined as T);
        }
        return super.call<T>(method, args);
    }
}

describe('PeopleComponent', () => {
    let transport: PeopleTransport;

    async function render(role: 'admin' | 'user'): Promise<ComponentFixture<PeopleComponent>> {
        transport = new PeopleTransport();
        transport.sessionInfo = {
            authenticated: true,
            user: { id: 'local', name: 'Анна', role, createdAt: '' },
            deviceId: 'd1',
        };
        TestBed.configureTestingModule({
            imports: [PeopleComponent],
            providers: [{ provide: Transport, useValue: transport }],
        });
        await TestBed.inject(SessionService).load();
        const fixture = TestBed.createComponent(PeopleComponent);
        fixture.detectChanges();
        await settle(fixture);
        return fixture;
    }

    async function settle(fixture: ComponentFixture<PeopleComponent>): Promise<void> {
        await new Promise((resolve) => setTimeout(resolve));
        await fixture.whenStable();
        fixture.detectChanges();
    }

    function text(fixture: ComponentFixture<PeopleComponent>): string {
        return (fixture.nativeElement as HTMLElement).textContent ?? '';
    }

    function button(fixture: ComponentFixture<PeopleComponent>, label: string): HTMLButtonElement {
        const found = [
            ...(fixture.nativeElement as HTMLElement).querySelectorAll<HTMLButtonElement>('button'),
        ].find((candidate) => candidate.textContent?.trim() === label);
        if (!found) {
            throw new Error(`no button ${label}`);
        }
        return found;
    }

    it('shows an admin the users and makes invites with a QR code', async () => {
        const fixture = await render('admin');
        expect(text(fixture)).toContain('iPhone · Safari');
        expect(text(fixture)).toContain('Борис');

        button(fixture, '+ Пригласить').click();
        await settle(fixture);
        const element = fixture.nativeElement as HTMLElement;
        expect(element.querySelector('.people__code')?.textContent).toContain('ABCDE-FGHJK');
        expect(element.querySelector<HTMLImageElement>('.people__qr')?.getAttribute('src')).toBe(
            '/api/v1/qr?text=' + encodeURIComponent(`${location.origin}/?code=ABCDE-FGHJK`),
        );
        expect(transport.calls).toContain('InviteUser user');
    });

    it('hides users from a regular user', async () => {
        const fixture = await render('user');
        expect(text(fixture)).not.toContain('Пользователи');
        expect(transport.calls).not.toContain('GetUsers');
    });

    it('revokes another device after confirming', async () => {
        const fixture = await render('user');
        button(fixture, 'Отозвать').click();
        fixture.detectChanges();
        const confirm = (fixture.nativeElement as HTMLElement).querySelector<HTMLButtonElement>(
            'app-confirm-dialog .btn--destructive',
        );
        confirm!.click();
        await settle(fixture);
        expect(transport.calls).toContain('RemoveDevice d2');
        expect(text(fixture)).not.toContain('iPhone · Safari');
    });
});
