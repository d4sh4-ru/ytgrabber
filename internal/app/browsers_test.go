package app

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"ytgrabber/internal/browsers"
	"ytgrabber/internal/model"
)

// withFirefoxProfile points the app at a fake home with one Firefox
// profile signed in to Instagram and returns the profile directory.
func withFirefoxProfile(t *testing.T, app *App) string {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, ".mozilla", "firefox")
	profileDir := filepath.Join(root, "abc.default-release")
	os.MkdirAll(profileDir, 0o755)
	os.WriteFile(filepath.Join(root, "profiles.ini"),
		[]byte("[Profile0]\nName=default-release\nIsRelative=1\nPath=abc.default-release\n"), 0o644)

	setFirefoxCookies(t, profileDir, "s1")
	app.browserEnv = browsers.Env{GOOS: "linux", Home: home}
	return profileDir
}

func setFirefoxCookies(t *testing.T, profileDir string, session string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(profileDir, "cookies.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`DROP TABLE IF EXISTS moz_cookies`,
		`CREATE TABLE moz_cookies (id INTEGER PRIMARY KEY, originAttributes TEXT NOT NULL DEFAULT '', name TEXT, value TEXT,
			host TEXT, path TEXT, expiry INTEGER, lastAccessed INTEGER, isSecure INTEGER, isHttpOnly INTEGER)`,
		`INSERT INTO moz_cookies (name, value, host, path, expiry, lastAccessed, isSecure, isHttpOnly) VALUES
			('sessionid', '` + session + `', '.instagram.com', '/', 2000000000, 1, 1, 1),
			('SID', 'google', '.google.com', '/', 2000000000, 1, 1, 1)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConnectFirefoxProfile(t *testing.T) {
	app, _ := newTestApp(t)
	profileDir := withFirefoxProfile(t, app)

	matches, err := app.ScanBrowsers("https://www.instagram.com/p/abc/")
	if err != nil || len(matches) != 1 {
		t.Fatalf("matches = %+v, %v", matches, err)
	}
	if m := matches[0]; !m.LoggedIn || !m.Connectable || m.ProfileDir != profileDir {
		t.Errorf("match = %+v", m)
	}

	account, err := app.ConnectBrowserProfile("instagram.com", "firefox", profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if account.Source != model.AccountFromBrowser || account.BrowserName != "Firefox" || account.Profile != "default-release" ||
		!account.LoggedIn || account.CookieCount != 1 || account.Status != model.AccountOK {
		t.Errorf("account = %+v", account)
	}
	cookies, err := app.accounts.Load(account.ID, "instagram.com")
	if err != nil || len(cookies) != 1 || cookies[0].Value != "s1" {
		t.Errorf("snapshot = %+v, %v", cookies, err)
	}

	// Refresh re-reads the same profile.
	setFirefoxCookies(t, profileDir, "s2")
	if _, err := app.RefreshAccount(account.ID); err != nil {
		t.Fatal(err)
	}
	cookies, _ = app.accounts.Load(account.ID, "instagram.com")
	if len(cookies) != 1 || cookies[0].Value != "s2" {
		t.Errorf("refreshed snapshot = %+v", cookies)
	}
	if list, _ := app.GetAccounts(); len(list) != 1 || list[0].BrowserName != "Firefox" {
		t.Errorf("accounts = %+v", list)
	}
}

func TestConnectBrowserProfileRejectsUnknownProfiles(t *testing.T) {
	app, _ := newTestApp(t)
	withFirefoxProfile(t, app)

	if _, err := app.ConnectBrowserProfile("instagram.com", "firefox", t.TempDir()); err == nil {
		t.Error("connected a directory that is not a known profile")
	}
	if _, err := app.ConnectBrowserProfile("vk.com", "firefox", filepath.Join(app.browserEnv.Home, ".mozilla", "firefox", "abc.default-release")); err == nil {
		t.Error("connected a profile without the site's cookies")
	}
}

func TestRefreshFileAccountNeedsNewFile(t *testing.T) {
	app, _ := newTestApp(t)
	file := filepath.Join(t.TempDir(), "cookies.txt")
	os.WriteFile(file, []byte("# Netscape HTTP Cookie File\n.vk.com\tTRUE\t/\tTRUE\t2000000000\tremixsid\tv\n"), 0o644)
	account, err := app.importCookiesFromPath("vk.com", file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.RefreshAccount(account.ID); err == nil {
		t.Error("refreshed a file account without a file")
	}
}
