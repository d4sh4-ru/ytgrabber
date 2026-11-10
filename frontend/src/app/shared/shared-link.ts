/** Where the share sheet sends what's shared to the app (manifest share_target). */
export const SHARE_PATH = '/share';

const LINK_PATTERN = /https?:\/\/\S+/;

/**
 * The link shared to the app through the share sheet, or null when the
 * page wasn't opened that way. Apps put it in different fields: YouTube
 * sends it in text, often after the video's title, so the first link in
 * any field wins.
 */
export function sharedLink(location: URL): string | null {
    if (location.pathname !== SHARE_PATH) {
        return null;
    }
    for (const field of ['url', 'text', 'title']) {
        const match = location.searchParams.get(field)?.match(LINK_PATTERN);
        if (match) {
            return match[0];
        }
    }
    return null;
}
