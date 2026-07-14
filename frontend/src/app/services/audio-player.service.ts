import { Injectable } from '@angular/core';
import { BehaviorSubject, Observable } from 'rxjs';

export interface AudioTrack {
  readonly id: string;
  readonly title: string;
  readonly src: string;
}

@Injectable({ providedIn: 'root' })
export class AudioPlayerService {
  public readonly currentTrack$: Observable<AudioTrack | null>;

  private readonly currentTrackSubject$ = new BehaviorSubject<AudioTrack | null>(null);

  public constructor() {
    this.currentTrack$ = this.currentTrackSubject$.asObservable();
  }

  public play(track: AudioTrack): void {
    this.currentTrackSubject$.next(track);
  }
}
