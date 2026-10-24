package app

import (
	"errors"
	"fmt"
	"time"

	"ytgrabber/internal/accounts"
	"ytgrabber/internal/browsers"
	"ytgrabber/internal/model"
)

// ScanBrowsers ищет профили браузеров, в которых есть cookies сайта, и
// показывает, где выполнен вход. Читаются только имена cookies и время
// последнего доступа, без значений и без обращения к связке ключей.
func (a *App) ScanBrowsers(site string) ([]browsers.Match, error) {
	if err := a.ensureReady(); err != nil {
		return []browsers.Match{}, err
	}
	domain, err := accounts.SiteFromInput(site)
	if err != nil {
		return []browsers.Match{}, err
	}
	matches := browsers.Scan(a.browserEnv, domain, a.paths.RunDir())
	if matches == nil {
		matches = []browsers.Match{}
	}
	return matches, nil
}

// ConnectBrowserProfile подключает аккаунт сайта из профиля браузера,
// найденного ScanBrowsers: из профиля берутся только cookies этого сайта.
func (a *App) ConnectBrowserProfile(site string, browserID string, profileDir string) (*model.Account, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	domain, err := accounts.SiteFromInput(site)
	if err != nil {
		return nil, err
	}
	profile, err := browsers.FindProfile(a.browserEnv, browserID, profileDir)
	if err != nil {
		return nil, err
	}
	return a.connectProfile(domain, profile)
}

// RefreshAccount заново берёт cookies из того же профиля браузера, из
// которого аккаунт был подключён. Аккаунт из файла обновляется новым
// файлом (ImportCookiesFile).
func (a *App) RefreshAccount(id string) (*model.Account, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	account, err := a.db.GetAccount(id)
	if err != nil {
		return nil, err
	}
	if account.Source != model.AccountFromBrowser {
		return nil, errors.New("этот аккаунт подключён из файла: выберите новый cookies.txt")
	}
	profile, err := browsers.FindProfile(a.browserEnv, account.Browser, account.ProfileDir)
	if err != nil {
		return nil, fmt.Errorf("профиль %s больше не найден: подключите сайт заново", account.Profile)
	}
	return a.connectProfile(account.Domain, profile)
}

func (a *App) connectProfile(domain string, profile browsers.Profile) (*model.Account, error) {
	if profile.Browser.Engine != browsers.EngineFirefox {
		return nil, fmt.Errorf("подключение из %s будет в следующей версии, пока используйте cookies.txt", profile.Browser.Name)
	}
	cookies, err := browsers.ReadFirefoxCookies(profile, domain, a.paths.RunDir(), time.Now())
	if err != nil {
		return nil, err
	}
	if len(cookies) == 0 {
		return nil, fmt.Errorf("в профиле %s · %s нет действующих cookies для %s", profile.Browser.Name, profile.Name, domain)
	}
	account, err := a.saveAccountCookies(domain, cookies, accountOrigin{
		source:     model.AccountFromBrowser,
		browser:    profile.Browser.ID,
		profile:    profile.Name,
		profileDir: profile.Dir,
	})
	if err != nil {
		return nil, err
	}
	decorateAccount(account)
	return account, nil
}

// decorateAccount fills the fields computed at read time.
func decorateAccount(account *model.Account) {
	account.LoginKnown = accounts.IsKnownSite(account.Domain)
	account.BrowserName = ""
	if browser, ok := browsers.Find(account.Browser); ok {
		account.BrowserName = browser.Name
	}
}
