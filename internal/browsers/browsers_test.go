package browsers

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
)

var now = time.Unix(1_800_000_000, 0)

func createDB(t *testing.T, path string, statements ...string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

const mozCookiesSchema = `CREATE TABLE moz_cookies (id INTEGER PRIMARY KEY, originAttributes TEXT NOT NULL DEFAULT '',
	name TEXT, value TEXT, host TEXT, path TEXT, expiry INTEGER, lastAccessed INTEGER, creationTime INTEGER,
	isSecure INTEGER, isHttpOnly INTEGER)`

const chromiumCookiesSchema = `CREATE TABLE cookies (host_key TEXT, name TEXT, encrypted_value BLOB, last_access_utc INTEGER)`

// fakeHome lays out a Linux home with a Firefox (two profiles, one
// signed in to Instagram), a Chrome (two profiles) and an Opera with an
// old cookie schema in its root directory.
func fakeHome(t *testing.T) Env {
	t.Helper()
	home := t.TempDir()

	ff := filepath.Join(home, ".mozilla", "firefox")
	os.MkdirAll(ff, 0o755)
	absolute := filepath.Join(home, "elsewhere", "work")
	os.WriteFile(filepath.Join(ff, "profiles.ini"), []byte(
		"[Install4F96D1932A9F858E]\nDefault=abc.default-release\n\n"+
			"[Profile0]\nName=default-release\nIsRelative=1\nPath=abc.default-release\n\n"+
			"[Profile1]\nName=work\nIsRelative=0\nPath="+absolute+"\n\n"+
			"[Profile2]\nName=never-started\nIsRelative=1\nPath=missing\n\n"+
			"[General]\nStartWithLastProfile=1\n"), 0o644)

	accessed := now.Add(-48 * time.Hour).UnixMicro()
	createDB(t, filepath.Join(ff, "abc.default-release", "cookies.sqlite"), mozCookiesSchema,
		`INSERT INTO moz_cookies (originAttributes, name, value, host, path, expiry, lastAccessed, isSecure, isHttpOnly) VALUES
		('', 'sessionid', 'secret', '.instagram.com', '/', 1900000000000, `+itoa(accessed)+`, 1, 1),
		('', 'csrftoken', 'c', '.instagram.com', '/', 1900000000, 1, 1, 0),
		('^userContextId=2', 'sessionid', 'container', '.instagram.com', '/', 1900000000, 1, 1, 1),
		('^partitionKey=%28https%2Cexample.com%29', 'tracker', 't', '.instagram.com', '/', 1900000000, 1, 1, 0),
		('', 'other', 'o', '.example.com', '/', 1900000000, 1, 0, 0)`)
	createDB(t, filepath.Join(absolute, "cookies.sqlite"), mozCookiesSchema,
		`INSERT INTO moz_cookies (name, value, host, path, expiry, lastAccessed, isSecure, isHttpOnly) VALUES
		('csrftoken', 'x', '.instagram.com', '/', 1900000000, 1, 1, 0)`)

	chrome := filepath.Join(home, ".config", "google-chrome")
	os.MkdirAll(chrome, 0o755)
	os.WriteFile(filepath.Join(chrome, "Local State"), []byte(
		`{"profile":{"info_cache":{"Default":{"name":"Личный"},"Profile 2":{"name":"Работа"}}}}`), 0o644)
	chromeAccess := (now.Add(-time.Hour).UnixMicro()) + chromiumEpochOffset
	createDB(t, filepath.Join(chrome, "Profile 2", "Network", "Cookies"), chromiumCookiesSchema,
		`INSERT INTO cookies VALUES ('.instagram.com', 'sessionid', x'00', `+itoa(chromeAccess)+`)`)
	createDB(t, filepath.Join(chrome, "Default", "Cookies"), chromiumCookiesSchema,
		`INSERT INTO cookies VALUES ('.youtube.com', 'SAPISID', x'00', 0)`)

	createDB(t, filepath.Join(home, ".config", "opera", "Cookies"),
		`CREATE TABLE cookies (host_key TEXT, name TEXT)`,
		`INSERT INTO cookies VALUES ('www.instagram.com', 'mid')`)

	return Env{GOOS: "linux", Home: home}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func TestProfiles(t *testing.T) {
	env := fakeHome(t)
	var got []string
	for _, profile := range Profiles(env) {
		got = append(got, profile.Browser.ID+"/"+profile.Name)
	}
	want := []string{"firefox/default-release", "firefox/work", "chrome/Личный", "chrome/Работа", "opera/Default"}
	if !slices.Equal(got, want) {
		t.Errorf("profiles = %q, want %q", got, want)
	}
}

func TestScan(t *testing.T) {
	env := fakeHome(t)
	matches := Scan(env, "instagram.com", filepath.Join(t.TempDir(), "run"))

	var got []string
	for _, m := range matches {
		got = append(got, m.BrowserID+"/"+m.Profile)
	}
	// Signed in first (most recent first), then the rest; Chrome's
	// "Личный" has no Instagram cookies and is left out.
	want := []string{"chrome/Работа", "firefox/default-release", "firefox/work", "opera/Default"}
	if !slices.Equal(got, want) {
		t.Fatalf("matches = %q, want %q", got, want)
	}

	ff := matches[1]
	if !ff.LoggedIn || !ff.LoginKnown || ff.CookieCount != 2 || !ff.Connectable || ff.Error != "" {
		t.Errorf("firefox match = %+v", ff)
	}
	if ff.LastAccess != now.Add(-48*time.Hour).UTC().Format(time.RFC3339) {
		t.Errorf("last access = %q", ff.LastAccess)
	}
	if chrome := matches[0]; !chrome.LoggedIn || chrome.Connectable || chrome.Note == "" {
		t.Errorf("chrome match = %+v", chrome)
	}
	if work := matches[2]; work.LoggedIn || work.CookieCount != 1 {
		t.Errorf("signed-out profile = %+v", work)
	}
	if opera := matches[3]; opera.Error != "" || opera.CookieCount != 1 || opera.LastAccess != "" {
		t.Errorf("old-schema profile = %+v", opera)
	}
}

func TestScanReportsUnreadableDatabase(t *testing.T) {
	env := fakeHome(t)
	path := filepath.Join(env.Home, ".config", "google-chrome", "Profile 2", "Network", "Cookies")
	os.WriteFile(path, []byte("not a database"), 0o644)

	for _, m := range Scan(env, "instagram.com", t.TempDir()) {
		if m.Profile == "Работа" {
			if m.Error == "" {
				t.Errorf("broken database reported no error: %+v", m)
			}
			return
		}
	}
	t.Error("profile with a broken database left out")
}

func TestReadFirefoxCookies(t *testing.T) {
	env := fakeHome(t)
	profile, err := FindProfile(env, "firefox", filepath.Join(env.Home, ".mozilla", "firefox", "abc.default-release"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(t.TempDir(), "run")
	cookies, err := ReadFirefoxCookies(profile, "instagram.com", tmp, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(cookies) != 2 {
		t.Fatalf("cookies = %+v", cookies)
	}
	session := cookies[0]
	if session.Name != "sessionid" || session.Value != "secret" || session.Expires != 1900000000 ||
		!session.Secure || !session.HTTPOnly || !session.IncludeSubdomains {
		t.Errorf("session cookie = %+v", session)
	}
	if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
		t.Errorf("database copies left behind: %v", entries)
	}

	chrome, _ := FindProfile(env, "chrome", filepath.Join(env.Home, ".config", "google-chrome", "Profile 2"))
	if _, err := ReadFirefoxCookies(chrome, "instagram.com", tmp, now); err == nil {
		t.Error("read a Chromium profile as Firefox")
	}
}

func TestFindProfileRejectsUnknownDirs(t *testing.T) {
	env := fakeHome(t)
	if _, err := FindProfile(env, "firefox", env.Home); err == nil {
		t.Error("accepted a directory that is not a profile")
	}
	if _, err := FindProfile(env, "chrome", filepath.Join(env.Home, ".mozilla", "firefox", "abc.default-release")); err == nil {
		t.Error("accepted a profile of another browser")
	}
}
