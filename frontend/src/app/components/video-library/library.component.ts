import {
    ChangeDetectionStrategy,
    Component,
    EventEmitter,
    inject,
    Input,
    Output,
} from '@angular/core';
import { model } from '../../api/models';
import { Transport } from '../../api/transport';
import { DownloadService } from '../../services/download.service';
import { NotificationService } from '../../services/notification.service';
import { AudioPlayerService } from '../../services/audio-player.service';
import { MediaCardComponent } from '../media-card/media-card.component';
import { bypassServiceWorker } from '../../shared/media-url';
import {
    ConfirmDialogComponent,
    ConfirmDialogResult,
} from '../confirm-dialog/confirm-dialog.component';

type LibraryGroup = 'video' | 'audio';

@Component({
    selector: 'app-library',
    standalone: true,
    imports: [MediaCardComponent, ConfirmDialogComponent],
    templateUrl: './library.component.html',
    styleUrl: './library.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class LibraryComponent {
    // --- DI ---
    private readonly downloadService = inject(DownloadService);
    private readonly notificationService = inject(NotificationService);
    private readonly audioPlayerService = inject(AudioPlayerService);
    /** The desktop may keep the file on disk; a server always deletes it. */
    protected readonly isDesktop = inject(Transport).isDesktop;

    // --- Items ---
    @Input()
    public items: model.VideoRecord[] = [];

    @Output()
    public readonly videoSelected = new EventEmitter<model.VideoRecord>();

    // --- Grouping ---
    protected activeGroup: LibraryGroup = 'video';

    // --- Deletion ---
    protected pendingDelete: model.VideoRecord | null = null;

    protected get videoItems(): model.VideoRecord[] {
        return this.items.filter((item) => item.mediaKind !== 'audio');
    }

    protected get audioItems(): model.VideoRecord[] {
        return this.items.filter((item) => item.mediaKind === 'audio');
    }

    protected get visibleItems(): model.VideoRecord[] {
        return this.activeGroup === 'video' ? this.videoItems : this.audioItems;
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
