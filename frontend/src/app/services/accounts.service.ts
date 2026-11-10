import { inject, Injectable } from '@angular/core';
import { BehaviorSubject, Observable } from 'rxjs';
import { browsers, model } from '../api/models';
import { Transport } from '../api/transport';

/**
 * Connected site accounts. The list is cached in a subject because the
 * settings screen shows it and the download form needs to know whether
 * there are any; the backend announces every change with an event.
 */
@Injectable({ providedIn: 'root' })
export class AccountsService {
    public readonly accounts$: Observable<model.Account[]>;

    private readonly transport = inject(Transport);
    private readonly accountsSubject$ = new BehaviorSubject<model.Account[]>([]);

    public constructor() {
        this.accounts$ = this.accountsSubject$.asObservable();

        // Root service: lives as long as the app, so the subscription does too.
        this.transport.events$<void>('accounts-changed').subscribe((): void => {
            void this.refresh().catch((): void => undefined);
        });
    }

    public async refresh(): Promise<model.Account[]> {
        const accounts = await this.transport.call<model.Account[]>('GetAccounts');
        this.accountsSubject$.next(accounts);
        return accounts;
    }

    /**
     * Connects (or refreshes) the site's account from a cookies.txt: on the
     * desktop the backend opens a file dialog, a browser passes the file
     * the user picked. Resolves to null if the dialog was cancelled.
     */
    public async importCookiesFile(site: string, file?: File): Promise<model.Account | null> {
        const account = await this.transport.call<model.Account | null>(
            'ImportCookiesFile',
            file ? [site, file] : [site],
        );
        await this.refresh();
        return account ?? null;
    }

    /** Browser profiles that have cookies of the site; reads names only. */
    public scanBrowsers(site: string): Promise<browsers.Match[]> {
        return this.transport.call<browsers.Match[]>('ScanBrowsers', [site]);
    }

    public async connectProfile(site: string, match: browsers.Match): Promise<model.Account> {
        const account = await this.transport.call<model.Account>('ConnectBrowserProfile', [
            site,
            match.browserId,
            match.profileDir,
        ]);
        await this.refresh();
        return account;
    }

    /** Re-reads a browser account's cookies from the same profile. */
    public async refreshFromBrowser(id: string): Promise<model.Account> {
        const account = await this.transport.call<model.Account>('RefreshAccount', [id]);
        await this.refresh();
        return account;
    }

    /** The account a site's downloads use, if one is connected. */
    public findBySite(site: string): model.Account | null {
        return this.accountsSubject$.value.find((account) => account.domain === site) ?? null;
    }

    public async check(id: string): Promise<model.Account> {
        const account = await this.transport.call<model.Account>('CheckAccount', [id]);
        await this.refresh();
        return account;
    }

    /**
     * The sites connected on this computer, while the app works with a
     * server: their cookies can be sent there.
     */
    public localAccounts(): Promise<model.Account[]> {
        return this.transport.call<model.Account[]>('GetLocalAccounts');
    }

    /** Connects the server's account of a site with this computer's cookies of it. */
    public async sendToServer(localId: string): Promise<model.Account> {
        const account = await this.transport.call<model.Account>('SendAccountToServer', [localId]);
        await this.refresh();
        return account;
    }

    public async remove(id: string): Promise<void> {
        await this.transport.call<void>('RemoveAccount', [id]);
        await this.refresh();
    }
}
