import { inject, Injectable } from '@angular/core';
import type { Device, IssuedCode, NewDevice, UserInfo, UserRole } from '../api/session';
import { Transport } from '../api/transport';

/** The user's devices and, for an admin, the server's users. Server only. */
@Injectable({ providedIn: 'root' })
export class PeopleService {
    private readonly transport = inject(Transport);

    // --- Devices ---
    public getDevices(): Promise<Device[]> {
        return this.transport.call<Device[]>('GetDevices');
    }

    public removeDevice(id: string): Promise<void> {
        return this.transport.call<void>('RemoveDevice', [id]);
    }

    /** A code that signs another device in as this user. */
    public createDeviceLink(): Promise<IssuedCode> {
        return this.transport.call<IssuedCode>('CreateDeviceLink');
    }

    public createApiToken(name: string): Promise<NewDevice> {
        return this.transport.call<NewDevice>('CreateApiToken', [name]);
    }

    // --- Users (admin) ---
    public getUsers(): Promise<UserInfo[]> {
        return this.transport.call<UserInfo[]>('GetUsers');
    }

    public inviteUser(role: UserRole): Promise<IssuedCode> {
        return this.transport.call<IssuedCode>('InviteUser', [role]);
    }

    /** A sign-in code for a user who lost their devices. */
    public createUserLink(id: string): Promise<IssuedCode> {
        return this.transport.call<IssuedCode>('CreateUserLink', [id]);
    }

    public removeUser(id: string): Promise<void> {
        return this.transport.call<void>('RemoveUser', [id]);
    }

    // --- Links ---
    /** The address that opens the login screen with code filled in. */
    public signInUrl(code: string): string {
        return `${location.origin}/?code=${encodeURIComponent(code)}`;
    }

    /** An SVG QR code of text, drawn by the server. */
    public qrUrl(text: string): string {
        return `/api/v1/qr?text=${encodeURIComponent(text)}`;
    }
}
