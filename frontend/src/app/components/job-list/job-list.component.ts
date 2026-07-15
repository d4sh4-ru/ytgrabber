import {
    ChangeDetectionStrategy,
    Component,
    inject,
    Input,
    OnChanges,
    OnDestroy,
    OnInit,
    SimpleChanges,
} from '@angular/core';
import { Subject, takeUntil } from 'rxjs';
import { main } from '../../wailsjs/wailsjs/go/models';
import { DownloadService } from '../../services/download.service';
import { JobCardComponent } from '../job-card/job-card.component';

@Component({
    selector: 'app-job-list',
    standalone: true,
    imports: [JobCardComponent],
    templateUrl: './job-list.component.html',
    styleUrl: './job-list.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class JobListComponent implements OnChanges, OnInit, OnDestroy {
    @Input()
    public jobs: main.DownloadJob[] = [];

    private readonly downloadService = inject(DownloadService);
    private readonly destroy$ = new Subject<void>();

    protected displayJobs: main.DownloadJob[] = [];

    public ngOnChanges(changes: SimpleChanges): void {
        if (changes['jobs']) {
            this.displayJobs = this.jobs;
        }
    }

    public ngOnInit(): void {
        this.downloadService.jobRemoved$
            .pipe(takeUntil(this.destroy$))
            .subscribe((id: string): void => {
                this.displayJobs = this.displayJobs.filter((job) => job.id !== id);
            });
    }

    public ngOnDestroy(): void {
        this.destroy$.next();
        this.destroy$.complete();
    }

    protected onRemove(id: string): void {
        void this.downloadService.removeJob(id);
    }
}
