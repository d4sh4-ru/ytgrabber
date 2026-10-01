import { ComponentFixture, TestBed } from '@angular/core/testing';
import { BehaviorSubject } from 'rxjs';
import { SettingsComponent } from './settings.component';
import { SettingsService } from '../../services/settings.service';
import { app, tools } from '../../wailsjs/wailsjs/go/models';

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
            providers: [{ provide: SettingsService, useValue: service }],
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
