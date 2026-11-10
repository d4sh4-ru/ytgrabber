import { model } from '../api/models';
import { formatBytes } from './format-bytes';

/** The part of the Network Information API this needs; Chrome on Android has it. */
interface NetworkInformation {
    readonly type?: string;
    readonly saveData?: boolean;
}

/** The URL that sends item's file as an attachment under its own name. */
export function deviceDownloadUrl(item: model.VideoRecord): string {
    return `${item.mediaUrl}?download=1`;
}

/**
 * What to warn about before saving item over a metered connection, or
 * null to go ahead. Only browsers that tell the connection type (Chrome
 * on Android) can warn; elsewhere the download just starts.
 */
export function meteredDownloadWarning(item: model.VideoRecord): string | null {
    const connection = (navigator as Navigator & { connection?: NetworkInformation }).connection;
    const isMetered = connection?.type === 'cellular' || connection?.saveData === true;
    if (!isMetered || item.sizeBytes <= 0) {
        return null;
    }
    return `Файл весит ${formatBytes(item.sizeBytes)}, а телефон, похоже, в мобильной сети.`;
}

/** Starts saving url the way a click on a download link would. */
export function startDeviceDownload(url: string): void {
    const link = document.createElement('a');
    link.href = url;
    link.download = '';
    document.body.append(link);
    link.click();
    link.remove();
}
