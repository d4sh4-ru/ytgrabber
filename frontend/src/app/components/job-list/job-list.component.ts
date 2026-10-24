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
import { AccountsService } from '../../services/accounts.service';
import { connectedMessage } from '../../shared/account-summary';
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
    private readonly accountsService = inject(AccountsService);
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

    /**
     * An account taken from a browser is refreshed from the same profile
     * and the job retried in one click; otherwise (a file, no account, or
     * the refresh failed) the connect dialog opens.
     */
    protected async onLoginRequested(job: model.DownloadJob): Promise<void> {
        const account = job.accountUsed ? this.accountsService.findBySite(job.site) : null;
        if (account?.source === 'browser') {
            try {
                const refreshed = await this.accountsService.refreshFromBrowser(account.id);
                this.notificationService.info(connectedMessage(refreshed));
                await this.retry(job.id, true);
                return;
            } catch (error: unknown) {
                this.notificationService.error(error);
            }
        }
        this.loginJob = job;
        this.changeDetectorRef.markForCheck();
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
