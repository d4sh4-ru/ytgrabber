import { ChangeDetectionStrategy, Component, inject, Input } from '@angular/core';
import { model } from '../../wailsjs/wailsjs/go/models';
import { DownloadService } from '../../services/download.service';
import { NotificationService } from '../../services/notification.service';
import { JobCardComponent } from '../job-card/job-card.component';

@Component({
    selector: 'app-job-list',
    standalone: true,
    imports: [JobCardComponent],
    templateUrl: './job-list.component.html',
    styleUrl: './job-list.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class JobListComponent {
    private readonly downloadService = inject(DownloadService);
    private readonly notificationService = inject(NotificationService);

    @Input()
    public jobs: model.DownloadJob[] = [];

    protected async onRemove(id: string): Promise<void> {
        try {
            await this.downloadService.removeJob(id);
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
    }

    protected async onRetry(id: string): Promise<void> {
        try {
            await this.downloadService.retryJob(id);
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
    }
}
