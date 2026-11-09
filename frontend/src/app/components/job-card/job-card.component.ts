import { ChangeDetectionStrategy, Component, EventEmitter, Input, Output } from '@angular/core';
import { model } from '../../api/models';
import { IconComponent, IconName } from '../icon/icon.component';

const STATUS_LABELS: Record<string, string> = {
    pending: 'в очереди',
    downloading: 'загружается',
    processing: 'обработка',
    done: 'готово',
    error: 'ошибка',
};

const VIDEO_QUALITY_LABELS: Record<string, string> = {
    best: 'Лучшее качество',
    '1080p': '1080p',
    '720p': '720p',
};

const TRACK_LABELS: Record<string, string> = {
    video: 'Видео',
    audio: 'Аудио',
};

@Component({
    selector: 'app-job-card',
    standalone: true,
    imports: [IconComponent],
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

    /** The full error output (errorDetails) is unfolded under the summary. */
    protected isErrorExpanded = false;

    protected get displayTitle(): string {
        return this.job.title || this.job.url;
    }

    protected get statusLabel(): string {
        // The server is still installing a tool the download needs.
        if (this.job.status === 'pending' && this.job.waitingFor) {
            return `ожидает установки ${this.job.waitingFor}`;
        }
        return STATUS_LABELS[this.job.status] ?? this.job.status;
    }

    protected get isAudio(): boolean {
        return this.job.quality === 'audio';
    }

    /** What the job downloads, from the job itself: "720p", "Аудио · MP3"; empty for old jobs. */
    protected get qualityLabel(): string {
        const format = this.job.format ? this.job.format.toUpperCase() : '';
        if (this.isAudio) {
            return format ? `Аудио · ${format}` : 'Аудио';
        }
        const label = VIDEO_QUALITY_LABELS[this.job.quality] ?? '';
        return label && format ? `${label} · ${format}` : label;
    }

    protected get qualityIcon(): IconName {
        return this.isAudio ? 'music' : 'video';
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

    /** Details worth unfolding: present and not just the summary again. */
    protected get hasErrorDetails(): boolean {
        const details = this.job.errorDetails?.trim() ?? '';
        return details !== '' && details !== (this.job.error ?? '').trim();
    }

    protected get errorDetailsId(): string {
        return `job-error-details-${this.job.id}`;
    }

    protected get errorToggleLabel(): string {
        return this.isErrorExpanded ? 'Скрыть подробности ошибки' : 'Показать подробности ошибки';
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

    protected toggleErrorDetails(): void {
        this.isErrorExpanded = !this.isErrorExpanded;
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
