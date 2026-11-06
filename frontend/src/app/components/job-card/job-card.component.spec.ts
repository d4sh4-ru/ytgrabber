import { ComponentFixture, TestBed } from '@angular/core/testing';
import { JobCardComponent } from './job-card.component';
import { model } from '../../api/models';

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

    describe('quality', () => {
        function quality(element: HTMLElement): string {
            return element.querySelector('.job-card__quality')?.textContent?.trim() ?? '';
        }

        for (const status of ['pending', 'downloading', 'processing', 'done', 'error']) {
            it(`shows the chosen video quality while ${status}`, () => {
                const element = render({ status, quality: '1080p', format: 'mp4' });
                expect(quality(element)).toBe('1080p · MP4');
                expect(element.querySelector('.job-card__quality app-icon')).not.toBeNull();
            });

            it(`marks an audio job while ${status}`, () => {
                expect(quality(render({ status, quality: 'audio', format: 'mp3' }))).toBe(
                    'Аудио · MP3',
                );
            });
        }

        it('names the best quality', () => {
            expect(quality(render({ status: 'pending', quality: 'best', format: 'mp4' }))).toBe(
                'Лучшее качество · MP4',
            );
        });

        it('marks audio even when the format is unknown', () => {
            expect(quality(render({ status: 'pending', quality: 'audio' }))).toBe('Аудио');
        });

        it('shows nothing for a job without a quality', () => {
            expect(render({ status: 'error' }).querySelector('.job-card__quality')).toBeNull();
        });
    });
});
