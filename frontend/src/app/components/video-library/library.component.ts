import {
    ChangeDetectionStrategy,
    Component,
    EventEmitter,
    inject,
    Input,
    Output,
} from '@angular/core';
import { AsyncPipe } from '@angular/common';
import { filter, map, Observable } from 'rxjs';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { model } from '../../api/models';
import type { VideoShare } from '../../api/session';
import { Transport } from '../../api/transport';
import { DownloadService } from '../../services/download.service';
import { NotificationService } from '../../services/notification.service';
import { AudioPlayerService } from '../../services/audio-player.service';
import { SessionService } from '../../services/session.service';
import { ShareService } from '../../services/share.service';
import { MediaCardComponent } from '../media-card/media-card.component';
import { bypassServiceWorker } from '../../shared/media-url';
import { IconComponent } from '../icon/icon.component';
import { ShareDialogComponent } from '../share-dialog/share-dialog.component';
import {
    ConfirmDialogComponent,
    ConfirmDialogResult,
} from '../confirm-dialog/confirm-dialog.component';

type LibraryGroup = 'video' | 'audio';

@Component({
    selector: 'app-library',
    standalone: true,
    imports: [
        AsyncPipe,
        MediaCardComponent,
        ConfirmDialogComponent,
        IconComponent,
        ShareDialogComponent,
    ],
    templateUrl: './library.component.html',
    styleUrl: './library.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class LibraryComponent {
    // --- DI ---
    private readonly downloadService = inject(DownloadService);
    private readonly notificationService = inject(NotificationService);
    private readonly audioPlayerService = inject(AudioPlayerService);
    private readonly sessionService = inject(SessionService);
    private readonly shareService = inject(ShareService);
    /** The desktop may keep the file on disk; a server always deletes it. */
    protected readonly isDesktop = inject(Transport).isDesktop;

    // --- Items ---
    @Input()
    public items: model.VideoRecord[] = [];

    @Output()
    public readonly videoSelected = new EventEmitter<model.VideoRecord>();

    /** The empty library's button: to the Downloads tab. */
    @Output()
    public readonly addRequested = new EventEmitter<void>();

    /** The track in the audio bar: its card is outlined. */
    protected readonly currentTrackId$: Observable<string | null> =
        this.audioPlayerService.currentTrack$.pipe(map((track) => track?.id ?? null));

    // --- Grouping ---
    protected activeGroup: LibraryGroup = 'video';

    // --- Deletion ---
    protected pendingDelete: model.VideoRecord | null = null;

    // --- Sharing ---
    /** Links lead to a server: an admin there may make them, the local desktop has none. */
    protected readonly canShare$: Observable<boolean> = this.sessionService.isAdmin$.pipe(
        map((isAdmin: boolean): boolean => isAdmin && !this.isDesktop),
    );
    /** The admin's links: a card with one shows it. */
    protected readonly shares$: Observable<VideoShare[]> = this.shareService.shares$;
    protected sharing: model.VideoRecord | null = null;

    protected get videoItems(): model.VideoRecord[] {
        return this.items.filter((item) => item.mediaKind !== 'audio');
    }

    protected get audioItems(): model.VideoRecord[] {
        return this.items.filter((item) => item.mediaKind === 'audio');
    }

    protected get visibleItems(): model.VideoRecord[] {
        return this.activeGroup === 'video' ? this.videoItems : this.audioItems;
    }

    public constructor() {
        this.canShare$
            .pipe(
                filter((canShare: boolean): boolean => canShare),
                takeUntilDestroyed(),
            )
            .subscribe((): void => {
                this.shareService.refresh().catch((error: unknown): void => {
                    this.notificationService.error(error);
                });
            });
    }

    // --- Grouping ---
    protected setGroup(group: LibraryGroup): void {
        this.activeGroup = group;
    }

    // --- Card actions ---
    protected onOpened(item: model.VideoRecord): void {
        if (item.fileMissing) {
            this.notificationService.error(`Файл не найден: ${item.filePath}`);
            return;
        }

        if (item.mediaKind === 'audio') {
            this.audioPlayerService.play({
                id: item.id,
                title: item.title,
                src: bypassServiceWorker(item.mediaUrl),
                coverSrc: item.thumbnailUrl ? bypassServiceWorker(item.thumbnailUrl) : null,
                positionSeconds: item.positionSeconds,
            });
        } else {
            this.videoSelected.emit(item);
        }
    }

    protected async onRevealed(item: model.VideoRecord): Promise<void> {
        try {
            await this.downloadService.revealVideo(item.id);
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
    }

    // --- Sharing ---
    protected onShareRequested(item: model.VideoRecord): void {
        this.sharing = item;
    }

    protected onShareClosed(): void {
        this.sharing = null;
    }

    protected shareOf(
        shares: readonly VideoShare[] | null,
        item: model.VideoRecord,
    ): VideoShare | null {
        return this.shareService.shareOf(shares ?? [], item.id);
    }

    // --- Deletion ---
    protected onDeleteRequested(item: model.VideoRecord): void {
        this.pendingDelete = item;
    }

    protected onDeleteCancelled(): void {
        this.pendingDelete = null;
    }

    protected async onDeleteConfirmed(result: ConfirmDialogResult): Promise<void> {
        const item = this.pendingDelete;
        this.pendingDelete = null;
        if (!item) {
            return;
        }

        this.audioPlayerService.stopIfPlaying(item.id);
        try {
            // A server keeps no files the library doesn't list: they'd only
            // fill its disk, out of everyone's reach.
            await this.downloadService.deleteVideo(item.id, result.option || !this.isDesktop);
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
    }
}
