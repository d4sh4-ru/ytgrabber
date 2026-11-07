import {
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    EventEmitter,
    inject,
    Output,
} from '@angular/core';
import { Transport } from '../../api/transport';
import { errorMessage } from '../../shared/error-message';

/**
 * Sign-in for ytgrabber-server: the access token from the server's log or
 * its data folder. The server answers with a session cookie, so this is
 * asked once per browser.
 */
@Component({
    selector: 'app-login',
    standalone: true,
    templateUrl: './login.component.html',
    styleUrl: './login.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class LoginComponent {
    @Output()
    public readonly loggedIn = new EventEmitter<void>();

    protected token = '';
    protected error = '';
    protected isSubmitting = false;

    private readonly transport = inject(Transport);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);

    protected onTokenInput(event: Event): void {
        this.token = (event.target as HTMLInputElement).value;
        this.error = '';
    }

    protected async onSubmit(event: Event): Promise<void> {
        event.preventDefault();
        const token = this.token.trim();
        if (!token || this.isSubmitting) {
            return;
        }

        this.isSubmitting = true;
        this.error = '';
        this.changeDetectorRef.markForCheck();
        try {
            await this.transport.login(token);
            this.loggedIn.emit();
        } catch (error: unknown) {
            this.error = errorMessage(error);
        }
        this.isSubmitting = false;
        this.changeDetectorRef.markForCheck();
    }
}
