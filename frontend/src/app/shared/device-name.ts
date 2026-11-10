const PLATFORMS: readonly (readonly [RegExp, string])[] = [
    [/iPhone/, 'iPhone'],
    [/iPad/, 'iPad'],
    [/Android/, 'Android'],
    [/Macintosh|Mac OS X/, 'Mac'],
    [/Windows/, 'Windows'],
    [/CrOS/, 'ChromeOS'],
    [/Linux/, 'Linux'],
];

// Order matters: Edge and Opera say "Chrome" too, Chrome says "Safari".
const BROWSERS: readonly (readonly [RegExp, string])[] = [
    [/Edg\//, 'Edge'],
    [/OPR\/|Opera/, 'Opera'],
    [/YaBrowser/, 'Яндекс Браузер'],
    [/Firefox|FxiOS/, 'Firefox'],
    [/Chrome|CriOS/, 'Chrome'],
    [/Safari/, 'Safari'],
];

/** A name for this device in the list of devices: "iPhone · Safari". */
export function guessDeviceName(userAgent: string): string {
    const platform = PLATFORMS.find(([pattern]) => pattern.test(userAgent))?.[1];
    const browser = BROWSERS.find(([pattern]) => pattern.test(userAgent))?.[1];
    return [platform, browser].filter(Boolean).join(' · ') || 'Браузер';
}
