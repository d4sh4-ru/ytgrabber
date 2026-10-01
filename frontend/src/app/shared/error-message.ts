/**
 * Wails rejects a bound method's promise with the Go error's text (a plain
 * string), while failures on the JS side arrive as Error objects.
 */
export function errorMessage(error: unknown): string {
    if (typeof error === 'string') {
        return error;
    }
    if (error instanceof Error) {
        return error.message;
    }
    return 'Неизвестная ошибка';
}
