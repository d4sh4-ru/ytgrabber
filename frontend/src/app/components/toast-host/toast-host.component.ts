import { AsyncPipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { NotificationService } from '../../services/notification.service';
import { IconComponent } from '../icon/icon.component';

@Component({
    selector: 'app-toast-host',
    standalone: true,
    imports: [AsyncPipe, IconComponent],
    templateUrl: './toast-host.component.html',
    styleUrl: './toast-host.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ToastHostComponent {
    protected readonly notificationService = inject(NotificationService);
}
