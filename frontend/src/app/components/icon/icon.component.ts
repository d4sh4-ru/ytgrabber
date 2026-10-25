import { ChangeDetectionStrategy, Component, Input } from '@angular/core';

export type IconName = 'close' | 'play' | 'pause' | 'music' | 'folder' | 'trash';

/** Stroke width per icon; the close cross is drawn heavier to read at small sizes. */
const STROKE_WIDTHS: Readonly<Record<IconName, number>> = {
    close: 2.5,
    play: 2,
    pause: 2,
    music: 1.75,
    folder: 1.75,
    trash: 1.75,
};

/**
 * A line icon drawn in the current text colour, so it follows the button
 * it sits in. The shapes are from Lucide (ISC licence, lucide.dev); they
 * replace emoji, which every OS renders in its own colourful style.
 */
@Component({
    selector: 'app-icon',
    standalone: true,
    templateUrl: './icon.component.html',
    styleUrl: './icon.component.scss',
    changeDetection: ChangeDetectionStrategy.OnPush,
})
export class IconComponent {
    @Input({ required: true })
    public name!: IconName;

    /** Size in pixels. */
    @Input()
    public size = 16;

    protected get strokeWidth(): number {
        return STROKE_WIDTHS[this.name];
    }
}
