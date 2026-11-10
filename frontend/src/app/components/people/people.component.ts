import {
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    inject,
    OnDestroy,
    OnInit,
} from '@angular/core';
import { Subject, takeUntil } from 'rxjs';
import type { Device, IssuedCode, UserInfo, UserRole } from '../../api/session';
import { NotificationService } from '../../services/notification.service';
import { PeopleService } from '../../services/people.service';
import { SessionService } from '../../services/session.service';
import { formatRelativeTime, pluralRu } from '../../shared/format-relative-time';
import { IconComponent } from '../icon/icon.component';
import { ConfirmDialogComponent } from '../confirm-dialog/confirm-dialog.component';

/** A code on screen to type in, open as a link or scan as a QR code. */
interface ShownCode {
    readonly title: string;
    readonly hint: string;
    readonly code: string;
    readonly url: string;
    readonly qrUrl: string;
    readonly expiresAt: string;
}

/**
 * Server only: the user's devices (signing in on another one, API tokens,
 * revoking) and, for an admin, the server's users (invites, sign-in
 * codes for someone who lost their devices, removal).
 */
@Component({
    selector: 'app-people',
    standalone: true,
    imports: [ConfirmDialogComponent, IconComponent],
    templateUrl: './people.component.html',
    styleUrl: './people.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PeopleComponent implements OnInit, OnDestroy {
    // --- DI / lifecycle ---
    private readonly peopleService = inject(PeopleService);
    private readonly sessionService = inject(SessionService);
    private readonly notificationService = inject(NotificationService);
    private readonly changeDetectorRef = inject(ChangeDetectorRef);
    private readonly destroy$ = new Subject<void>();

    // --- Session ---
    protected userName = '';
    protected isAdmin = false;
    protected isSigningOut = false;

    // --- Devices ---
    protected devices: Device[] = [];
    protected deviceToRevoke: Device | null = null;
    protected isRevoking = false;
    protected tokenName = '';
    protected apiToken = '';

    // --- Users ---
    protected users: UserInfo[] = [];
    protected inviteRole: UserRole = 'user';
    protected userToRemove: UserInfo | null = null;
    protected isRemovingUser = false;

    // --- Codes ---
    protected shownCode: ShownCode | null = null;
    protected readonly canCopy = typeof navigator.clipboard?.writeText === 'function';

    // --- Lifecycle ---
    public ngOnInit(): void {
        this.sessionService.session$.pipe(takeUntil(this.destroy$)).subscribe((session): void => {
            this.userName = session?.user?.name ?? '';
            this.changeDetectorRef.markForCheck();
        });
        this.sessionService.isAdmin$
            .pipe(takeUntil(this.destroy$))
            .subscribe((isAdmin: boolean): void => {
                this.isAdmin = isAdmin;
                this.changeDetectorRef.markForCheck();
                void this.refresh();
            });
    }

    public ngOnDestroy(): void {
        this.destroy$.next();
        this.destroy$.complete();
    }

    // --- Labels ---
    protected lastSeenLabel(iso: string): string {
        return iso ? formatRelativeTime(iso) : 'не заходил';
    }

    protected expiresLabel(iso: string): string {
        const date = new Date(iso);
        return date.toLocaleString('ru-RU', {
            day: 'numeric',
            month: 'long',
            hour: '2-digit',
            minute: '2-digit',
        });
    }

    protected roleLabel(role: UserRole): string {
        return role === 'admin' ? 'администратор' : 'пользователь';
    }

    protected devicesLabel(count: number): string {
        return `${count} ${pluralRu(count, 'устройство', 'устройства', 'устройств')}`;
    }

    // --- Session ---
    protected async onSignOut(): Promise<void> {
        this.isSigningOut = true;
        this.changeDetectorRef.markForCheck();
        try {
            await this.sessionService.signOut();
            location.reload();
        } catch (error: unknown) {
            this.notificationService.error(error);
            this.isSigningOut = false;
            this.changeDetectorRef.markForCheck();
        }
    }

    // --- Devices ---
    protected async onAddDevice(): Promise<void> {
        await this.showCode(
            'Вход на другом устройстве',
            'Откройте ссылку или отсканируйте QR-код на новом устройстве, или введите код на экране входа.',
            (): Promise<IssuedCode> => this.peopleService.createDeviceLink(),
        );
    }

    protected onTokenNameInput(event: Event): void {
        this.tokenName = (event.target as HTMLInputElement).value;
    }

    protected async onCreateToken(event: Event): Promise<void> {
        event.preventDefault();
        const name = this.tokenName.trim();
        if (!name) {
            return;
        }
        try {
            const created = await this.peopleService.createApiToken(name);
            this.apiToken = created.token;
            this.tokenName = '';
            await this.refresh();
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.changeDetectorRef.markForCheck();
    }

    protected onTokenShown(): void {
        this.apiToken = '';
    }

    protected onRevokeRequested(device: Device): void {
        this.deviceToRevoke = device;
    }

    protected onRevokeCancelled(): void {
        this.deviceToRevoke = null;
    }

    protected async onRevokeConfirmed(): Promise<void> {
        const device = this.deviceToRevoke;
        if (!device) {
            return;
        }
        if (device.current) {
            this.deviceToRevoke = null;
            await this.onSignOut();
            return;
        }
        this.isRevoking = true;
        this.changeDetectorRef.markForCheck();
        try {
            await this.peopleService.removeDevice(device.id);
            this.deviceToRevoke = null;
            await this.refresh();
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.isRevoking = false;
        this.changeDetectorRef.markForCheck();
    }

    // --- Users ---
    protected onInviteRoleChange(event: Event): void {
        this.inviteRole = (event.target as HTMLSelectElement).value as UserRole;
    }

    protected async onInvite(): Promise<void> {
        const role = this.inviteRole;
        await this.showCode(
            role === 'admin' ? 'Приглашение администратора' : 'Приглашение',
            'Отправьте ссылку тому, кого приглашаете: по ней он выберет имя и войдёт. Приглашение одноразовое.',
            (): Promise<IssuedCode> => this.peopleService.inviteUser(role),
        );
    }

    protected async onLinkUser(user: UserInfo): Promise<void> {
        await this.showCode(
            `Вход для ${user.name}`,
            'Код входит как этот пользователь на новом устройстве: для того, кто потерял все свои устройства.',
            (): Promise<IssuedCode> => this.peopleService.createUserLink(user.id),
        );
    }

    protected onRemoveUserRequested(user: UserInfo): void {
        this.userToRemove = user;
    }

    protected onRemoveUserCancelled(): void {
        this.userToRemove = null;
    }

    protected async onRemoveUserConfirmed(): Promise<void> {
        const user = this.userToRemove;
        if (!user) {
            return;
        }
        this.isRemovingUser = true;
        this.changeDetectorRef.markForCheck();
        try {
            await this.peopleService.removeUser(user.id);
            this.userToRemove = null;
            await this.refresh();
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.isRemovingUser = false;
        this.changeDetectorRef.markForCheck();
    }

    // --- Codes ---
    protected onCodeClosed(): void {
        this.shownCode = null;
    }

    protected async onCopy(text: string): Promise<void> {
        try {
            await navigator.clipboard.writeText(text);
            this.notificationService.info('Скопировано');
        } catch {
            this.notificationService.error(
                new Error('Не удалось скопировать: выделите текст в поле и скопируйте вручную'),
            );
        }
    }

    protected onSelectAll(event: Event): void {
        (event.target as HTMLInputElement).select();
    }

    private async showCode(
        title: string,
        hint: string,
        issue: () => Promise<IssuedCode>,
    ): Promise<void> {
        try {
            const issued = await issue();
            const url = this.peopleService.signInUrl(issued.code);
            this.shownCode = {
                title,
                hint,
                code: issued.code,
                url,
                qrUrl: this.peopleService.qrUrl(url),
                expiresAt: issued.expiresAt,
            };
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.changeDetectorRef.markForCheck();
    }

    private async refresh(): Promise<void> {
        try {
            const [devices, users] = await Promise.all([
                this.peopleService.getDevices(),
                this.isAdmin ? this.peopleService.getUsers() : Promise.resolve([]),
            ]);
            this.devices = devices;
            this.users = users;
        } catch (error: unknown) {
            this.notificationService.error(error);
        }
        this.changeDetectorRef.markForCheck();
    }
}
