package browsers

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"

	"ytgrabber/internal/accounts"
)

var (
	errNotFirefox = errors.New("этот браузер не основан на Firefox")
	// ErrUnreadable is what a locked database looks like: Chromium on
	// Windows keeps its cookie file exclusively open while running.
	ErrUnreadable = errors.New("не удалось прочитать cookies: закройте браузер и повторите")
)

// cookieName is what a scan reads from a database: no values.
type cookieName struct {
	host       string
	name       string
	lastAccess time.Time
}

// Match is one profile's answer to "are you signed in to this site?".
type Match struct {
	BrowserID string `json:"browserId"`
	Browser   string `json:"browser"`
	Profile   string `json:"profile"`
	// ProfileDir identifies the profile when the UI asks to connect it;
	// the backend only accepts directories a fresh scan finds.
	ProfileDir  string `json:"profileDir"`
	CookieCount int    `json:"cookieCount"`
	LoginKnown  bool   `json:"loginKnown"`
	LoggedIn    bool   `json:"loggedIn"`
	// LastAccess is when the browser last sent one of the site's
	// cookies (RFC 3339), empty if unknown.
	LastAccess string `json:"lastAccess"`
	// Connectable means the app can take the cookies from this profile
	// itself; otherwise Note says what to do instead.
	Connectable bool   `json:"connectable"`
	Note        string `json:"note"`
	Error       string `json:"error"`
}

// Scan checks every profile of every known browser for cookies of site.
// Profiles without any are left out unless reading them failed. tmpDir
// receives short-lived database copies.
func Scan(env Env, site string, tmpDir string) []Match {
	var matches []Match
	for _, profile := range Profiles(env) {
		match := Match{
			BrowserID:  profile.Browser.ID,
			Browser:    profile.Browser.Name,
			Profile:    profile.Name,
			ProfileDir: profile.Dir,
		}
		match.Connectable, match.Note = connectable(profile.Browser)

		names, err := readNames(profile, tmpDir)
		if err != nil {
			match.Error = err.Error()
			matches = append(matches, match)
			continue
		}

		var siteCookies []accounts.Cookie
		var lastAccess time.Time
		for _, c := range names {
			if !accounts.Belongs(c.host, site) {
				continue
			}
			// Names only: LoginState needs a non-empty value to count a
			// session, so a placeholder stands in for it.
			siteCookies = append(siteCookies, accounts.Cookie{Domain: c.host, Name: c.name, Value: "-"})
			if c.lastAccess.After(lastAccess) {
				lastAccess = c.lastAccess
			}
		}
		if len(siteCookies) == 0 {
			continue
		}
		match.CookieCount = len(siteCookies)
		match.LoginKnown, match.LoggedIn = accounts.LoginState(site, siteCookies)
		if !lastAccess.IsZero() {
			match.LastAccess = lastAccess.UTC().Format(time.RFC3339)
		}
		matches = append(matches, match)
	}

	// Signed-in and recently used profiles first: the likely choice.
	sort.SliceStable(matches, func(i, k int) bool {
		if matches[i].LoggedIn != matches[k].LoggedIn {
			return matches[i].LoggedIn
		}
		return matches[i].LastAccess > matches[k].LastAccess
	})
	return matches
}

// connectable says whether the app can take cookies from browser now.
func connectable(browser Browser) (bool, string) {
	if browser.Engine == EngineFirefox {
		return true, ""
	}
	if browser.YtDlpName == "" {
		return false, "Из этого браузера cookies не читаются: экспортируйте cookies.txt"
	}
	return false, "Подключение из этого браузера будет в следующей версии, пока используйте cookies.txt"
}

// FindProfile returns the profile of browserID in dir, as a fresh
// lookup finds it — never a path taken on trust from the UI.
func FindProfile(env Env, browserID string, dir string) (Profile, error) {
	for _, profile := range Profiles(env) {
		if profile.Browser.ID == browserID && profile.Dir == dir {
			return profile, nil
		}
	}
	return Profile{}, fmt.Errorf("профиль браузера не найден: %s", dir)
}

func readNames(profile Profile, tmpDir string) ([]cookieName, error) {
	var names []cookieName
	err := withDatabaseCopy(profile.CookiesPath, tmpDir, func(db *sql.DB) error {
		var err error
		if profile.Browser.Engine == EngineFirefox {
			names, err = firefoxNames(db)
		} else {
			names, err = chromiumNames(db)
		}
		return err
	})
	return names, err
}

// withDatabaseCopy copies a browser's SQLite database (with its WAL, which
// holds the most recent changes) into a private temporary directory and
// opens the copy: the browser may hold the original open or locked, and a
// copy can't be damaged by reading it.
func withDatabaseCopy(path string, tmpDir string, fn func(*sql.DB) error) error {
	if err := accounts.EnsurePrivateDir(tmpDir); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(tmpDir, "scan-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	copyPath := filepath.Join(dir, filepath.Base(path))
	if err := copyFile(path, copyPath); err != nil {
		return ErrUnreadable
	}
	if err := copyFile(path+"-wal", copyPath+"-wal"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrUnreadable
	}

	db, err := sql.Open("sqlite", copyPath+"?_pragma=busy_timeout(1000)")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := fn(db); err != nil {
		return fmt.Errorf("не удалось прочитать базу cookies: %w", err)
	}
	return nil
}

func copyFile(src string, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
