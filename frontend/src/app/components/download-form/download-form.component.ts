import { ChangeDetectionStrategy, Component, EventEmitter, Input, Output } from '@angular/core';
import {
    AbstractControl,
    FormControl,
    ReactiveFormsModule,
    ValidationErrors,
    Validators,
} from '@angular/forms';
import { DownloadQuality } from '../../services/download.service';
import { main } from '../../wailsjs/wailsjs/go/models';
import { JobListComponent } from '../job-list/job-list.component';

const URL_LIKE_PATTERN = /^(https?:\/\/)?([\w-]+\.)+[a-zA-Z]{2,}([/?#]\S*)?$/;

function urlLikeValidator(control: AbstractControl<string>): ValidationErrors | null {
    const value = control.value.trim();
    return URL_LIKE_PATTERN.test(value) ? null : { urlLike: true };
}

export interface DownloadRequest {
    readonly url: string;
    readonly quality: DownloadQuality;
}

interface QualityOption {
    readonly value: DownloadQuality;
    readonly label: string;
}

const QUALITY_OPTIONS: readonly QualityOption[] = [
    { value: 'best', label: 'Лучшее' },
    { value: '1080p', label: '1080p' },
    { value: '720p', label: '720p' },
    { value: 'audio', label: 'Только аудио (mp3)' },
];

@Component({
    selector: 'app-download-form',
    standalone: true,
    imports: [ReactiveFormsModule, JobListComponent],
    templateUrl: './download-form.component.html',
    styleUrl: './download-form.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DownloadFormComponent {
    @Input()
    public jobs: main.DownloadJob[] = [];

    @Output()
    public readonly downloadRequested = new EventEmitter<DownloadRequest>();

    public readonly qualityOptions = QUALITY_OPTIONS;

    public readonly urlControl = new FormControl<string>('', {
        nonNullable: true,
        validators: [Validators.required, urlLikeValidator],
    });

    public readonly qualityControl = new FormControl<DownloadQuality>('best', {
        nonNullable: true,
    });

    protected get isInvalid(): boolean {
        return this.urlControl.invalid && this.urlControl.touched;
    }

    protected get placeholder(): string {
        return this.isInvalid
            ? 'Введите непустую ссылку, похожую на URL'
            : 'Ссылка на видео YouTube';
    }

    protected onSubmit(event: Event): void {
        event.preventDefault();

        if (this.urlControl.invalid) {
            this.urlControl.markAsTouched();
            return;
        }

        this.downloadRequested.emit({
            url: this.urlControl.value.trim(),
            quality: this.qualityControl.value,
        });
        this.urlControl.reset('');
    }
}
