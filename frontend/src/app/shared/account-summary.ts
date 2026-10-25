import { model } from '../wailsjs/wailsjs/go/models';
import { pluralRu } from './format-relative-time';

export function cookieCountLabel(count: number): string {
    return `${count} ${pluralRu(count, 'cookie', 'cookies', 'cookies')}`;
}

/** What connecting an account achieved, for a toast. */
export function connectedMessage(account: model.Account): string {
    const cookies = cookieCountLabel(account.cookieCount);
    if (!account.loginKnown) {
        return `${account.domain} подключён: ${cookies}.`;
    }
    if (account.loggedIn) {
        return `${account.domain} подключён: ${cookies}, вход найден.`;
    }
    return `${account.domain}: ${cookies}, но вход не найден. Экспортируйте cookies со страницы сайта, где вы вошли в аккаунт.`;
}
