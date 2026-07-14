import { TestBed } from '@angular/core/testing';
import { Observable } from 'rxjs';
import { App } from './app';
import { DownloadQuality, DownloadService } from './services/download.service';
import { main } from './wailsjs/wailsjs/go/models';

class DownloadServiceStub {
  public readonly progress$: Observable<main.DownloadJob> = new Observable();
  public readonly status$: Observable<main.DownloadJob> = new Observable();
  public readonly jobRemoved$: Observable<string> = new Observable();

  public download(url: string, quality: DownloadQuality): Promise<string> {
    return Promise.resolve('job-1');
  }

  public getJobs(): Promise<main.DownloadJob[]> {
    return Promise.resolve([]);
  }

  public getLibrary(): Promise<main.VideoRecord[]> {
    return Promise.resolve([]);
  }

  public removeJob(id: string): Promise<void> {
    return Promise.resolve();
  }
}

describe('App', () => {
  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [App],
      providers: [{ provide: DownloadService, useClass: DownloadServiceStub }],
    }).compileComponents();
  });

  it('should create the app', () => {
    const fixture = TestBed.createComponent(App);
    const app = fixture.componentInstance;
    expect(app).toBeTruthy();
  });

  it('should render title', async () => {
    const fixture = TestBed.createComponent(App);
    await fixture.whenStable();
    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.querySelector('h1')?.textContent).toContain('YT Grabber');
  });
});
