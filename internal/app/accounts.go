package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"ytgrabber/internal/accounts"
	"ytgrabber/internal/model"
	"ytgrabber/internal/storage"
	"ytgrabber/internal/ytdlp"
)

const (
	accountCheckTimeout = 2 * time.Minute
	// maxCookiesFile bounds what is read back from yt-dlp.
	maxCookiesFile = 16 << 20
)

// GetAccounts возвращает подключённые аккаунты (без самих cookies).
func (a *App) GetAccounts() ([]*model.Account, error) {
	if err := a.ensureReady(); err != nil {
		return []*model.Account{}, err
	}
	list, err := a.db.LoadAccounts()
	if err != nil {
		return []*model.Account{}, fmt.Errorf("не удалось загрузить аккаунты: %w", err)
	}
	for _, account := range list {
		account.LoginKnown = accounts.IsKnownSite(account.Domain)
	}
	return list, nil
}

// ImportCookiesFile подключает (или обновляет) аккаунт сайта из файла
// cookies.txt: открывает диалог выбора файла, оставляет только cookies
// этого сайта и сохраняет их в зашифрованном виде. site — ссылка или домен.
// Отмена диалога возвращает nil без ошибки.
func (a *App) ImportCookiesFile(site string) (*model.Account, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	domain, err := accounts.SiteFromInput(site)
	if err != nil {
		return nil, err
	}

	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: fmt.Sprintf("Файл cookies.txt для %s", domain),
		Filters: []runtime.FileFilter{
			{DisplayName: "cookies.txt (*.txt)", Pattern: "*.txt"},
			{DisplayName: "Все файлы", Pattern: "*"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть диалог: %w", err)
	}
	if path == "" {
		return nil, nil
	}
	return a.importCookiesFromPath(domain, path)
}

// importCookiesFromPath is ImportCookiesFile after the dialog.
func (a *App) importCookiesFromPath(domain string, path string) (*model.Account, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть файл: %w", err)
	}
	parsed, err := accounts.ParseCookies(file)
	file.Close()
	if err != nil {
		return nil, err
	}
	cookies := accounts.FilterCookies(parsed, domain, time.Now())
	if len(cookies) == 0 {
		return nil, fmt.Errorf("в файле нет действующих cookies для %s. Экспортируйте cookies, открыв %s в браузере, где выполнен вход", domain, domain)
	}

	account, err := a.db.GetAccountByDomain(domain)
	now := timestamp()
	if errors.Is(err, storage.ErrAccountNotFound) {
		account = &model.Account{ID: uuid.NewString(), Domain: domain, CreatedAt: now}
	} else if err != nil {
		return nil, fmt.Errorf("не удалось прочитать аккаунт: %w", err)
	}
	account.Source = model.AccountFromFile
	account.Browser = ""
	account.Profile = ""
	account.RefreshedAt = now
	applyCookieStats(account, cookies)
	account.Status = localStatus(account)

	unlock := a.accounts.Lock(account.ID)
	defer unlock()
	if err := a.accounts.Save(account.ID, domain, cookies); err != nil {
		return nil, fmt.Errorf("не удалось сохранить cookies: %w", err)
	}
	if err := a.db.SaveAccount(account); err != nil {
		return nil, fmt.Errorf("не удалось сохранить аккаунт: %w", err)
	}

	log.Printf("ytgrabber: account %s imported from file: %d cookies, logged in: %v", domain, account.CookieCount, account.LoggedIn)
	a.publish("accounts-changed")
	return account, nil
}

// RemoveAccount отключает аккаунт: удаляет его зашифрованные cookies и
// запись о нём. Загрузки с этого сайта пойдут без входа.
func (a *App) RemoveAccount(id string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	unlock := a.accounts.Lock(id)
	defer unlock()

	if err := a.accounts.Delete(id); err != nil {
		return fmt.Errorf("не удалось удалить cookies: %w", err)
	}
	if err := a.db.DeleteAccount(id); err != nil {
		return fmt.Errorf("не удалось удалить аккаунт: %w", err)
	}
	a.publish("accounts-changed")
	return nil
}

// CheckAccount проверяет, действует ли вход: есть ли непросроченный
// cookie сессии, а для сайтов с проверочной ссылкой (YouTube) — пускает ли
// сайт с этими cookies (yt-dlp без скачивания).
func (a *App) CheckAccount(id string) (*model.Account, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	account, err := a.db.GetAccount(id)
	if err != nil {
		return nil, err
	}

	checkURL := accounts.CheckURL(account.Domain)
	unlock := a.accounts.Lock(id)
	cookies, err := a.accounts.Load(id, account.Domain)
	if err == nil {
		applyCookieStats(account, accounts.FilterCookies(cookies, account.Domain, time.Now()))
		// The cookies alone can't overrule the site: an account it refused
		// stays expired unless it can be tested online.
		if status := localStatus(account); checkURL != "" || account.Status != model.AccountExpired {
			account.Status = status
		}
	} else {
		log.Printf("ytgrabber: account %s: %v", account.Domain, err)
		account.Status = model.AccountError
	}
	saveErr := a.db.SaveAccount(account)
	unlock()
	if saveErr != nil {
		return nil, fmt.Errorf("не удалось сохранить аккаунт: %w", saveErr)
	}

	if checkURL != "" && account.Status == model.AccountOK {
		if err := a.checkAccountOnline(account, checkURL); err != nil {
			a.publish("accounts-changed")
			return nil, err
		}
		if account, err = a.db.GetAccount(id); err != nil {
			return nil, err
		}
	}

	account.LoginKnown = accounts.IsKnownSite(account.Domain)
	a.publish("accounts-changed")
	return account, nil
}

// checkAccountOnline asks yt-dlp to list one entry of a page that needs a
// login. A login error expires the account; any other failure is
// reported without touching it.
func (a *App) checkAccountOnline(account *model.Account, checkURL string) error {
	if err := a.checkToolsReady(); err != nil {
		return err
	}
	session := a.startAccountSession(account.Domain, "check-"+uuid.NewString())
	if session == nil {
		return errors.New("не удалось подготовить cookies для проверки")
	}

	a.mu.RLock()
	toolSet := a.tools
	a.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), accountCheckTimeout)
	defer cancel()
	cmd := toolSet.Command(ctx, toolSet.YtDlp.Path,
		"--ignore-config", "--flat-playlist", "--playlist-items", "1", "--simulate", "--no-warnings",
		"--cookies", session.path, "--", checkURL)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	message := ""
	if runErr != nil {
		message = ytdlp.ExtractErrorMessage(splitLines(stderr.String()))
	}
	loginRequired := runErr != nil && ytdlp.IsLoginRequired(message)
	a.finishAccountSession(session, ctx.Err() == nil, loginRequired)

	if runErr != nil && !loginRequired {
		if ctx.Err() != nil {
			return errors.New("проверка не уложилась во время, попробуйте позже")
		}
		return fmt.Errorf("не удалось проверить вход: %s", message)
	}
	return nil
}

// accountSession is one yt-dlp run with an account's cookies: they are
// decrypted into a private file for --cookies, and what yt-dlp writes
// back is merged into the snapshot afterwards.
type accountSession struct {
	account *model.Account
	base    []accounts.Cookie
	path    string
}

// startAccountSession prepares the cookies of site's account, if there is
// a usable one; nil means "run without an account". Problems are logged,
// never fatal: a download that might not need the login still runs.
func (a *App) startAccountSession(site string, runName string) *accountSession {
	if site == "" {
		return nil
	}
	account, err := a.db.GetAccountByDomain(site)
	if err != nil {
		if !errors.Is(err, storage.ErrAccountNotFound) {
			log.Printf("ytgrabber: failed to read account %s: %v", site, err)
		}
		return nil
	}
	if account.Status == model.AccountError {
		return nil
	}

	unlock := a.accounts.Lock(account.ID)
	cookies, err := a.accounts.Load(account.ID, site)
	unlock()
	if err != nil {
		log.Printf("ytgrabber: account %s unusable: %v", site, err)
		account.Status = model.AccountError
		if err := a.db.SaveAccount(account); err != nil {
			log.Printf("ytgrabber: failed to save account %s: %v", site, err)
		}
		a.publish("accounts-changed")
		return nil
	}

	path := filepath.Join(a.paths.RunDir(), runName+".cookies")
	if err := accounts.WritePrivateFile(path, accounts.FormatCookies(cookies)); err != nil {
		log.Printf("ytgrabber: failed to prepare cookies for %s: %v", site, err)
		return nil
	}
	return &accountSession{account: account, base: cookies, path: path}
}

// finishAccountSession merges the cookies yt-dlp wrote back (writeBack is
// false when it was killed and the file may be half-written), records the
// use and, on a login error, marks the account expired. The decrypted
// file is always removed.
func (a *App) finishAccountSession(s *accountSession, writeBack bool, loginRequired bool) {
	defer func() {
		if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("ytgrabber: failed to remove %s: %v", s.path, err)
		}
	}()

	unlock := a.accounts.Lock(s.account.ID)
	defer unlock()

	account, err := a.db.GetAccount(s.account.ID)
	if err != nil {
		return // disconnected meanwhile
	}
	// Re-imported meanwhile: the new cookies win over this run's.
	if account.RefreshedAt != s.account.RefreshedAt {
		return
	}

	if writeBack {
		if err := a.writeBackCookies(account, s); err != nil {
			log.Printf("ytgrabber: account %s: cookies not updated: %v", account.Domain, err)
		}
	}
	account.LastUsedAt = timestamp()
	if loginRequired {
		account.Status = model.AccountExpired
	}
	if err := a.db.SaveAccount(account); err != nil {
		log.Printf("ytgrabber: failed to save account %s: %v", account.Domain, err)
	}
	a.publish("accounts-changed")
}

// writeBackCookies must be called with the account locked.
func (a *App) writeBackCookies(account *model.Account, s *accountSession) error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	if len(data) > maxCookiesFile {
		return errors.New("файл cookies слишком большой")
	}
	parsed, err := accounts.ParseCookies(bytes.NewReader(data))
	if err != nil {
		return err
	}
	now := time.Now()
	updated := accounts.FilterCookies(parsed, account.Domain, now)
	// An empty jar means yt-dlp didn't get to write it, not a logout.
	if len(updated) == 0 {
		return nil
	}

	current, err := a.accounts.Load(account.ID, account.Domain)
	if err != nil {
		return err
	}
	merged := accounts.FilterCookies(accounts.MergeCookies(s.base, current, updated), account.Domain, now)
	if err := a.accounts.Save(account.ID, account.Domain, merged); err != nil {
		return err
	}
	applyCookieStats(account, merged)
	return nil
}

// siteOfURL is the account key of a download URL, "" if it has none.
func siteOfURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	site, err := accounts.SiteOf(parsed.Hostname())
	if err != nil {
		return ""
	}
	return site
}

// applyCookieStats updates what the UI shows about the cookies. The status
// is left alone: a download that refreshed the cookies doesn't prove that
// a login an earlier one was refused with works again.
func applyCookieStats(account *model.Account, cookies []accounts.Cookie) {
	account.CookieCount = len(cookies)
	account.LoginKnown, account.LoggedIn = accounts.LoginState(account.Domain, cookies)
}

// localStatus judges an account by its cookies alone.
func localStatus(account *model.Account) model.AccountStatus {
	if account.CookieCount == 0 || (account.LoginKnown && !account.LoggedIn) {
		return model.AccountExpired
	}
	return model.AccountOK
}

// cleanRunDir removes decrypted cookies left behind by a crash.
func (a *App) cleanRunDir() {
	if err := removeAppDir(a.paths.RunDir()); err != nil {
		log.Printf("ytgrabber: failed to clean %s: %v", a.paths.RunDir(), err)
	}
}

func splitLines(text string) []string {
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}

func timestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
