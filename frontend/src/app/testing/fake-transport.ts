import { NEVER, Observable } from 'rxjs';
import type { CodeCheck, SessionInfo, SignInRequest } from '../api/session';
import { Transport } from '../api/transport';

/**
 * A Transport for tests: the desktop one unless isDesktop is cleared
 * (desktopApp tells the desktop app connected to a server from a browser),
 * with no backend behind it. Tests override call() for what they need.
 */
export class FakeTransport extends Transport {
    public isDesktop = true;
    public desktopApp: boolean | null = null;
    public readonly reconnected$: Observable<void> = NEVER;
    public sessionInfo: SessionInfo = { authenticated: true };

    public get isDesktopApp(): boolean {
        return this.desktopApp ?? this.isDesktop;
    }

    public call<T>(method: string, _args: readonly unknown[] = []): Promise<T> {
        return Promise.reject(new Error(`unexpected call ${method}`));
    }

    public events$<T>(): Observable<T> {
        return NEVER;
    }

    public session(): Promise<SessionInfo> {
        return Promise.resolve(this.sessionInfo);
    }

    public checkCode(): Promise<CodeCheck> {
        return Promise.resolve({ kind: 'link' });
    }

    public signIn(request: SignInRequest): Promise<SessionInfo> {
        this.sessionInfo = {
            authenticated: true,
            user: { id: 'u', name: request.name ?? 'u', role: 'user', createdAt: '' },
        };
        return Promise.resolve(this.sessionInfo);
    }

    public signOut(): Promise<void> {
        this.sessionInfo = { authenticated: false };
        return Promise.resolve();
    }
}
