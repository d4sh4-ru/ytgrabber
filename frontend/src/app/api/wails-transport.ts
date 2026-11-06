import { Observable } from 'rxjs';
import { EventsOn } from '../wailsjs/wailsjs/runtime';
import type { Transport } from './transport';

type Binding = (...args: unknown[]) => Promise<unknown>;

interface WailsWindow {
    go?: { app?: { App?: Record<string, Binding | undefined> } };
}

/**
 * The desktop transport: methods are the Wails bindings of
 * internal/app.App (window.go.app.App), events come from the Wails runtime.
 */
export class WailsTransport implements Transport {
    public call<T>(method: string, args: readonly unknown[] = []): Promise<T> {
        const binding = (window as WailsWindow).go?.app?.App?.[method];
        if (!binding) {
            return Promise.reject(new Error(`Метод бэкенда ${method} недоступен`));
        }
        return binding(...args) as Promise<T>;
    }

    public events$<T>(name: string): Observable<T> {
        return new Observable<T>((subscriber) => {
            return EventsOn(name, (payload: T): void => {
                subscriber.next(payload);
            });
        });
    }
}
