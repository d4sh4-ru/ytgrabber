import { Injectable } from '@angular/core';
import { BehaviorSubject, Observable } from 'rxjs';
import { errorMessage } from '../shared/error-message';

export type NotificationKind = 'error' | 'info';

export interface AppNotification {
    readonly id: number;
    readonly kind: NotificationKind;
    readonly message: string;
}

const NOTIFICATION_LIFETIME_MS = 6000;

@Injectable({ providedIn: 'root' })
export class NotificationService {
    public readonly notifications$: Observable<AppNotification[]>;

    private readonly notificationsSubject$ = new BehaviorSubject<AppNotification[]>([]);
    private nextId = 1;

    public constructor() {
        this.notifications$ = this.notificationsSubject$.asObservable();
    }

    public error(error: unknown): void {
        this.push('error', errorMessage(error));
    }

    public info(message: string): void {
        this.push('info', message);
    }

    public dismiss(id: number): void {
        this.notificationsSubject$.next(
            this.notificationsSubject$.value.filter((notification) => notification.id !== id),
        );
    }

    private push(kind: NotificationKind, message: string): void {
        const notification: AppNotification = { id: this.nextId++, kind, message };
        this.notificationsSubject$.next([...this.notificationsSubject$.value, notification]);
        setTimeout((): void => this.dismiss(notification.id), NOTIFICATION_LIFETIME_MS);
    }
}
