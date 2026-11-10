import { inject, Injectable } from '@angular/core';
import { BehaviorSubject, map, Observable } from 'rxjs';
import type { CodeCheck, SessionInfo, SignInRequest } from '../api/session';
import { Transport } from '../api/transport';

/**
 * Who is signed in on a server. The desktop app has no users: its
 * session is always authenticated and nobody is an admin there, which
 * nothing on the desktop asks about.
 */
@Injectable({ providedIn: 'root' })
export class SessionService {
    public readonly session$: Observable<SessionInfo | null>;
    /** Whether the user may manage users, the tools and server settings. */
    public readonly isAdmin$: Observable<boolean>;

    private readonly transport = inject(Transport);
    private readonly sessionSubject$ = new BehaviorSubject<SessionInfo | null>(null);

    public constructor() {
        this.session$ = this.sessionSubject$.asObservable();
        this.isAdmin$ = this.session$.pipe(
            map((session) => this.transport.isDesktop || session?.user?.role === 'admin'),
        );
    }

    public get current(): SessionInfo | null {
        return this.sessionSubject$.value;
    }

    public async load(): Promise<SessionInfo> {
        const session = await this.transport.session();
        this.sessionSubject$.next(session);
        return session;
    }

    public checkCode(code: string): Promise<CodeCheck> {
        return this.transport.checkCode(code);
    }

    public async signIn(request: SignInRequest): Promise<SessionInfo> {
        const session = await this.transport.signIn(request);
        this.sessionSubject$.next(session);
        return session;
    }

    public async signOut(): Promise<void> {
        await this.transport.signOut();
        this.sessionSubject$.next({ authenticated: false });
    }
}
