//go:build !windows

package app

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytgrabber/internal/browsers"
	"ytgrabber/internal/model"
)

// fakeYtDlpExtract plays yt-dlp loading a Chromium profile's cookies
// (--cookies-from-browser) and writing the jar on exit. The profile
// directory's name picks the outcome: "Locked" can't be copied, "NoKey"
// can't be decrypted, anything else yields the site's cookie plus
// another site's. The spec it got is recorded in <record>/spec.
const fakeYtDlpExtract = `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in
    --cookies-from-browser) spec="$2"; shift ;;
    --cookies) jar="$2"; shift ;;
  esac
  shift
done
printf '%%s' "$spec" > "%[1]s/spec"
echo "ERROR: [generic] 'notaurl' is not a valid URL" >&2
case "$spec" in
  */Locked) echo "ERROR: Could not copy Chrome cookie database. See  https://github.com/yt-dlp/yt-dlp/issues/7271  for more info" >&2; exit 1 ;;
  */NoKey)
    echo "Extracted 1 cookies from chrome (2 could not be decrypted)"
    printf '# Netscape HTTP Cookie File\n.google.com\tTRUE\t/\tTRUE\t2000000000\tSID\tg\n' > "$jar"
    exit 1 ;;
esac
echo "Extracted 2 cookies from chrome"
printf '# Netscape HTTP Cookie File\n.instagram.com\tTRUE\t/\tTRUE\t2000000000\tsessionid\tchrome-secret\n.google.com\tTRUE\t/\tTRUE\t2000000000\tSID\tg\n' > "$jar"
exit 1
`

// withChromeProfiles gives the app a fake home with Chrome profiles
// "Default", "Locked" and "NoKey", each with an Instagram session cookie
// name, and a fake yt-dlp; it returns the Chrome data directory and the
// directory the fake yt-dlp records into.
func withChromeProfiles(t *testing.T, app *App) (string, string) {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, ".config", "google-chrome")
	os.MkdirAll(root, 0o755)
	os.WriteFile(filepath.Join(root, "Local State"), []byte(
		`{"profile":{"info_cache":{"Default":{"name":"Личный"},"Locked":{"name":"Locked"},"NoKey":{"name":"NoKey"}}}}`), 0o644)
	for _, profile := range []string{"Default", "Locked", "NoKey"} {
		path := filepath.Join(root, profile, "Network", "Cookies")
		os.MkdirAll(filepath.Dir(path), 0o755)
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		db.Exec(`CREATE TABLE cookies (host_key TEXT, name TEXT, encrypted_value BLOB, last_access_utc INTEGER)`)
		db.Exec(`INSERT INTO cookies VALUES ('.instagram.com', 'sessionid', x'00', 0)`)
		db.Close()
	}
	app.browserEnv = browsers.Env{GOOS: "linux", Home: home}

	record := t.TempDir()
	binDir := t.TempDir()
	ytDlp := filepath.Join(binDir, "yt-dlp")
	ffmpeg := filepath.Join(binDir, "ffmpeg")
	os.WriteFile(ytDlp, []byte(fmt.Sprintf(fakeYtDlpExtract, record)), 0o755)
	os.WriteFile(ffmpeg, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	app.settings.YtDlpPath = ytDlp
	app.settings.FfmpegPath = ffmpeg
	app.tools = app.resolveTools(app.settings)
	return root, record
}

func TestConnectChromeProfile(t *testing.T) {
	app, _ := newTestApp(t)
	root, record := withChromeProfiles(t, app)
	profileDir := filepath.Join(root, "Default")

	matches, _ := app.ScanBrowsers("instagram.com")
	if len(matches) != 3 || !matches[0].Connectable {
		t.Fatalf("matches = %+v", matches)
	}

	account, err := app.ConnectBrowserProfile("instagram.com", "chrome", profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if account.Source != model.AccountFromBrowser || account.BrowserName != "Chrome" || account.Profile != "Личный" || !account.LoggedIn {
		t.Errorf("account = %+v", account)
	}
	if spec, _ := os.ReadFile(filepath.Join(record, "spec")); string(spec) != "chrome:"+profileDir {
		t.Errorf("--cookies-from-browser %q", spec)
	}
	cookies, err := app.accounts.Load(account.ID, "instagram.com")
	if err != nil || len(cookies) != 1 || cookies[0].Value != "chrome-secret" {
		t.Errorf("snapshot = %+v, %v", cookies, err)
	}
	// The jar with every site's cookies must not outlive the extraction.
	if entries, _ := os.ReadDir(app.paths.RunDir()); len(entries) != 0 {
		t.Errorf("left behind in run/: %v", entries)
	}

	if _, err := app.RefreshAccount(account.ID); err != nil {
		t.Errorf("refresh: %v", err)
	}
}

func TestConnectChromeProfileErrors(t *testing.T) {
	app, _ := newTestApp(t)
	root, _ := withChromeProfiles(t, app)

	_, err := app.ConnectBrowserProfile("instagram.com", "chrome", filepath.Join(root, "Locked"))
	if err == nil || !strings.Contains(err.Error(), "закройте браузер") {
		t.Errorf("locked database error = %v", err)
	}
	_, err = app.ConnectBrowserProfile("instagram.com", "chrome", filepath.Join(root, "NoKey"))
	if err == nil || !strings.Contains(err.Error(), "не смог расшифровать") {
		t.Errorf("decryption error = %v", err)
	}
	if list, _ := app.GetAccounts(); len(list) != 0 {
		t.Errorf("failed extractions created accounts: %+v", list)
	}
	if entries, _ := os.ReadDir(app.paths.RunDir()); len(entries) != 0 {
		t.Errorf("left behind in run/: %v", entries)
	}
}

// TestConnectRefusesAppBoundProfiles: Chrome 127+ on Windows can't be
// decrypted by yt-dlp, so the app doesn't even try.
func TestConnectRefusesAppBoundProfiles(t *testing.T) {
	app, _ := newTestApp(t)
	root, record := withChromeProfiles(t, app)
	local := filepath.Join(app.browserEnv.Home, "AppData", "Local")
	winRoot := filepath.Join(local, "Google", "Chrome", "User Data")
	os.MkdirAll(filepath.Dir(winRoot), 0o755)
	if err := os.Rename(root, winRoot); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(winRoot, "Last Version"), []byte("131.0.6778.86"), 0o644)
	app.browserEnv = browsers.Env{GOOS: "windows", Home: app.browserEnv.Home, Vars: map[string]string{"LOCALAPPDATA": local}}

	_, err := app.ConnectBrowserProfile("instagram.com", "chrome", filepath.Join(winRoot, "Default"))
	if err == nil || !strings.Contains(err.Error(), "Firefox") {
		t.Errorf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(record, "spec")); err == nil {
		t.Error("yt-dlp was run anyway")
	}
}
