//go:build !windows

package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"ytgrabber/internal/platform"
)

// fakeBuild is a yt-dlp that reports version and names its downloads
// after title, so a test can tell which build did a job. With gate set it
// waits for that file before finishing, after creating started.
func fakeBuild(version, title, started, gate string) string {
	wait := ""
	if gate != "" {
		wait = fmt.Sprintf("touch %q\nwhile [ ! -f %q ]; do sleep 0.05; done\n", started, gate)
	}
	return `#!/bin/sh
[ "$1" = "--version" ] && { echo ` + version + `; exit 0; }
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift ;;
    --) shift; url="$1" ;;
  esac
  shift
done
base="$(dirname "$out")/` + title + ` [${url##*=}]"
echo "YTG_TITLE ` + title + `"
echo "YTG_FILENAME $base.mp4"
` + wait + `echo "[download] 100% of 1MiB"
echo "video" > "$base.mp4"
echo "YTG_FILEPATH $base.mp4"
`
}

// fakeGitHub serves a yt-dlp release the way github.com does: a redirect
// from releases/latest to the tag, the build and SHA2-256SUMS.
type fakeGitHub struct {
	server  *httptest.Server
	tag     string
	build   []byte
	fetches atomic.Int32
}

func newFakeGitHub(t *testing.T, tag string, build string) *fakeGitHub {
	t.Helper()
	gh := &fakeGitHub{tag: tag, build: []byte(build)}
	assets := []string{"yt-dlp_macos", "yt-dlp_linux", "yt-dlp_linux_aarch64"}
	gh.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const repo = "/yt-dlp/yt-dlp/releases"
		switch name, _ := strings.CutPrefix(r.URL.Path, repo+"/latest/download/"); {
		case r.URL.Path == repo+"/latest":
			w.Header().Set("Location", "https://github.com"+repo+"/tag/"+gh.tag)
			w.WriteHeader(http.StatusFound)
		case name == "SHA2-256SUMS":
			sum := sha256.Sum256(gh.build)
			for _, asset := range assets {
				fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
			}
		case slices.Contains(assets, name):
			gh.fetches.Add(1)
			w.Write(gh.build)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(gh.server.Close)
	return gh
}

// client sends every request, whatever its host, to the fake server.
func (gh *fakeGitHub) client() *http.Client {
	target, _ := url.Parse(gh.server.URL)
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return http.DefaultTransport.RoundTrip(r)
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// newAppWithManagedYtDlp puts build into the app's tools folder, where
// InstallTools puts yt-dlp, and a fake ffmpeg on a custom path.
func newAppWithManagedYtDlp(t *testing.T, build string, gh *fakeGitHub) (*App, *recordedEvents) {
	t.Helper()
	app, events := newTestApp(t)
	toolsDir := app.paths.ToolsDir()
	os.MkdirAll(toolsDir, 0o755)
	os.WriteFile(filepath.Join(toolsDir, platform.ExecutableName("yt-dlp")), []byte(build), 0o755)
	ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	os.WriteFile(ffmpeg, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	app.settings.FfmpegPath = ffmpeg
	app.installer.Client = gh.client()
	app.tools = app.resolveTools(app.settings)
	if !app.tools.YtDlp.Managed {
		t.Fatalf("yt-dlp is not the managed copy: %+v", app.tools.YtDlp)
	}
	return app, events
}

func libraryTitles(t *testing.T, app *App) map[string]string {
	t.Helper()
	videos, err := app.GetLibrary(LocalUser)
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]string{}
	for _, video := range videos {
		titles[video.SourceURL] = video.Title
	}
	return titles
}

// TestYtDlpUpdateDuringDownload: the update replaces yt-dlp while a
// download runs on it; that download finishes with the old build and the
// next one runs the new build, without restarting anything.
func TestYtDlpUpdateDuringDownload(t *testing.T) {
	dir := t.TempDir()
	started, gate := filepath.Join(dir, "started"), filepath.Join(dir, "gate")
	gh := newFakeGitHub(t, "2026.02.02", fakeBuild("2026.02.02", "New build", "", ""))
	app, events := newAppWithManagedYtDlp(t, fakeBuild("2026.01.01", "Old build", started, gate), gh)

	first := "https://example.com/watch?v=one"
	if _, err := app.DownloadVideo(LocalUser, first, "best", false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first download to start", func() bool {
		_, err := os.Stat(started)
		return err == nil
	})

	version, updated, err := app.UpdateYtDlpIfOutdated(context.Background())
	if err != nil || !updated || version != "2026.02.02" {
		t.Fatalf("update = %q, %v, %v", version, updated, err)
	}

	second := "https://example.com/watch?v=two"
	if _, err := app.DownloadVideo(LocalUser, second, "best", false); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(gate, nil, 0o644)
	waitFor(t, "both downloads", func() bool { return len(libraryTitles(t, app)) == 2 })

	titles := libraryTitles(t, app)
	if titles[first] != "Old build" || titles[second] != "New build" {
		t.Errorf("titles = %v", titles)
	}
	if events.count("tool-install-progress") == 0 {
		t.Error("the update reported no progress")
	}
}

func TestYtDlpUpdateSkipsCurrentVersion(t *testing.T) {
	gh := newFakeGitHub(t, "2026.01.01", fakeBuild("2026.01.01", "New build", "", ""))
	app, _ := newAppWithManagedYtDlp(t, fakeBuild("2026.01.01", "Old build", "", ""), gh)

	version, updated, err := app.UpdateYtDlpIfOutdated(context.Background())
	if err != nil || updated || version != "2026.01.01" || gh.fetches.Load() != 0 {
		t.Errorf("update = %q, %v, %v; %d downloads", version, updated, err, gh.fetches.Load())
	}
}

// A yt-dlp the user installed (PATH or a custom path) is theirs to update.
func TestYtDlpUpdateLeavesUnmanagedCopy(t *testing.T) {
	gh := newFakeGitHub(t, "2026.02.02", fakeBuild("2026.02.02", "New build", "", ""))
	app, _ := newAppWithManagedYtDlp(t, fakeBuild("2026.01.01", "Old build", "", ""), gh)
	custom := filepath.Join(t.TempDir(), "yt-dlp")
	os.WriteFile(custom, []byte(fakeBuild("2026.01.01", "Mine", "", "")), 0o755)
	app.settings.YtDlpPath = custom

	if _, updated, err := app.UpdateYtDlpIfOutdated(context.Background()); err != nil || updated || gh.fetches.Load() != 0 {
		t.Errorf("custom yt-dlp updated: %v, %v", updated, err)
	}
}
