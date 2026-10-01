import {
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    inject,
    Input,
} from '@angular/core';
import {
    AbstractControl,
    FormControl,
    ReactiveFormsModule,
    ValidationErrors,
    Validators,
} from '@angular/forms';
import { DownloadQuality, DownloadService } from '../../services/download.service';
import { NotificationService } from '../../services/notification.service';
import { model } from '../../wailsjs/wailsjs/go/models';
import { JobListComponent } from '../job-list/job-list.component';

const URL_LIKE_PATTERN = /^(https?:\/\/)?([\w-]+\.)+[a-zA-Z]{2,}([/?#]\S*)?$/;

function urlLikeValidator(control: AbstractControl<string>): ValidationErrors | null {
    const value = control.value.trim();
    return URL_LIKE_PATTERN.test(value) ? null : { urlLike: true };
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
    private readonly downloadService = inject(DownloadService);
    private readonly notificationService = inject(NotificationService);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);

    @Input()
    public jobs: model.DownloadJob[] = [];

    public readonly qualityOptions = QUALITY_OPTIONS;

    public readonly urlControl = new FormControl<string>('', {
        nonNullable: true,
        validators: [Validators.required, urlLikeValidator],
    });

    public readonly qualityControl = new FormControl<DownloadQuality>('best', {
        nonNullable: true,
    });

    protected isSubmitting = false;
    // Validation is shown only after a submit attempt: flagging the empty
    // field as soon as it loses focus reads as an error the user didn't make.
    protected isSubmitAttempted = false;

    protected get isInvalid(): boolean {
        return this.urlControl.invalid && this.isSubmitAttempted;
    }

    protected get placeholder(): string {
        return this.isInvalid
            ? 'Введите непустую ссылку, похожую на URL'
            : 'Ссылка на видео (YouTube и другие сайты)';
    }

    protected async onSubmit(event: Event): Promise<void> {
        event.preventDefault();

        if (this.urlControl.invalid) {
            this.isSubmitAttempted = true;
            return;
        }

        this.isSubmitting = true;
        try {
            await this.downloadService.download(
                this.urlControl.value.trim(),
                this.qualityControl.value,
            );
            // Only clear the field once the backend accepted the link, so a
            // rejected one can be corrected instead of retyped.
            this.urlControl.reset('');
            this.isSubmitAttempted = false;
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.isSubmitting = false;
        this.changeDetectorRef.markForCheck();
    }
}
