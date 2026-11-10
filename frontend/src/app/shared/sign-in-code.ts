/** A code shaped like the ones the server issues: XXXXX-XXXXX. */
const CODE_PATTERN = /^[A-Z0-9]{5}-[A-Z0-9]{5}$/i;

/** What a scanned QR code holds, as far as signing in goes. */
export type ScannedSignIn =
    | { readonly kind: 'code'; readonly code: string }
    | { readonly kind: 'other-server'; readonly server: string }
    | { readonly kind: 'not-a-code' };

/**
 * Takes the sign-in code out of what a QR code says: a link made in
 * «Войти на другом» (…/?code=…) or a bare code. A link to another server
 * is refused: its code means nothing here, and the user likely meant to
 * open that server instead. origin is this app's.
 */
export function signInCodeFromScan(text: string, origin: string): ScannedSignIn {
    const trimmed = text.trim();
    if (CODE_PATTERN.test(trimmed)) {
        return { kind: 'code', code: trimmed.toUpperCase() };
    }
    let url: URL;
    try {
        url = new URL(trimmed);
    } catch {
        return { kind: 'not-a-code' };
    }
    const code = url.searchParams.get('code')?.trim() ?? '';
    if (!code || (url.protocol !== 'https:' && url.protocol !== 'http:')) {
        return { kind: 'not-a-code' };
    }
    if (url.origin !== origin) {
        return { kind: 'other-server', server: url.host };
    }
    return { kind: 'code', code };
}
