import { TestBed } from '@angular/core/testing';
import { Transport } from '../api/transport';
import { FakeTransport } from '../testing/fake-transport';
import { DownloadService } from './download.service';

describe('DownloadService', () => {
    let service: DownloadService;

    beforeEach(() => {
        const transport = new FakeTransport();
        transport.call = <T>(): Promise<T> => Promise.resolve(undefined as T);
        TestBed.configureTestingModule({
            providers: [{ provide: Transport, useValue: transport }],
        });
        service = TestBed.inject(DownloadService);
    });

    // A phone's socket can be dead without knowing: the screen mustn't
    // wait for the backend's event to drop what was removed from it.
    it('reports a job removed from here without waiting for the event', async () => {
        const removed: string[] = [];
        service.jobRemoved$.subscribe((id) => removed.push(id));
        await service.removeJob('job-1');
        expect(removed).toEqual(['job-1']);
    });

    it('reports the library changed after deleting from here', async () => {
        let changes = 0;
        service.libraryChanged$.subscribe(() => changes++);
        await service.deleteVideo('v1', true);
        expect(changes).toBe(1);
    });
});
