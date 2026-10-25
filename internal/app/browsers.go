package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ytgrabber/internal/accounts"
	"ytgrabber/internal/browsers"
	"ytgrabber/internal/model"
	"ytgrabber/internal/ytdlp"
)

// extractTimeout leaves time for the user to answer the macOS Keychain
// prompt yt-dlp triggers.
const extractTimeout = 3 * time.Minute

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
	if ok, note := browsers.Connectable(profile, a.browserEnv.GOOS); !ok {
		return nil, errors.New(note)
	}
	var cookies []accounts.Cookie
	var err error
	if profile.Browser.Engine == browsers.EngineFirefox {
		cookies, err = browsers.ReadFirefoxCookies(profile, domain, a.paths.RunDir(), time.Now())
	} else {
		cookies, err = a.extractWithYtDlp(profile, domain)
	}
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

// extractWithYtDlp has yt-dlp decrypt a Chromium profile's cookies:
// that needs the browser's key from the OS keychain (macOS asks the user
// once), DPAPI or the Linux keyring, which yt-dlp already knows how to
// reach. It is pointed at a URL that isn't one, so it does nothing but load
// the cookies and, on exit, write them to the jar. The jar holds every
// site's cookies, so it lives in a private temporary directory only until
// the site's cookies are filtered out.
func (a *App) extractWithYtDlp(profile browsers.Profile, domain string) ([]accounts.Cookie, error) {
	if err := a.checkToolsReady(); err != nil {
		return nil, err
	}
	a.mu.RLock()
	toolSet := a.tools
	a.mu.RUnlock()

	if err := accounts.EnsurePrivateDir(a.paths.RunDir()); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(a.paths.RunDir(), "extract-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	jar := filepath.Join(dir, "jar.txt")

	ctx, cancel := context.WithTimeout(context.Background(), extractTimeout)
	defer cancel()
	cmd := toolSet.Command(ctx, toolSet.YtDlp.Path,
		"--ignore-config", "--cookies-from-browser", profile.YtDlpSpec(), "--cookies", jar, "--", "notaurl")
	output, _ := cmd.CombinedOutput() // always fails on the fake URL
	if ctx.Err() != nil {
		return nil, errors.New("извлечение cookies не уложилось во время: возможно, не был подтверждён доступ к Связке ключей")
	}

	file, err := os.Open(jar)
	if err != nil {
		return nil, extractionError(profile, string(output))
	}
	parsed, err := accounts.ParseCookies(file)
	file.Close()
	if err != nil {
		return nil, extractionError(profile, string(output))
	}
	cookies := accounts.FilterCookies(parsed, domain, time.Now())
	if len(cookies) == 0 && strings.Contains(string(output), "could not be decrypted") {
		return nil, fmt.Errorf("yt-dlp не смог расшифровать cookies %s · %s. Войдите на сайт в Firefox или экспортируйте cookies.txt", profile.Browser.Name, profile.Name)
	}
	return cookies, nil
}

// extractionError explains why yt-dlp wrote no jar, from its output.
func extractionError(profile browsers.Profile, output string) error {
	lower := strings.ToLower(output)
	switch {
	case strings.Contains(lower, "could not copy chrome cookie database"), strings.Contains(lower, "database is locked"):
		return fmt.Errorf("%s держит базу cookies открытой: закройте браузер и повторите", profile.Browser.Name)
	case strings.Contains(lower, "keyring"), strings.Contains(lower, "keychain"), strings.Contains(lower, "secretstorage"):
		return fmt.Errorf("нет доступа к хранилищу паролей, где %s держит ключ cookies: %s", profile.Browser.Name, lastError(output))
	}
	if message := lastError(output); message != "" {
		return fmt.Errorf("не удалось извлечь cookies из %s: %s", profile.Browser.Name, message)
	}
	return fmt.Errorf("не удалось извлечь cookies из %s", profile.Browser.Name)
}

// lastError is yt-dlp's last ERROR line, ignoring the one about the fake
// URL every extraction run ends with.
func lastError(output string) string {
	var lines []string
	for _, line := range splitLines(output) {
		if !strings.Contains(line, "notaurl") {
			lines = append(lines, line)
		}
	}
	if message := ytdlp.ExtractErrorMessage(lines); message != "yt-dlp завершился с ошибкой" {
		return message
	}
	return ""
}

// decorateAccount fills the fields computed at read time.
func decorateAccount(account *model.Account) {
	account.LoginKnown = accounts.IsKnownSite(account.Domain)
	account.BrowserName = ""
	if browser, ok := browsers.Find(account.Browser); ok {
		account.BrowserName = browser.Name
	}
}
