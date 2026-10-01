import {
    ChangeDetectionStrategy,
    Component,
    EventEmitter,
    HostListener,
    Input,
    Output,
} from '@angular/core';

export interface ConfirmDialogResult {
    readonly option: boolean;
}

/**
 * A small modal confirmation with an optional checkbox. Native confirm()
 * isn't an option: WKWebView/WebView2 in Wails don't implement JS dialogs
 * consistently.
 */
@Component({
    selector: 'app-confirm-dialog',
    standalone: true,
    imports: [],
    templateUrl: './confirm-dialog.component.html',
    styleUrl: './confirm-dialog.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ConfirmDialogComponent {
    @Input({ required: true })
    public title = '';

    @Input()
    public message = '';

    @Input()
    public confirmLabel = 'Подтвердить';

    @Input()
    public optionLabel = '';

    @Input()
    public option = false;

    /** Styles the confirm button as destructive. */
    @Input()
    public danger = false;

    /** Disables the buttons while the confirmed action runs. */
    @Input()
    public busy = false;

    @Output()
    public readonly confirmed = new EventEmitter<ConfirmDialogResult>();

    @Output()
    public readonly cancelled = new EventEmitter<void>();

    @HostListener('document:keydown.escape')
    public onEscape(): void {
        this.onCancel();
    }

    protected onCancel(): void {
        if (!this.busy) {
            this.cancelled.emit();
        }
    }

    protected onOptionChange(event: Event): void {
        this.option = (event.target as HTMLInputElement).checked;
    }

    protected onConfirm(): void {
        this.confirmed.emit({ option: this.option });
    }
}
