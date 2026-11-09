import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Transport } from '../../api/transport';
import { FakeTransport } from '../../testing/fake-transport';
import { BehaviorSubject } from 'rxjs';
import { SettingsComponent } from './settings.component';
import { SettingsService } from '../../services/settings.service';
import { ToolInstallService, ToolInstallState } from '../../services/tool-install.service';
import { AccountsService } from '../../services/accounts.service';
import { NotificationService } from '../../services/notification.service';
import { UpdateProgress, UpdateService } from '../../services/update.service';
import { browsers, core, model, tools } from '../../api/models';

class SettingsServiceStub {
    public readonly dependencies$ = new BehaviorSubject<tools.Report | null>(null);
    public readonly resetCalls: boolean[] = [];
    public readonly updateSettingsCalls: [boolean, boolean][] = [];
    public rollbackCalls = 0;

    public rollbackYtDlp(): Promise<tools.Report> {
        this.rollbackCalls++;
        return Promise.resolve(
            tools.Report.createFrom({
                ready: true,
                tools: [{ name: 'yt-dlp', found: true, managed: true, version: '2026.01.01' }],
            }),
        );
    }

    public setUpdateSettings(autoCheck: boolean, prerelease: boolean): Promise<core.Settings> {
        this.updateSettingsCalls.push([autoCheck, prerelease]);
        return Promise.resolve(
            core.Settings.createFrom({
                downloadsDir: '/videos',
                autoCheckUpdates: autoCheck,
                prereleaseUpdates: prerelease,
            }),
        );
    }

    public getSettings(): Promise<core.Settings> {
        return Promise.resolve(
            core.Settings.createFrom({ downloadsDir: '/videos', autoCheckUpdates: true }),
        );
    }

    public getAppInfo(): Promise<core.AppInfo> {
        return Promise.resolve(core.AppInfo.createFrom({ version: '1.0.0' }));
    }

    public storageRequests = 0;

    public getStorageInfo(): Promise<core.StorageInfo> {
        this.storageRequests++;
        return Promise.resolve(
            core.StorageInfo.createFrom({
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
    public readonly connected: string[] = [];
    public readonly refreshed: string[] = [];
    public checkResult = model.Account.createFrom({});
    public matches: browsers.Match[] = [];

    public refresh(): Promise<model.Account[]> {
        return Promise.resolve(this.accounts$.value);
    }

    public readonly importedFiles: (File | undefined)[] = [];
    public scans = 0;

    public importCookiesFile(site: string, file?: File): Promise<model.Account | null> {
        this.imported.push(site);
        this.importedFiles.push(file);
        return Promise.resolve(null);
    }

    public scanBrowsers(): Promise<browsers.Match[]> {
        this.scans++;
        return Promise.resolve(this.matches);
    }

    public connectProfile(site: string, match: browsers.Match): Promise<model.Account> {
        this.connected.push(`${site} ${match.browserId} ${match.profileDir}`);
        return Promise.resolve(model.Account.createFrom({ domain: site, cookieCount: 1 }));
    }

    public refreshFromBrowser(id: string): Promise<model.Account> {
        this.refreshed.push(id);
        return Promise.resolve(model.Account.createFrom({ domain: 'vk.com', cookieCount: 2 }));
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

class UpdateServiceStub {
    public readonly info$ = new BehaviorSubject<core.UpdateInfo | null>(null);
    public readonly progress$ = new BehaviorSubject<UpdateProgress | null>(null);
    public checkResult = core.UpdateInfo.createFrom({ available: false });
    public installs = 0;

    public check(): Promise<core.UpdateInfo> {
        this.info$.next(this.checkResult);
        return Promise.resolve(this.checkResult);
    }

    public install(): Promise<void> {
        this.installs++;
        this.progress$.next({ stage: 'downloading', done: 1024 * 1024, total: 2 * 1024 * 1024 });
        return new Promise<void>(() => undefined); // the app restarts
    }

    public openReleasePage(): Promise<void> {
        return Promise.resolve();
    }
}

describe('SettingsComponent: delete all data', () => {
    let transport: FakeTransport;
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
        transport = new FakeTransport();
        service = new SettingsServiceStub();
        await TestBed.configureTestingModule({
            imports: [SettingsComponent],
            providers: [
                { provide: Transport, useValue: transport },
                { provide: SettingsService, useValue: service },
                { provide: ToolInstallService, useValue: new ToolInstallServiceStub() },
                { provide: AccountsService, useValue: new AccountsServiceStub() },
                { provide: UpdateService, useValue: new UpdateServiceStub() },
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
    let transport: FakeTransport;
    let fixture: ComponentFixture<SettingsComponent>;
    let service: SettingsServiceStub;
    let installer: ToolInstallServiceStub;

    beforeEach(async () => {
        transport = new FakeTransport();
        service = new SettingsServiceStub();
        installer = new ToolInstallServiceStub();
        await TestBed.configureTestingModule({
            imports: [SettingsComponent],
            providers: [
                { provide: Transport, useValue: transport },
                { provide: SettingsService, useValue: service },
                { provide: ToolInstallService, useValue: installer },
                { provide: AccountsService, useValue: new AccountsServiceStub() },
                { provide: UpdateService, useValue: new UpdateServiceStub() },
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

    it('offers to roll yt-dlp back only to a kept version', async () => {
        expect(text()).not.toContain('Вернуть');

        service.dependencies$.next(
            tools.Report.createFrom({
                ready: true,
                tools: [
                    {
                        name: 'yt-dlp',
                        found: true,
                        managed: true,
                        version: '2026.02.02',
                        previousVersion: '2026.01.01',
                    },
                ],
            }),
        );
        fixture.detectChanges();
        const button = Array.from(
            (fixture.nativeElement as HTMLElement).querySelectorAll('button'),
        ).find((candidate) => candidate.textContent?.trim() === 'Вернуть 2026.01.01');
        button!.click();
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();

        expect(service.rollbackCalls).toBe(1);
        expect(text()).toContain('Возвращена версия yt-dlp 2026.01.01');
    });
});

describe('SettingsComponent: accounts', () => {
    let transport: FakeTransport;
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
        transport = new FakeTransport();
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
            model.Account.createFrom({
                id: 'a3',
                domain: 'vk.com',
                source: 'browser',
                browser: 'firefox',
                browserName: 'Firefox',
                profile: 'default-release',
                cookieCount: 5,
                loginKnown: true,
                loggedIn: true,
                status: 'ok',
            }),
        ]);
        accounts.matches = [
            browsers.Match.createFrom({
                browserId: 'firefox',
                browser: 'Firefox',
                profile: 'default-release',
                profileDir: '/home/u/.mozilla/firefox/abc',
                cookieCount: 4,
                loginKnown: true,
                loggedIn: true,
                connectable: true,
            }),
            browsers.Match.createFrom({
                browserId: 'chrome',
                browser: 'Chrome',
                profile: 'Работа',
                profileDir: '/home/u/.config/google-chrome/Profile 2',
                cookieCount: 9,
                loginKnown: true,
                loggedIn: true,
                connectable: false,
                note: 'Chrome на Windows шифрует cookies так, что их может прочитать только он сам',
            }),
        ];
        await TestBed.configureTestingModule({
            imports: [SettingsComponent],
            providers: [
                { provide: Transport, useValue: transport },
                { provide: SettingsService, useValue: new SettingsServiceStub() },
                { provide: ToolInstallService, useValue: new ToolInstallServiceStub() },
                { provide: AccountsService, useValue: accounts },
                { provide: UpdateService, useValue: new UpdateServiceStub() },
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
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();
        const dialog = element().querySelector('app-account-connect-dialog');
        expect(dialog?.textContent).toContain('Вход на instagram.com');
        expect(dialog?.textContent).toContain('в зашифрованном виде');
        expect(dialog?.textContent).toContain('не от программ');
        expect(accounts.imported).toEqual([]);

        await clickButton('Выбрать cookies.txt');
        expect(accounts.imported).toEqual(['instagram.com']);
    });

    it('shows where the login was found and connects a Firefox profile', async () => {
        await clickButton('Загрузить новый');
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();

        const dialog = element().querySelector('app-account-connect-dialog')!;
        expect(dialog.textContent).toContain('Firefox · default-release');
        expect(dialog.textContent).toContain('Chrome · Работа');
        expect(dialog.textContent).toContain('может прочитать только он сам');
        expect(dialog.textContent).toContain('только имена cookies');
        // Only the Firefox profile can be connected directly.
        const connect = Array.from(dialog.querySelectorAll('button')).filter(
            (button) => button.textContent?.trim() === 'Подключить',
        );
        expect(connect.length).toBe(1);

        connect[0].click();
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();
        expect(accounts.connected).toEqual(['instagram.com firefox /home/u/.mozilla/firefox/abc']);
        expect(element().querySelector('app-account-connect-dialog')).toBeNull();
    });

    it('refreshes a browser account in one click', async () => {
        expect(element().textContent).toContain('Firefox · default-release');
        await clickButton('Обновить');
        expect(accounts.refreshed).toEqual(['a3']);
        expect(element().querySelector('app-account-connect-dialog')).toBeNull();
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

describe('SettingsComponent: updates', () => {
    let transport: FakeTransport;
    let fixture: ComponentFixture<SettingsComponent>;
    let settings: SettingsServiceStub;
    let updates: UpdateServiceStub;

    function element(): HTMLElement {
        return fixture.nativeElement as HTMLElement;
    }

    async function settle(): Promise<void> {
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();
    }

    async function clickButton(label: string): Promise<void> {
        Array.from(element().querySelectorAll('button'))
            .find((candidate) => candidate.textContent?.includes(label))!
            .click();
        await settle();
    }

    beforeEach(async () => {
        transport = new FakeTransport();
        settings = new SettingsServiceStub();
        updates = new UpdateServiceStub();
        await TestBed.configureTestingModule({
            imports: [SettingsComponent],
            providers: [
                { provide: Transport, useValue: transport },
                { provide: SettingsService, useValue: settings },
                { provide: ToolInstallService, useValue: new ToolInstallServiceStub() },
                { provide: AccountsService, useValue: new AccountsServiceStub() },
                { provide: UpdateService, useValue: updates },
            ],
        }).compileComponents();
        fixture = TestBed.createComponent(SettingsComponent);
        fixture.detectChanges();
        await settle();
    });

    it('reports that the app is up to date', async () => {
        await clickButton('Проверить обновления');
        expect(element().textContent).toContain('Установлена последняя версия');
    });

    it('shows the changes and installs with progress', async () => {
        updates.checkResult = core.UpdateInfo.createFrom({
            available: true,
            version: '1.3.0',
            canInstall: true,
            notes: '* вход через браузер',
        });
        await clickButton('Проверить обновления');
        expect(element().textContent).toContain('Доступна версия 1.3.0');
        expect(element().textContent).toContain('* вход через браузер');

        await clickButton('Обновить и перезапустить');
        expect(updates.installs).toBe(1);
        expect(element().textContent).toContain('Скачивание: 1.0 МБ из 2.0 МБ');
    });

    it('explains why an update has to be installed by hand', async () => {
        updates.checkResult = core.UpdateInfo.createFrom({
            available: true,
            version: '1.3.0',
            canInstall: false,
            installNote: 'нет прав на запись в папку /Applications',
        });
        await clickButton('Проверить обновления');
        expect(element().textContent).toContain('нет прав на запись в папку /Applications');
        expect(element().textContent).not.toContain('Обновить и перезапустить');
        expect(element().textContent).toContain('Страница релиза');
    });

    it('saves the update preferences', async () => {
        const boxes = element().querySelectorAll<HTMLInputElement>('.settings__check input');
        expect(boxes[0].checked).toBe(true);
        boxes[1].click();
        await settle();
        expect(settings.updateSettingsCalls).toEqual([[true, true]]);
    });
});

describe('SettingsComponent: on a server', () => {
    let fixture: ComponentFixture<SettingsComponent>;
    let settings: SettingsServiceStub;
    let accounts: AccountsServiceStub;

    function text(): string {
        return (fixture.nativeElement as HTMLElement).textContent ?? '';
    }

    async function settle(): Promise<void> {
        await new Promise((resolve) => setTimeout(resolve));
        fixture.detectChanges();
    }

    beforeEach(async () => {
        const transport = new FakeTransport();
        transport.isDesktop = false;
        settings = new SettingsServiceStub();
        accounts = new AccountsServiceStub();
        accounts.accounts$.next([
            model.Account.createFrom({ id: 'b', domain: 'youtube.com', source: 'browser' }),
        ]);
        await TestBed.configureTestingModule({
            imports: [SettingsComponent],
            providers: [
                { provide: Transport, useValue: transport },
                { provide: SettingsService, useValue: settings },
                { provide: ToolInstallService, useValue: new ToolInstallServiceStub() },
                { provide: AccountsService, useValue: accounts },
                { provide: UpdateService, useValue: new UpdateServiceStub() },
            ],
        }).compileComponents();
        fixture = TestBed.createComponent(SettingsComponent);
        fixture.detectChanges();
        await settle();
    });

    it('hides what only the desktop can do', () => {
        for (const label of ['Изменить…', 'Указать…', 'Проверить обновления', 'Удалить всё']) {
            expect(text()).not.toContain(label);
        }
        expect(settings.storageRequests).toBe(0);
    });

    it('refreshes any account with an uploaded cookies.txt', async () => {
        const refresh = Array.from(
            (fixture.nativeElement as HTMLElement).querySelectorAll('button'),
        ).find((button) => button.textContent?.includes('Загрузить новый'));
        refresh!.click();
        await settle();
        expect(text()).not.toContain('Найти в браузерах');
        expect(accounts.scans).toBe(0);

        const input = (fixture.nativeElement as HTMLElement).querySelector<HTMLInputElement>(
            'app-account-connect-dialog input[type=file]',
        )!;
        const file = new File(['# Netscape HTTP Cookie File'], 'cookies.txt');
        Object.defineProperty(input, 'files', { value: [file] });
        input.dispatchEvent(new Event('change'));
        await settle();
        expect(accounts.imported).toEqual(['youtube.com']);
        expect(accounts.importedFiles).toEqual([file]);
    });
});
