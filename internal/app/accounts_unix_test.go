//go:build !windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ytgrabber/internal/accounts"
	"ytgrabber/internal/model"
)

// fakeYtDlpWithCookies records the cookies file it was given (as
// <record>/<last path segment of the URL>.txt) and, like yt-dlp on exit,
// writes the jar back with one rotated cookie for the site and one
// foreign cookie that must never reach the snapshot. URLs containing
// "login" fail with YouTube's sign-in error.
const fakeYtDlpWithCookies = `#!/bin/sh
cookies=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift ;;
    --cookies) cookies="$2"; shift ;;
    --) shift; url="$1" ;;
  esac
  shift
done
tag=$(basename "$url")
case "$url" in
  *slow*) sleep 1 ;;
esac
if [ -n "$cookies" ]; then
  cp "$cookies" "%[1]s/$tag.txt"
  printf '.example.com\tTRUE\t/\tTRUE\t2000000000\trotated_%%s\tv\n' "$tag" >> "$cookies"
  printf '.google.com\tTRUE\t/\tTRUE\t2000000000\tSID\tstolen\n' >> "$cookies"
fi
case "$url" in
  *login*) echo "ERROR: [youtube] $tag: Sign in to confirm you're not a bot. Use --cookies-from-browser or --cookies for the authentication." >&2; exit 1 ;;
esac
dir=$(dirname "$out")
base="$dir/Clip $tag"
echo "YTG_TITLE Clip"
echo "[download] Destination: $base.mp4"
echo "[download] 100%% of 1MiB"
echo "video" > "$base.mp4"
echo "YTG_FILEPATH $base.mp4"
`

func newAppWithCookieTools(t *testing.T) (*App, string) {
	t.Helper()
	app, _ := newTestApp(t)

	binDir := t.TempDir()
	record := t.TempDir()
	ytDlp := filepath.Join(binDir, "yt-dlp")
	ffmpeg := filepath.Join(binDir, "ffmpeg")
	os.WriteFile(ytDlp, []byte(fmt.Sprintf(fakeYtDlpWithCookies, record)), 0o755)
	os.WriteFile(ffmpeg, []byte("#!/bin/sh\nexit 0\n"), 0o755)

	app.settings.YtDlpPath = ytDlp
	app.settings.FfmpegPath = ffmpeg
	app.tools = app.resolveTools(app.settings)
	app.tools.Ffprobe.Found = false
	return app, record
}

// importTestAccount connects example.com from a cookies.txt that also
// holds another site's cookie.
func importTestAccount(t *testing.T, app *App) *model.Account {
	t.Helper()
	file := filepath.Join(t.TempDir(), "cookies.txt")
	os.WriteFile(file, []byte("# Netscape HTTP Cookie File\n"+
		".example.com\tTRUE\t/\tTRUE\t2000000000\tsession\ts1\n"+
		".other.org\tTRUE\t/\tTRUE\t2000000000\tforeign\tf1\n"), 0o644)

	account, err := app.importCookiesFromPath("example.com", file)
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func snapshotNames(t *testing.T, app *App, account *model.Account) []string {
	t.Helper()
	cookies, err := app.accounts.Load(account.ID, account.Domain)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range cookies {
		names = append(names, c.Name)
	}
	slices.Sort(names)
	return names
}

func waitForJobs(t *testing.T, app *App, done func([]model.DownloadJob) bool) []model.DownloadJob {
	t.Helper()
	var jobs []model.DownloadJob
	waitFor(t, "jobs", func() bool {
		jobs, _ = app.GetJobs()
		return done(jobs)
	})
	return jobs
}

func TestImportCookiesFile(t *testing.T) {
	app, _ := newAppWithCookieTools(t)
	account := importTestAccount(t, app)

	if account.Domain != "example.com" || account.CookieCount != 1 || account.Status != model.AccountOK || account.Source != model.AccountFromFile {
		t.Errorf("account = %+v", account)
	}
	if got := snapshotNames(t, app, account); !slices.Equal(got, []string{"session"}) {
		t.Errorf("snapshot = %q", got)
	}

	// Importing again refreshes the same account.
	again := importTestAccount(t, app)
	list, _ := app.GetAccounts()
	if again.ID != account.ID || len(list) != 1 {
		t.Errorf("re-import created a second account: %+v", list)
	}

	empty := filepath.Join(t.TempDir(), "cookies.txt")
	os.WriteFile(empty, []byte("# Netscape HTTP Cookie File\n.other.org\tTRUE\t/\tTRUE\t2000000000\ta\tb\n"), 0o644)
	if _, err := app.importCookiesFromPath("vk.com", empty); err == nil {
		t.Error("imported a file without the site's cookies")
	}
}

// TestDownloadWithAccount: the job gets the site's cookies through a
// private temporary file, and what yt-dlp writes back is filtered and
// re-encrypted.
func TestDownloadWithAccount(t *testing.T) {
	app, record := newAppWithCookieTools(t)
	account := importTestAccount(t, app)

	if _, err := app.DownloadVideo("https://www.example.com/watch/one", "best", true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "library entry", func() bool {
		videos, _ := app.GetLibrary()
		return len(videos) == 1
	})

	passed, err := os.ReadFile(filepath.Join(record, "one.txt"))
	if err != nil {
		t.Fatalf("yt-dlp got no cookies: %v", err)
	}
	if !strings.Contains(string(passed), "session\ts1") || strings.Contains(string(passed), "foreign") {
		t.Errorf("cookies passed to yt-dlp:\n%s", passed)
	}

	if got := snapshotNames(t, app, account); !slices.Equal(got, []string{"rotated_one", "session"}) {
		t.Errorf("snapshot after write-back = %q", got)
	}
	updated, _ := app.db.GetAccount(account.ID)
	if updated.LastUsedAt == "" || updated.CookieCount != 2 {
		t.Errorf("account after download = %+v", updated)
	}

	if entries, _ := os.ReadDir(app.paths.RunDir()); len(entries) != 0 {
		t.Errorf("decrypted cookies left behind: %v", entries)
	}
}

func TestDownloadWithoutAccount(t *testing.T) {
	app, record := newAppWithCookieTools(t)
	importTestAccount(t, app)

	if _, err := app.DownloadVideo("https://example.com/watch/anon", "best", false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "library entry", func() bool {
		videos, _ := app.GetLibrary()
		return len(videos) == 1
	})
	if _, err := os.Stat(filepath.Join(record, "anon.txt")); err == nil {
		t.Error("cookies passed although the job opted out")
	}
}

// TestLoginRequiredExpiresAccount: a sign-in error is reported as such on
// the job and marks the account it ran with as expired.
func TestLoginRequiredExpiresAccount(t *testing.T) {
	app, _ := newAppWithCookieTools(t)
	account := importTestAccount(t, app)

	if _, err := app.DownloadVideo("https://example.com/login", "best", true); err != nil {
		t.Fatal(err)
	}
	jobs := waitForJobs(t, app, func(jobs []model.DownloadJob) bool {
		return len(jobs) == 1 && jobs[0].Status == model.StatusError
	})
	if job := jobs[0]; job.ErrorKind != model.ErrorLoginRequired || !job.AccountUsed || job.Site != "example.com" {
		t.Errorf("job = %+v", job)
	}
	if updated, _ := app.db.GetAccount(account.ID); updated.Status != model.AccountExpired {
		t.Errorf("account status = %s", updated.Status)
	}

	// After a fresh import the retry runs again with the account.
	importTestAccount(t, app)
	if updated, _ := app.db.GetAccount(account.ID); updated.Status != model.AccountOK {
		t.Errorf("re-imported account status = %s", updated.Status)
	}
	if err := app.RetryJob(jobs[0].ID, true); err != nil {
		t.Fatal(err)
	}
	jobs = waitForJobs(t, app, func(jobs []model.DownloadJob) bool {
		return len(jobs) == 1 && jobs[0].Status == model.StatusError
	})
	if jobs[0].ErrorKind != model.ErrorLoginRequired {
		t.Errorf("retried job = %+v", jobs[0])
	}
}

func TestLoginRequiredWithoutAccount(t *testing.T) {
	app, _ := newAppWithCookieTools(t)

	app.DownloadVideo("https://example.com/login", "best", true)
	jobs := waitForJobs(t, app, func(jobs []model.DownloadJob) bool {
		return len(jobs) == 1 && jobs[0].Status == model.StatusError
	})
	if job := jobs[0]; job.ErrorKind != model.ErrorLoginRequired || job.AccountUsed {
		t.Errorf("job = %+v", job)
	}

	// Connecting the account and retrying uses it.
	importTestAccount(t, app)
	app.RetryJob(jobs[0].ID, true)
	jobs = waitForJobs(t, app, func(jobs []model.DownloadJob) bool {
		return len(jobs) == 1 && jobs[0].Status == model.StatusError
	})
	if !jobs[0].AccountUsed {
		t.Errorf("retry after connecting ran without the account: %+v", jobs[0])
	}
}

// TestRetryAfterOptOut: a job added "without an account" still uses one
// when it is retried after the user signed in.
func TestRetryAfterOptOut(t *testing.T) {
	app, _ := newAppWithCookieTools(t)
	importTestAccount(t, app)

	id, _ := app.DownloadVideo("https://example.com/login", "best", false)
	jobs := waitForJobs(t, app, func(jobs []model.DownloadJob) bool {
		return len(jobs) == 1 && jobs[0].Status == model.StatusError
	})
	if jobs[0].AccountUsed || jobs[0].UseAccount {
		t.Fatalf("opted-out job = %+v", jobs[0])
	}

	app.RetryJob(id, true)
	jobs = waitForJobs(t, app, func(jobs []model.DownloadJob) bool {
		return len(jobs) == 1 && jobs[0].Status == model.StatusError
	})
	if !jobs[0].AccountUsed {
		t.Errorf("retried job = %+v", jobs[0])
	}
}

// TestParallelWriteBack: two downloads with the same account run at once;
// neither write-back may drop the other's refreshed cookie.
func TestParallelWriteBack(t *testing.T) {
	app, _ := newAppWithCookieTools(t)
	account := importTestAccount(t, app)

	app.DownloadVideo("https://example.com/slow-a", "best", true)
	app.DownloadVideo("https://example.com/slow-b", "best", true)
	waitFor(t, "both downloads", func() bool {
		videos, _ := app.GetLibrary()
		return len(videos) == 2
	})

	want := []string{"rotated_slow-a", "rotated_slow-b", "session"}
	if got := snapshotNames(t, app, account); !slices.Equal(got, want) {
		t.Errorf("snapshot = %q, want %q", got, want)
	}
}

func TestRemoveAccount(t *testing.T) {
	app, record := newAppWithCookieTools(t)
	account := importTestAccount(t, app)

	if err := app.RemoveAccount(account.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := app.GetAccounts(); len(list) != 0 {
		t.Errorf("accounts = %+v", list)
	}
	if _, err := os.Stat(filepath.Join(app.paths.AccountsDir(), account.ID+".enc")); !os.IsNotExist(err) {
		t.Errorf("snapshot not deleted: %v", err)
	}

	app.DownloadVideo("https://example.com/watch/after", "best", true)
	waitFor(t, "library entry", func() bool {
		videos, _ := app.GetLibrary()
		return len(videos) == 1
	})
	if _, err := os.Stat(filepath.Join(record, "after.txt")); err == nil {
		t.Error("cookies passed after the account was removed")
	}
}

func TestCheckAccountWithoutOnlineCheck(t *testing.T) {
	app, _ := newAppWithCookieTools(t)
	account := importTestAccount(t, app)

	// Simulate the session cookie having expired since the import.
	app.accounts.Save(account.ID, account.Domain, []accounts.Cookie{
		{Domain: ".example.com", Path: "/", Name: "session", Value: "s1", Expires: 1},
	})
	checked, err := app.CheckAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if checked.Status != model.AccountExpired || checked.CookieCount != 0 {
		t.Errorf("checked = %+v", checked)
	}
}

// TestCheckKeepsRefusedAccountExpired: for a site without an online check,
// valid-looking cookies don't overrule a login the site refused.
func TestCheckKeepsRefusedAccountExpired(t *testing.T) {
	app, _ := newAppWithCookieTools(t)
	account := importTestAccount(t, app)

	app.DownloadVideo("https://example.com/login", "best", true)
	waitForJobs(t, app, func(jobs []model.DownloadJob) bool {
		return len(jobs) == 1 && jobs[0].Status == model.StatusError
	})
	checked, err := app.CheckAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if checked.Status != model.AccountExpired {
		t.Errorf("status after check = %s", checked.Status)
	}
}

func TestRunDirCleanedOnStartup(t *testing.T) {
	app, _ := newTestApp(t)
	leftover := filepath.Join(app.paths.RunDir(), "crashed.cookies")
	accounts.WritePrivateFile(leftover, []byte("secret"))

	app.db.Close()
	app.db = nil
	if err := app.init(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Errorf("leftover cookies not removed: %v", err)
	}
}
