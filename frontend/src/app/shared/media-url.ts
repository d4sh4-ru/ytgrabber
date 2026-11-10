/**
 * url with the marker that makes the service worker leave the request to
 * the browser. Media goes past it: Safari loses seeking (Range requests)
 * in a video whose requests a service worker answers, and the files
 * aren't cached anyway.
 */
export function bypassServiceWorker(url: string): string {
    if (!url) {
        return url;
    }
    return `${url}${url.includes('?') ? '&' : '?'}ngsw-bypass=1`;
}
