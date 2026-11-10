import { ComponentFixture, TestBed } from '@angular/core/testing';
import { BehaviorSubject } from 'rxjs';
import { model } from '../../api/models';
import { AccountsService } from '../../services/accounts.service';
import { NotificationService } from '../../services/notification.service';
import { LocalAccountsComponent } from './local-accounts.component';

function account(id: string, domain: string): model.Account {
    return { id, domain, browserName: 'Firefox', profile: 'default' } as model.Account;
}

class AccountsServiceStub {
    public readonly accounts$ = new BehaviorSubject<model.Account[]>([
        account('srv-1', 'youtube.com'),
    ]);
    public local: model.Account[] = [
        account('loc-1', 'youtube.com'),
        account('loc-2', 'instagram.com'),
    ];
    public sent: string[] = [];
    public sendError: Error | null = null;

    public async localAccounts(): Promise<model.Account[]> {
        return this.local;
    }

    public async sendToServer(id: string): Promise<model.Account> {
        this.sent.push(id);
        if (this.sendError) {
            throw this.sendError;
        }
        return account('srv-2', 'instagram.com');
    }
}

describe('LocalAccountsComponent', () => {
    let service: AccountsServiceStub;
    let notifications: { info: string[]; errors: unknown[] };
    let fixture: ComponentFixture<LocalAccountsComponent>;

    function text(): string {
        return (fixture.nativeElement as HTMLElement).textContent ?? '';
    }

    function buttons(): HTMLButtonElement[] {
        return Array.from((fixture.nativeElement as HTMLElement).querySelectorAll('button'));
    }

    async function settle(): Promise<void> {
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();
    }

    beforeEach(async () => {
        service = new AccountsServiceStub();
        notifications = { info: [], errors: [] };
        await TestBed.configureTestingModule({
            imports: [LocalAccountsComponent],
            providers: [
                { provide: AccountsService, useValue: service },
                {
                    provide: NotificationService,
                    useValue: {
                        info: (message: string): number => notifications.info.push(message),
                        error: (error: unknown): number => notifications.errors.push(error),
                    },
                },
            ],
        }).compileComponents();
        fixture = TestBed.createComponent(LocalAccountsComponent);
        fixture.detectChanges();
        await settle();
    });

    it("offers this computer's sites, telling which the server has", () => {
        expect(text()).toContain('instagram.com');
        expect(buttons().map((button) => button.textContent?.trim())).toEqual([
            'Обновить на сервере',
            'Отправить на сервер',
        ]);
    });

    it('sends a site and reports the result', async () => {
        buttons()[1].click();
        await settle();
        expect(service.sent).toEqual(['loc-2']);
        expect(notifications.info).toEqual(['instagram.com: cookies отправлены на сервер']);

        service.sendError = new Error('сервер недоступен');
        buttons()[0].click();
        await settle();
        expect(notifications.errors).toEqual([service.sendError]);
        expect(buttons()[0].disabled).toBe(false);
    });

    it('shows nothing when no site is connected here', async () => {
        service.local = [];
        fixture = TestBed.createComponent(LocalAccountsComponent);
        fixture.detectChanges();
        await settle();
        expect(text().trim()).toBe('');
    });
});
