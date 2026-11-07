import { NEVER, Observable } from 'rxjs';
import { Transport } from '../api/transport';

/**
 * A Transport for tests: the desktop one unless isDesktop is cleared,
 * with no backend behind it. Tests override call() for what they need.
 */
export class FakeTransport extends Transport {
    public isDesktop = true;
    public readonly reconnected$: Observable<void> = NEVER;
    public isAuthenticated = true;

    public call<T>(method: string, _args: readonly unknown[] = []): Promise<T> {
        return Promise.reject(new Error(`unexpected call ${method}`));
    }

    public events$<T>(): Observable<T> {
        return NEVER;
    }

    public checkSession(): Promise<boolean> {
        return Promise.resolve(this.isAuthenticated);
    }

    public login(): Promise<void> {
        this.isAuthenticated = true;
        return Promise.resolve();
    }
}
