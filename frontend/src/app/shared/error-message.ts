/**
 * Wails rejects a bound method's promise with the Go error's text (a plain
 * string), while failures on the JS side arrive as Error objects. Anything
 * else is shown as it is rather than swapped for a generic phrase, so the
 * real cause stays visible.
 */
export function errorMessage(error: unknown): string {
    if (typeof error === 'string') {
        return error;
    }
    if (error instanceof Error) {
        return error.message;
    }
    if (typeof error === 'object' && error !== null) {
        const message = (error as { message?: unknown }).message;
        if (typeof message === 'string' && message !== '') {
            return message;
        }
        try {
            const json = JSON.stringify(error);
            if (json && json !== '{}') {
                return json;
            }
        } catch {
            // Circular or otherwise unserialisable: nothing readable left.
        }
        return 'Неизвестная ошибка';
    }
    if (error === null || error === undefined) {
        return 'Неизвестная ошибка';
    }
    return String(error);
}
