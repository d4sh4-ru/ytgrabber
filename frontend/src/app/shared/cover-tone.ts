/** A background and a foreground for media without a cover image. */
export interface CoverTone {
    readonly background: string;
    readonly foreground: string;
}

// Dark tints with light notes: they read the same in both themes.
const TONES: readonly CoverTone[] = [
    { background: '#3a2f5a', foreground: '#d7c8ff' },
    { background: '#1f3d4a', foreground: '#a8e0f0' },
    { background: '#4a3420', foreground: '#f3d1a2' },
    { background: '#203a2c', foreground: '#b5e6c8' },
    { background: '#4a2430', foreground: '#f5b8c6' },
    { background: '#2b3a55', foreground: '#bcd2ff' },
];

/** The same title always gets the same tone, so a track keeps its colour. */
export function coverTone(title: string): CoverTone {
    let hash = 0;
    for (const char of title) {
        hash = (hash * 31 + char.codePointAt(0)!) | 0;
    }
    return TONES[Math.abs(hash) % TONES.length];
}
