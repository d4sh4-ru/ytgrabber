import {
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    EventEmitter,
    inject,
    Input,
    OnInit,
    Output,
} from '@angular/core';
import type { CodeCheck } from '../../api/session';
import { SessionService } from '../../services/session.service';
import { guessDeviceName } from '../../shared/device-name';
import { errorMessage } from '../../shared/error-message';

type LoginStep = 'code' | 'details';

/**
 * Sign-in for ytgrabber-server with a one-time code: the setup code from
 * the server's log (first start), an invite from the admin, or a code
 * made on another of the user's devices. A link with ?code= fills it in.
 * The server answers with a session cookie for this device.
 */
@Component({
    selector: 'app-login',
    standalone: true,
    templateUrl: './login.component.html',
    styleUrl: './login.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class LoginComponent implements OnInit {
    // --- Inputs / outputs ---
    /** The server has no users yet: the setup code makes the first admin. */
    @Input()
    public isSetup = false;

    @Output()
    public readonly loggedIn = new EventEmitter<void>();

    // --- DI ---
    private readonly sessionService = inject(SessionService);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);

    // --- Form ---
    protected step: LoginStep = 'code';
    protected code = '';
    protected checked: CodeCheck | null = null;
    protected name = '';
    protected deviceName = guessDeviceName(navigator.userAgent);
    protected error = '';
    protected isSubmitting = false;

    protected get needsName(): boolean {
        return this.checked?.kind !== 'link';
    }

    protected get title(): string {
        switch (this.checked?.kind) {
            case 'setup':
                return 'Настройка сервера';
            case 'invite':
                return 'Приглашение';
            case 'link':
                return `Вход как ${this.checked.userName ?? 'пользователь'}`;
            default:
                return 'YT Grabber';
        }
    }

    // --- Lifecycle ---
    public ngOnInit(): void {
        const code = new URLSearchParams(location.search).get('code');
        if (code) {
            this.code = code;
            // The code is in the address bar and history otherwise.
            history.replaceState(null, '', location.pathname);
            void this.onCheckCode();
        }
    }

    // --- Inputs ---
    protected onCodeInput(event: Event): void {
        this.code = (event.target as HTMLInputElement).value;
        this.error = '';
    }

    protected onNameInput(event: Event): void {
        this.name = (event.target as HTMLInputElement).value;
        this.error = '';
    }

    protected onDeviceNameInput(event: Event): void {
        this.deviceName = (event.target as HTMLInputElement).value;
    }

    // --- Steps ---
    protected async onSubmitCode(event?: Event): Promise<void> {
        event?.preventDefault();
        await this.onCheckCode();
    }

    protected async onSubmitDetails(event: Event): Promise<void> {
        event.preventDefault();
        if (this.isSubmitting || (this.needsName && !this.name.trim())) {
            return;
        }
        await this.run(async (): Promise<void> => {
            await this.sessionService.signIn({
                code: this.code.trim(),
                name: this.needsName ? this.name.trim() : undefined,
                deviceName: this.deviceName.trim(),
            });
            this.loggedIn.emit();
        });
    }

    protected onBack(): void {
        this.step = 'code';
        this.checked = null;
        this.error = '';
    }

    private async onCheckCode(): Promise<void> {
        if (!this.code.trim() || this.isSubmitting) {
            return;
        }
        await this.run(async (): Promise<void> => {
            this.checked = await this.sessionService.checkCode(this.code.trim());
            this.step = 'details';
        });
    }

    private async run(action: () => Promise<void>): Promise<void> {
        this.isSubmitting = true;
        this.error = '';
        this.changeDetectorRef.markForCheck();
        try {
            await action();
        } catch (error: unknown) {
            this.error = errorMessage(error);
        }
        this.isSubmitting = false;
        this.changeDetectorRef.markForCheck();
    }
}
