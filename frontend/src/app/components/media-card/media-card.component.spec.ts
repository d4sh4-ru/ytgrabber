import { TestBed } from '@angular/core/testing';
import { MediaCardComponent } from './media-card.component';
import { model } from '../../wailsjs/wailsjs/go/models';

describe('MediaCardComponent', () => {
    beforeEach(async () => {
        await TestBed.configureTestingModule({ imports: [MediaCardComponent] }).compileComponents();
    });

    function render(item: Partial<model.VideoRecord>): HTMLElement {
        const fixture = TestBed.createComponent(MediaCardComponent);
        fixture.componentRef.setInput(
            'item',
            model.VideoRecord.createFrom({ id: 'v', title: 'clip', mediaKind: 'video', ...item }),
        );
        fixture.detectChanges();
        return fixture.nativeElement as HTMLElement;
    }

    it('shows how much was watched', () => {
        const element = render({ durationSeconds: 600, positionSeconds: 150 });
        const bar = element.querySelector<HTMLElement>('.media-card__watched-bar');
        expect(bar?.style.width).toBe('25%');
        expect(element.querySelector('.media-card__watched')?.getAttribute('title')).toBe(
            'Остановились на 2:30',
        );
    });

    it('shows no bar for unwatched or finished entries', () => {
        expect(
            render({ durationSeconds: 600, positionSeconds: 0 }).querySelector(
                '.media-card__watched',
            ),
        ).toBeNull();
        expect(
            render({ durationSeconds: 0, positionSeconds: 30 }).querySelector(
                '.media-card__watched',
            ),
        ).toBeNull();
    });
});
