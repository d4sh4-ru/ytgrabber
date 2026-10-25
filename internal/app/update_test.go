package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ytgrabber/internal/model"
	"ytgrabber/internal/updater"
)

// withFakeRelease sets the app up as version 1.0.0 whose repository has
// a signed 2.0.0 release with a build for this platform, installed as a
// single replaceable executable; it returns that executable.
func withFakeRelease(t *testing.T, app *App) string {
	t.Helper()
	var kind updater.InstallKind
	var payload []byte
	switch runtime.GOOS {
	case "linux":
		kind = updater.InstallLinuxBinary
		var archive bytes.Buffer
		gz := gzip.NewWriter(&archive)
		tw := tar.NewWriter(gz)
		tw.WriteHeader(&tar.Header{Name: "pkg/ytgrabber", Mode: 0o755, Size: 3, Typeflag: tar.TypeReg})
		tw.Write([]byte("new"))
		tw.Close()
		gz.Close()
		payload = archive.Bytes()
	case "windows":
		kind = updater.InstallWindowsPortable
		payload = []byte("new")
	default:
		t.Skip("covered by the updater package tests")
	}

	version, _ := updater.ParseVersion("2.0.0")
	asset := updater.AssetName(version, runtime.GOOS, runtime.GOARCH, kind)
	if asset == "" {
		t.Skip("no release build for this architecture")
	}
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	sum := sha256.Sum256(payload)
	sums := []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), asset))
	files := map[string][]byte{
		asset:                payload,
		"SHA256SUMS.txt":     sums,
		"SHA256SUMS.txt.sig": []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(private, sums))),
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases" {
			var assets []map[string]any
			for name := range files {
				assets = append(assets, map[string]any{"name": name, "browser_download_url": server.URL + "/files/" + name})
			}
			json.NewEncoder(w).Encode([]map[string]any{
				{"tag_name": "v2.0.0", "body": "## What's Changed\n* новое", "html_url": "https://example/v2.0.0", "assets": assets},
				{"tag_name": "v1.0.0", "html_url": "https://example/v1.0.0"},
			})
			return
		}
		w.Write(files[strings.TrimPrefix(r.URL.Path, "/files/")])
	}))
	t.Cleanup(server.Close)

	target := filepath.Join(t.TempDir(), "ytgrabber")
	os.WriteFile(target, []byte("old"), 0o755)

	oldVersion, oldKey := Version, updater.PublicKey
	Version, updater.PublicKey = "1.0.0", base64.StdEncoding.EncodeToString(public)
	t.Cleanup(func() { Version, updater.PublicKey = oldVersion, oldKey })

	app.updater.HTTP = server.Client()
	app.updater.APIURL = server.URL + "/releases"
	app.detectInstall = func() updater.Install { return updater.Install{Kind: kind, Target: target} }
	return target
}

func TestCheckAndInstallUpdate(t *testing.T) {
	app, _ := newTestApp(t)
	target := withFakeRelease(t, app)
	quit := make(chan struct{}, 1)
	app.quit = func() { quit <- struct{}{} }

	info, err := app.CheckForUpdates()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.Version != "2.0.0" || !info.CanInstall || info.Notes != "## What's Changed\n* новое" || info.CurrentVersion != "1.0.0" {
		t.Fatalf("info = %+v", info)
	}

	if err := app.InstallUpdate(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != "new" {
		t.Errorf("executable = %q", data)
	}
	<-quit
	if app.update.relaunch == nil || app.update.relaunch.Path != target {
		t.Errorf("relaunch = %+v", app.update.relaunch)
	}
}

func TestInstallUpdateWaitsForDownloads(t *testing.T) {
	app, _ := newTestApp(t)
	target := withFakeRelease(t, app)
	app.CheckForUpdates()

	app.jobs["busy"] = &job{DownloadJob: model.DownloadJob{ID: "busy", Status: model.StatusDownloading}}
	if err := app.InstallUpdate(); err == nil || !strings.Contains(err.Error(), "загрузок") {
		t.Errorf("error = %v", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "old" {
		t.Error("updated during a download")
	}
}

func TestUpdateNotInstallableWithoutKey(t *testing.T) {
	app, _ := newTestApp(t)
	withFakeRelease(t, app)
	updater.PublicKey = ""

	info, err := app.CheckForUpdates()
	if err != nil || !info.Available || info.CanInstall || info.InstallNote == "" {
		t.Errorf("info = %+v, %v", info, err)
	}
	if err := app.InstallUpdate(); err == nil {
		t.Error("installed an update the build can't verify")
	}
}

func TestNoUpdateWhenCurrent(t *testing.T) {
	app, _ := newTestApp(t)
	withFakeRelease(t, app)
	Version = "2.0.0"

	info, err := app.CheckForUpdates()
	if err != nil || info.Available {
		t.Errorf("info = %+v, %v", info, err)
	}
	if err := app.InstallUpdate(); err == nil {
		t.Error("installed without an update")
	}
}

func TestUpdateSettings(t *testing.T) {
	app, _ := newTestApp(t)
	if s := app.GetSettings(); !s.AutoCheckUpdates || s.PrereleaseUpdates {
		t.Errorf("defaults = %+v", s)
	}
	if _, err := app.SetUpdateSettings(false, true); err != nil {
		t.Fatal(err)
	}
	settings, _ := loadSettings(app.db, "/d")
	if settings.AutoCheckUpdates || !settings.PrereleaseUpdates {
		t.Errorf("reloaded = %+v", settings)
	}
}
