import { ComponentFixture, TestBed } from '@angular/core/testing';
import { BehaviorSubject } from 'rxjs';
import { SettingsComponent } from './settings.component';
import { SettingsService } from '../../services/settings.service';
import { ToolInstallService, ToolInstallState } from '../../services/tool-install.service';
import { AccountsService } from '../../services/accounts.service';
import { NotificationService } from '../../services/notification.service';
import { app, model, tools } from '../../wailsjs/wailsjs/go/models';

class SettingsServiceStub {
    public readonly dependencies$ = new BehaviorSubject<tools.Report | null>(null);
    public readonly resetCalls: boolean[] = [];

    public getSettings(): Promise<app.Settings> {
        return Promise.resolve(app.Settings.createFrom({ downloadsDir: '/videos' }));
    }

    public getAppInfo(): Promise<app.AppInfo> {
        return Promise.resolve(app.AppInfo.createFrom({ version: '1.0.0' }));
    }

    public getStorageInfo(): Promise<app.StorageInfo> {
        return Promise.resolve(
            app.StorageInfo.createFrom({
                dataBytes: 2048,
                mediaFiles: 3,
                mediaBytes: 5 * 1024 ** 3,
            }),
        );
    }

    public resetAllData(deleteMedia: boolean): Promise<void> {
        this.resetCalls.push(deleteMedia);
        return new Promise<void>(() => undefined); // the app quits; never resolves
    }
}

class ToolInstallServiceStub {
    public readonly state$ = new BehaviorSubject<ToolInstallState>({
        isRunning: false,
        progress: {},
    });
    public readonly installCalls: (readonly string[])[] = [];

    public install(names: readonly string[] = []): Promise<void> {
        this.installCalls.push(names);
        return Promise.resolve();
    }

    public cancel(): Promise<void> {
        return Promise.resolve();
    }
}

class AccountsServiceStub {
    public readonly accounts$ = new BehaviorSubject<model.Account[]>([]);
    public readonly removed: string[] = [];
    public readonly imported: string[] = [];
    public checkResult = model.Account.createFrom({});

    public refresh(): Promise<model.Account[]> {
        return Promise.resolve(this.accounts$.value);
    }

    public importCookiesFile(site: string): Promise<model.Account | null> {
        this.imported.push(site);
        return Promise.resolve(null);
    }

    public check(): Promise<model.Account> {
        return Promise.resolve(this.checkResult);
    }

    public remove(id: string): Promise<void> {
        this.removed.push(id);
        this.accounts$.next(this.accounts$.value.filter((account) => account.id !== id));
        return Promise.resolve();
    }
}

describe('SettingsComponent: delete all data', () => {
    let fixture: ComponentFixture<SettingsComponent>;
    let service: SettingsServiceStub;

    async function settle(): Promise<void> {
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();
    }

    function element(): HTMLElement {
        return fixture.nativeElement as HTMLElement;
    }

    function clickButton(label: string): void {
        const button = Array.from(element().querySelectorAll('button')).find((candidate) =>
            candidate.textContent?.includes(label),
        );
        if (!button) {
            throw new Error(`no button "${label}"`);
        }
        button.click();
        fixture.detectChanges();
    }

    beforeEach(async () => {
        service = new SettingsServiceStub();
        await TestBed.configureTestingModule({
            imports: [SettingsComponent],
            providers: [
                { provide: SettingsService, useValue: service },
                { provide: ToolInstallService, useValue: new ToolInstallServiceStub() },
                { provide: AccountsService, useValue: new AccountsServiceStub() },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(SettingsComponent);
        fixture.detectChanges();
        await settle();
    });

    it('asks about media files, then asks for final confirmation', async () => {
        clickButton('Удалить всё');
        await settle();

        const option = element().querySelector<HTMLInputElement>(
            'app-confirm-dialog input[type=checkbox]',
        );
        expect(element().textContent).toContain('3 шт., 5.0 ГБ');
        expect(option?.checked).toBe(false);

        option!.click();
        fixture.detectChanges();
        clickButton('Далее');
        await settle();

        expect(element().textContent).toContain('Вы точно хотите удалить все данные?');
        expect(element().textContent).toContain('будут удалены с диска');
        expect(service.resetCalls).toEqual([]);

        clickButton('Удалить и закрыть');
        await settle();
        expect(service.resetCalls).toEqual([true]);
        expect(element().textContent).toContain('Удаление…');
    });

    it('keeps media by default and can be cancelled at the last step', async () => {
        clickButton('Удалить всё');
        await settle();
        clickButton('Далее');
        await settle();

        expect(element().textContent).toContain('останутся в папке /videos');
        clickButton('Отмена');
        await settle();

        expect(element().querySelector('app-confirm-dialog')).toBeNull();
        expect(service.resetCalls).toEqual([]);
    });
});

describe('SettingsComponent: installing tools', () => {
    let fixture: ComponentFixture<SettingsComponent>;
    let service: SettingsServiceStub;
    let installer: ToolInstallServiceStub;

    beforeEach(async () => {
        service = new SettingsServiceStub();
        installer = new ToolInstallServiceStub();
        await TestBed.configureTestingModule({
            imports: [SettingsComponent],
            providers: [
                { provide: SettingsService, useValue: service },
                { provide: ToolInstallService, useValue: installer },
                { provide: AccountsService, useValue: new AccountsServiceStub() },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(SettingsComponent);
        fixture.detectChanges();
        service.dependencies$.next(
            tools.Report.createFrom({
                ready: false,
                tools: [
                    { name: 'yt-dlp', required: true, found: true, path: '/bin/yt-dlp' },
                    {
                        name: 'ffmpeg',
                        required: true,
                        found: false,
                        installable: true,
                        installSource: 'example.org',
                    },
                    { name: 'ffprobe', required: false, found: false, installable: true },
                ],
            }),
        );
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();
    });

    function text(): string {
        return (fixture.nativeElement as HTMLElement).textContent ?? '';
    }

    it('offers to install what is missing', () => {
        expect(text()).toContain('Установить недостающие (2)');
        expect(text()).toContain('источник: example.org');

        const button = Array.from(
            (fixture.nativeElement as HTMLElement).querySelectorAll('button'),
        ).find((candidate) => candidate.textContent?.trim() === 'Установить');
        button!.click();
        expect(installer.installCalls).toEqual([['ffmpeg']]);
    });

    it('shows progress, with ffprobe following ffmpeg', () => {
        installer.state$.next({
            isRunning: true,
            progress: {
                ffmpeg: {
                    tool: 'ffmpeg',
                    stage: 'downloading',
                    downloaded: 1024 * 1024,
                    total: 4 * 1024 * 1024,
                },
            },
        });
        fixture.detectChanges();

        expect(text().match(/Скачивание: 1\.0 МБ из 4\.0 МБ/g)?.length).toBe(2);
        expect(text()).toContain('Отменить установку');
        expect(text()).not.toContain('Установить недостающие');
    });
});

describe('SettingsComponent: accounts', () => {
    let fixture: ComponentFixture<SettingsComponent>;
    let accounts: AccountsServiceStub;
    let notifications: NotificationService;

    function element(): HTMLElement {
        return fixture.nativeElement as HTMLElement;
    }

    async function clickButton(label: string): Promise<void> {
        const button = Array.from(element().querySelectorAll('button')).find((candidate) =>
            candidate.textContent?.includes(label),
        );
        if (!button) {
            throw new Error(`no button "${label}"`);
        }
        button.click();
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();
    }

    beforeEach(async () => {
        accounts = new AccountsServiceStub();
        accounts.accounts$.next([
            model.Account.createFrom({
                id: 'a1',
                domain: 'instagram.com',
                source: 'file',
                cookieCount: 14,
                loginKnown: true,
                loggedIn: true,
                status: 'ok',
                refreshedAt: new Date(Date.now() - 26 * 3600 * 1000).toISOString(),
            }),
            model.Account.createFrom({
                id: 'a2',
                domain: 'example.org',
                source: 'file',
                cookieCount: 3,
                loginKnown: false,
                status: 'expired',
            }),
        ]);
        await TestBed.configureTestingModule({
            imports: [SettingsComponent],
            providers: [
                { provide: SettingsService, useValue: new SettingsServiceStub() },
                { provide: ToolInstallService, useValue: new ToolInstallServiceStub() },
                { provide: AccountsService, useValue: accounts },
            ],
        }).compileComponents();

        notifications = TestBed.inject(NotificationService);
        fixture = TestBed.createComponent(SettingsComponent);
        fixture.detectChanges();
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();
    });

    it('lists accounts with their state', () => {
        const text = element().textContent ?? '';
        expect(text).toContain('instagram.com');
        expect(text).toContain('вход есть');
        expect(text).toContain('обновлено вчера');
        expect(text).toContain('example.org');
        expect(text).toContain('вход истёк');
    });

    it('asks before disconnecting an account', async () => {
        await clickButton('Отключить');
        expect(element().textContent).toContain('Отключить instagram.com?');
        expect(accounts.removed).toEqual([]);

        const dialog = element().querySelector('app-confirm-dialog')!;
        const confirm = Array.from(dialog.querySelectorAll('button')).find(
            (button) => button.textContent?.trim() === 'Отключить',
        );
        confirm!.click();
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();

        expect(accounts.removed).toEqual(['a1']);
        expect(element().textContent).not.toContain('instagram.com');
    });

    it('explains what is stored before the file is picked', async () => {
        await clickButton('Загрузить новый');
        const dialog = element().querySelector('app-account-connect-dialog');
        expect(dialog?.textContent).toContain('Вход на instagram.com');
        expect(dialog?.textContent).toContain('в зашифрованном виде');
        expect(dialog?.textContent).toContain('не от программ');
        expect(accounts.imported).toEqual([]);

        await clickButton('Выбрать cookies.txt');
        expect(accounts.imported).toEqual(['instagram.com']);
    });

    it('requires a site when adding one', async () => {
        const errors: string[] = [];
        const original = notifications.error.bind(notifications);
        notifications.error = (error: unknown): void => {
            errors.push(String(error));
            original(error);
        };

        await clickButton('Добавить сайт');
        await clickButton('Выбрать cookies.txt');
        expect(accounts.imported).toEqual([]);
        expect(errors.length).toBe(1);

        const input = element().querySelector<HTMLInputElement>('.account-connect__input')!;
        input.value = 'https://vk.com/video-1_2';
        input.dispatchEvent(new Event('input'));
        await clickButton('Выбрать cookies.txt');
        expect(accounts.imported).toEqual(['https://vk.com/video-1_2']);
    });
});
