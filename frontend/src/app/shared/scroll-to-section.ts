/**
 * Scrolls a section to the top of the page and keeps it there while the
 * page above it is still loading and growing (a list arriving shifts it
 * down). Stops after `settleMs` or as soon as the user scrolls by hand.
 */
export function scrollToSection(id: string, settleMs = 1500): void {
    const element = document.getElementById(id);
    if (!element) {
        return;
    }
    element.scrollIntoView({ block: 'start' });
    // Where it lands once the page is long enough to scroll that far.
    const target = parseFloat(getComputedStyle(element).scrollMarginTop) || 0;

    const stop = (): void => {
        clearInterval(interval);
        clearTimeout(timer);
        for (const type of USER_SCROLL_EVENTS) {
            window.removeEventListener(type, stop);
        }
    };
    // A timer rather than a ResizeObserver: it also runs when the window
    // isn't drawing frames (hidden, in the background).
    const interval = setInterval((): void => {
        if (Math.abs(element.getBoundingClientRect().top - target) > 1) {
            element.scrollIntoView({ block: 'start' });
        }
    }, 50);
    const timer = setTimeout(stop, settleMs);
    for (const type of USER_SCROLL_EVENTS) {
        window.addEventListener(type, stop, { once: true, passive: true });
    }
}

const USER_SCROLL_EVENTS = ['wheel', 'touchstart', 'keydown', 'pointerdown'] as const;
