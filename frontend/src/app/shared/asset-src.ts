/**
 * WKWebView blocks `<video src="/absolute/fs/path">` (and `<img>`/`<audio>`
 * the same way) — arbitrary filesystem access is off-limits for security
 * reasons, not a bug. Downloaded videos, extracted audio, and thumbnails are
 * all served through Wails' asset server instead, under "/videos/" (see
 * main.go's newVideoHandler), so callers only ever need a file's basename
 * to build a same-origin URL.
 */
export function buildAssetSrc(filename: string): string {
  return `/videos/${encodeURIComponent(filename)}`;
}
