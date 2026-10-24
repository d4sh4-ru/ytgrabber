import { Injectable } from '@angular/core';
import { BehaviorSubject, Observable } from 'rxjs';
import {
    CheckAccount,
    ConnectBrowserProfile,
    GetAccounts,
    ImportCookiesFile,
    RefreshAccount,
    RemoveAccount,
    ScanBrowsers,
} from '../wailsjs/wailsjs/go/app/App';
import { browsers, model } from '../wailsjs/wailsjs/go/models';
import { fromWailsEvent } from '../shared/wails-event';

/**
 * Connected site accounts. The list is cached in a subject because the
 * settings screen shows it and the download form needs to know whether
 * there are any; the backend announces every change with an event.
 */
@Injectable({ providedIn: 'root' })
export class AccountsService {
    public readonly accounts$: Observable<model.Account[]>;

    private readonly accountsSubject$ = new BehaviorSubject<model.Account[]>([]);

    public constructor() {
        this.accounts$ = this.accountsSubject$.asObservable();

        // Root service: lives as long as the app, so the subscription does too.
        fromWailsEvent<void>('accounts-changed').subscribe((): void => {
            void this.refresh().catch((): void => undefined);
        });
    }

    public async refresh(): Promise<model.Account[]> {
        const accounts = await GetAccounts();
        this.accountsSubject$.next(accounts);
        return accounts;
    }

    /**
     * Opens a file dialog and connects (or refreshes) the site's account
     * from the chosen cookies.txt. Resolves to null if the dialog was
     * cancelled.
     */
    public async importCookiesFile(site: string): Promise<model.Account | null> {
        const account = await ImportCookiesFile(site);
        await this.refresh();
        return account ?? null;
    }

    /** Browser profiles that have cookies of the site; reads names only. */
    public scanBrowsers(site: string): Promise<browsers.Match[]> {
        return ScanBrowsers(site);
    }

    public async connectProfile(site: string, match: browsers.Match): Promise<model.Account> {
        const account = await ConnectBrowserProfile(site, match.browserId, match.profileDir);
        await this.refresh();
        return account;
    }

    /** Re-reads a browser account's cookies from the same profile. */
    public async refreshFromBrowser(id: string): Promise<model.Account> {
        const account = await RefreshAccount(id);
        await this.refresh();
        return account;
    }

    /** The account a site's downloads use, if one is connected. */
    public findBySite(site: string): model.Account | null {
        return this.accountsSubject$.value.find((account) => account.domain === site) ?? null;
    }

    public async check(id: string): Promise<model.Account> {
        const account = await CheckAccount(id);
        await this.refresh();
        return account;
    }

    public async remove(id: string): Promise<void> {
        await RemoveAccount(id);
        await this.refresh();
    }
}
