import { ComponentFixture, TestBed } from '@angular/core/testing';
import { JobCardComponent } from './job-card.component';
import { model } from '../../wailsjs/wailsjs/go/models';

describe('JobCardComponent', () => {
    let fixture: ComponentFixture<JobCardComponent>;

    function render(job: Partial<model.DownloadJob>): HTMLElement {
        fixture = TestBed.createComponent(JobCardComponent);
        fixture.componentRef.setInput(
            'job',
            model.DownloadJob.createFrom({ id: 'j1', url: 'https://x', tracks: [], ...job }),
        );
        fixture.detectChanges();
        return fixture.nativeElement as HTMLElement;
    }

    function buttons(element: HTMLElement): string[] {
        return Array.from(element.querySelectorAll('button')).map(
            (button) => button.textContent?.trim() ?? '',
        );
    }

    beforeEach(async () => {
        await TestBed.configureTestingModule({ imports: [JobCardComponent] }).compileComponents();
    });

    it('offers to sign in when the site wants a login', () => {
        const element = render({
            status: 'error',
            errorKind: 'login_required',
            site: 'youtube.com',
            error: 'Sign in to confirm you are not a bot',
        });
        expect(buttons(element)).toContain('Войти…');
        expect(element.textContent).toContain('Подключите аккаунт youtube.com');

        let requested: model.DownloadJob | null = null;
        fixture.componentInstance.loginRequested.subscribe((job: model.DownloadJob): void => {
            requested = job;
        });
        Array.from(element.querySelectorAll('button'))
            .find((button) => button.textContent?.includes('Войти'))!
            .click();
        expect(requested!.id).toBe('j1');
    });

    it('offers to refresh an account that stopped working', () => {
        const element = render({
            status: 'error',
            errorKind: 'login_required',
            site: 'instagram.com',
            accountUsed: true,
        });
        expect(buttons(element)).toContain('Обновить вход');
        expect(element.textContent).toContain('через аккаунт instagram.com');
    });

    it('shows no sign-in button for other errors', () => {
        const element = render({ status: 'error', error: 'HTTP Error 404', site: 'x.com' });
        expect(buttons(element)).not.toContain('Войти…');
        expect(buttons(element)).toContain('Повторить');
    });
});
