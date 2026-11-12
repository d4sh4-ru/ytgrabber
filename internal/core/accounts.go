package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

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
func (a *App) GetAccounts(user UserID) ([]*model.Account, error) {
	if err := a.ensureReady(); err != nil {
		return []*model.Account{}, err
	}
	list, err := a.db.LoadAccounts(string(user))
	if err != nil {
		return []*model.Account{}, fmt.Errorf("не удалось загрузить аккаунты: %w", err)
	}
	for _, account := range list {
		decorateAccount(account)
	}
	return list, nil
}

// ImportCookiesFile подключает (или обновляет) аккаунт сайта из файла
// cookies.txt по пути path (выбранному в диалоге адаптером): оставляет
// только cookies этого сайта и сохраняет их в зашифрованном виде.
// site — ссылка или домен.
func (a *App) ImportCookiesFile(user UserID, site string, path string) (*model.Account, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	domain, err := accounts.SiteFromInput(site)
	if err != nil {
		return nil, err
	}
	return a.importCookiesFromPath(user, domain, path)
}

// ImportCookies — то же, что ImportCookiesFile, но cookies.txt приходит
// потоком: сервер получает файл загрузкой, а не путём на диске.
func (a *App) ImportCookies(user UserID, site string, r io.Reader) (*model.Account, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	domain, err := accounts.SiteFromInput(site)
	if err != nil {
		return nil, err
	}
	return a.importCookies(user, domain, r)
}

// importCookiesFromPath is ImportCookiesFile with the site already checked.
func (a *App) importCookiesFromPath(user UserID, domain string, path string) (*model.Account, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть файл: %w", err)
	}
	defer file.Close()
	return a.importCookies(user, domain, file)
}

func (a *App) importCookies(user UserID, domain string, r io.Reader) (*model.Account, error) {
	parsed, err := accounts.ParseCookies(r)
	if err != nil {
		return nil, err
	}
	cookies := accounts.FilterCookies(parsed, domain, time.Now())
	if len(cookies) == 0 {
		return nil, fmt.Errorf("в файле нет действующих cookies для %s. Экспортируйте cookies, открыв %s в браузере, где выполнен вход", domain, domain)
	}

	return a.saveAccountCookies(user, domain, cookies, accountOrigin{source: model.AccountFromFile})
}

// accountOrigin is where an account's cookies came from.
type accountOrigin struct {
	source     model.AccountSource
	browser    string // browser id, see package browsers
	profile    string // display name
	profileDir string
}

// saveAccountCookies creates or refreshes the account of domain with
// cookies already filtered to it.
func (a *App) saveAccountCookies(user UserID, domain string, cookies []accounts.Cookie, origin accountOrigin) (*model.Account, error) {
	account, err := a.db.GetAccountByDomain(string(user), domain)
	now := timestamp()
	if errors.Is(err, storage.ErrAccountNotFound) {
		account = &model.Account{ID: uuid.NewString(), UserID: string(user), Domain: domain, CreatedAt: now}
	} else if err != nil {
		return nil, fmt.Errorf("не удалось прочитать аккаунт: %w", err)
	}
	account.Source = origin.source
	account.Browser = origin.browser
	account.Profile = origin.profile
	account.ProfileDir = origin.profileDir
	account.RefreshedAt = now
	applyCookieStats(account, cookies)
	account.Status = localStatus(account)

	unlock := a.accounts.Lock(account.ID)
	defer unlock()
	if err := a.accounts.Save(account.ID, account.UserID, domain, cookies); err != nil {
		return nil, fmt.Errorf("не удалось сохранить cookies: %w", err)
	}
	if err := a.db.SaveAccount(account); err != nil {
		return nil, fmt.Errorf("не удалось сохранить аккаунт: %w", err)
	}

	log.Printf("ytgrabber: account %s connected (%s %s): %d cookies, logged in: %v",
		domain, origin.source, origin.browser, account.CookieCount, account.LoggedIn)
	a.publishTo(user, "accounts-changed")
	return account, nil
}

// RemoveAccount отключает аккаунт: удаляет его зашифрованные cookies и
// запись о нём. Загрузки с этого сайта пойдут без входа.
func (a *App) RemoveAccount(user UserID, id string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	// Only the owner may: anyone else gets "not found".
	if _, err := a.db.GetAccount(string(user), id); err != nil {
		return err
	}
	unlock := a.accounts.Lock(id)
	defer unlock()

	if err := a.accounts.Delete(id); err != nil {
		return fmt.Errorf("не удалось удалить cookies: %w", err)
	}
	if err := a.db.DeleteAccount(string(user), id); err != nil {
		return fmt.Errorf("не удалось удалить аккаунт: %w", err)
	}
	a.publishTo(user, "accounts-changed")
	return nil
}

// AccountCookies is the account's cookies as a cookies.txt, for the
// desktop to hand them to a server: only that site's unexpired cookies,
// as they were stored. They leave the encrypted store only for that.
func (a *App) AccountCookies(user UserID, id string) (*model.Account, []byte, error) {
	if err := a.ensureReady(); err != nil {
		return nil, nil, err
	}
	account, err := a.db.GetAccount(string(user), id)
	if err != nil {
		return nil, nil, err
	}
	unlock := a.accounts.Lock(id)
	cookies, err := a.accounts.Load(id, account.UserID, account.Domain)
	unlock()
	if err != nil {
		return nil, nil, fmt.Errorf("не удалось прочитать cookies %s: %w", account.Domain, err)
	}
	cookies = accounts.FilterCookies(cookies, account.Domain, time.Now())
	if len(cookies) == 0 {
		return nil, nil, fmt.Errorf("у %s не осталось действующих cookies: обновите вход", account.Domain)
	}
	decorateAccount(account)
	return account, accounts.FormatCookies(cookies), nil
}

// CheckAccount проверяет, действует ли вход: есть ли непросроченный
// cookie сессии, а для сайтов с проверочной ссылкой (YouTube) — пускает ли
// сайт с этими cookies (yt-dlp без скачивания).
func (a *App) CheckAccount(user UserID, id string) (*model.Account, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	account, err := a.db.GetAccount(string(user), id)
	if err != nil {
		return nil, err
	}

	checkURL := accounts.CheckURL(account.Domain)
	unlock := a.accounts.Lock(id)
	cookies, err := a.accounts.Load(id, account.UserID, account.Domain)
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
			a.publishTo(user, "accounts-changed")
			return nil, err
		}
		if account, err = a.db.GetAccount(string(user), id); err != nil {
			return nil, err
		}
	}

	decorateAccount(account)
	a.publishTo(user, "accounts-changed")
	return account, nil
}

// checkAccountOnline asks yt-dlp to list one entry of a page that needs a
// login. A login error expires the account; any other failure is
// reported without touching it.
func (a *App) checkAccountOnline(account *model.Account, checkURL string) error {
	if err := a.checkToolsReady(); err != nil {
		return err
	}
	session := a.startAccountSession(UserID(account.UserID), account.Domain, "check-"+uuid.NewString())
	if session == nil {
		return errors.New("не удалось подготовить cookies для проверки")
	}

	a.mu.RLock()
	toolSet := a.tools
	a.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), accountCheckTimeout)
	defer cancel()
	args := []string{"--ignore-config", "--encoding", "utf-8", "--flat-playlist", "--playlist-items", "1", "--simulate", "--no-warnings",
		"--cookies", session.path}
	if proxy := a.proxy(); proxy != "" {
		args = append(args, "--proxy", proxy)
	}
	cmd := toolSet.Command(ctx, toolSet.YtDlp.Path, append(args, "--", checkURL)...)
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

// startAccountSession prepares the cookies of user's account on site, if there is
// a usable one; nil means "run without an account". Problems are logged,
// never fatal: a download that might not need the login still runs.
func (a *App) startAccountSession(user UserID, site string, runName string) *accountSession {
	if site == "" {
		return nil
	}
	account, err := a.db.GetAccountByDomain(string(user), site)
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
	cookies, err := a.accounts.Load(account.ID, account.UserID, site)
	unlock()
	if err != nil {
		log.Printf("ytgrabber: account %s unusable: %v", site, err)
		account.Status = model.AccountError
		if err := a.db.SaveAccount(account); err != nil {
			log.Printf("ytgrabber: failed to save account %s: %v", site, err)
		}
		a.publishTo(user, "accounts-changed")
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

	account, err := a.db.GetAccount(s.account.UserID, s.account.ID)
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
	a.publishTo(UserID(account.UserID), "accounts-changed")
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

	current, err := a.accounts.Load(account.ID, account.UserID, account.Domain)
	if err != nil {
		return err
	}
	merged := accounts.FilterCookies(accounts.MergeCookies(s.base, current, updated), account.Domain, now)
	if err := a.accounts.Save(account.ID, account.UserID, account.Domain, merged); err != nil {
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
