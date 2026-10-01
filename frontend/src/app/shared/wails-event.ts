import { Observable } from 'rxjs';
import { EventsOn } from '../wailsjs/wailsjs/runtime';

/** Wraps a Wails backend event in an Observable; unsubscribing removes the listener. */
export function fromWailsEvent<T>(name: string): Observable<T> {
    return new Observable<T>((subscriber) => {
        return EventsOn(name, (payload: T): void => {
            subscriber.next(payload);
        });
    });
}
