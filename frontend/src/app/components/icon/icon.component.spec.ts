import { TestBed } from '@angular/core/testing';
import { IconComponent, IconName } from './icon.component';

describe('IconComponent', () => {
    beforeEach(async () => {
        await TestBed.configureTestingModule({ imports: [IconComponent] }).compileComponents();
    });

    function render(name: IconName, size?: number): SVGElement {
        const fixture = TestBed.createComponent(IconComponent);
        fixture.componentRef.setInput('name', name);
        if (size !== undefined) {
            fixture.componentRef.setInput('size', size);
        }
        fixture.detectChanges();
        return (fixture.nativeElement as HTMLElement).querySelector('svg')!;
    }

    it('draws every icon in the current colour', () => {
        for (const name of ['close', 'play', 'pause', 'music', 'folder', 'trash'] as IconName[]) {
            const svg = render(name);
            expect(svg.getAttribute('stroke')).toBe('currentColor');
            expect(svg.children.length).toBeGreaterThan(0);
        }
    });

    it('draws the close cross heavier than the others', () => {
        expect(Number(render('close').getAttribute('stroke-width'))).toBeGreaterThan(
            Number(render('trash').getAttribute('stroke-width')),
        );
    });

    it('takes a size', () => {
        const svg = render('folder', 24);
        expect(svg.getAttribute('width')).toBe('24');
        expect(svg.getAttribute('height')).toBe('24');
    });
});
