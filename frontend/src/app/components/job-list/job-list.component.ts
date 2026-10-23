import {
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    inject,
    Input,
} from '@angular/core';
import { model } from '../../wailsjs/wailsjs/go/models';
import { DownloadService } from '../../services/download.service';
import { NotificationService } from '../../services/notification.service';
import { JobCardComponent } from '../job-card/job-card.component';
import { AccountConnectDialogComponent } from '../account-connect-dialog/account-connect-dialog.component';

@Component({
    selector: 'app-job-list',
    standalone: true,
    imports: [JobCardComponent, AccountConnectDialogComponent],
    templateUrl: './job-list.component.html',
    styleUrl: './job-list.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class JobListComponent {
    private readonly downloadService = inject(DownloadService);
    private readonly notificationService = inject(NotificationService);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);

    @Input()
    public jobs: model.DownloadJob[] = [];

    /** The failed job whose site the connect dialog is open for. */
    protected loginJob: model.DownloadJob | null = null;

    protected async onRemove(id: string): Promise<void> {
        try {
            await this.downloadService.removeJob(id);
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
    }

    protected async onRetry(id: string): Promise<void> {
        const job = this.jobs.find((candidate) => candidate.id === id);
        await this.retry(id, job?.useAccount ?? true);
    }

    protected onLoginRequested(job: model.DownloadJob): void {
        this.loginJob = job;
    }

    protected onLoginCancelled(): void {
        this.loginJob = null;
    }

    protected async onLoginConnected(): Promise<void> {
        const job = this.loginJob;
        this.loginJob = null;
        this.changeDetectorRef.markForCheck();
        if (job) {
            // The user just signed in for this job, so it uses the account
            // even if it was added "without an account".
            await this.retry(job.id, true);
        }
    }

    private async retry(id: string, useAccount: boolean): Promise<void> {
        try {
            await this.downloadService.retryJob(id, useAccount);
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
    }
}
