import { ChangeDetectionStrategy, Component, EventEmitter, Input, Output } from '@angular/core';
import { model } from '../../wailsjs/wailsjs/go/models';

const STATUS_LABELS: Record<string, string> = {
    pending: 'в очереди',
    downloading: 'загружается',
    processing: 'обработка',
    done: 'готово',
    error: 'ошибка',
};

const TRACK_LABELS: Record<string, string> = {
    video: 'Видео',
    audio: 'Аудио',
};

@Component({
    selector: 'app-job-card',
    standalone: true,
    imports: [],
    templateUrl: './job-card.component.html',
    styleUrl: './job-card.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class JobCardComponent {
    @Input({ required: true })
    public job!: model.DownloadJob;

    @Output()
    public readonly removed = new EventEmitter<string>();

    @Output()
    public readonly retried = new EventEmitter<string>();

    /** The site wants a login: connect or refresh its account, then retry. */
    @Output()
    public readonly loginRequested = new EventEmitter<model.DownloadJob>();

    protected get displayTitle(): string {
        return this.job.title || this.job.url;
    }

    protected get statusLabel(): string {
        return STATUS_LABELS[this.job.status] ?? this.job.status;
    }

    protected get isProcessing(): boolean {
        return this.job.status === 'processing';
    }

    protected get isError(): boolean {
        return this.job.status === 'error';
    }

    protected get isLoginRequired(): boolean {
        return this.isError && this.job.errorKind === 'login_required';
    }

    protected get loginHint(): string {
        if (this.job.accountUsed) {
            return `Сайт не принял вход через аккаунт ${this.job.site}: похоже, он истёк. Загрузите свежий cookies.txt.`;
        }
        return this.job.site
            ? `Это видео доступно только после входа. Подключите аккаунт ${this.job.site}.`
            : 'Это видео доступно только после входа.';
    }

    protected get loginButtonLabel(): string {
        return this.job.accountUsed ? 'Обновить вход' : 'Войти…';
    }

    protected get removeButtonLabel(): string {
        return this.isError ? 'Убрать' : 'Отменить';
    }

    protected trackLabel(kind: string): string {
        return TRACK_LABELS[kind] ?? kind;
    }

    protected trackPercent(progress: number): number {
        return Math.min(100, Math.max(0, Math.round(progress)));
    }

    protected onRemove(): void {
        this.removed.emit(this.job.id);
    }

    protected onRetry(): void {
        this.retried.emit(this.job.id);
    }

    protected onLogin(): void {
        this.loginRequested.emit(this.job);
    }
}
