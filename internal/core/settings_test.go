package core

import (
	"path/filepath"
	"strings"
	"testing"

	"ytgrabber/internal/storage"
)

func TestSettingsRoundTrip(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"), storage.MigrationEnv{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	settings, err := loadSettings(db, "/default")
	if err != nil {
		t.Fatal(err)
	}
	if settings.DownloadsDir != "/default" || settings.MaxConcurrentDownloads != defaultMaxConcurrentDownloads {
		t.Errorf("defaults = %+v", settings)
	}

	db.SetSetting(settingDownloadsDir, "/custom")
	db.SetSetting(settingMaxConcurrent, "99")
	db.SetSetting(settingYtDlpPath, "/bin/yt")
	db.SetSetting(settingYtDlpPath, "")

	settings, _ = loadSettings(db, "/default")
	if settings.DownloadsDir != "/custom" || settings.MaxConcurrentDownloads != maxConcurrentDownloadsLimit || settings.YtDlpPath != "" {
		t.Errorf("settings = %+v", settings)
	}
}

func TestSetProxy(t *testing.T) {
	app, _ := newTestApp(t)

	for _, bad := range []string{"localhost:1080", "ftp://host:21", "socks5://", "http://host:3128/path", "http://host?x=1"} {
		if _, err := app.SetProxy(bad); err == nil {
			t.Errorf("SetProxy(%q) accepted", bad)
		}
	}

	settings, err := app.SetProxy("  SOCKS5H://user:secret@proxy.lan:1080/ ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(settings.Proxy, "secret") || !strings.Contains(settings.Proxy, "proxy.lan:1080") {
		t.Errorf("returned proxy must hide the password: %q", settings.Proxy)
	}
	if got := app.GetSettings("someone").Proxy; strings.Contains(got, "secret") {
		t.Errorf("GetSettings leaks the proxy password: %q", got)
	}
	if got := app.proxy(); got != "socks5h://user:secret@proxy.lan:1080" {
		t.Errorf("proxy for yt-dlp = %q", got)
	}

	// Saved back from the settings screen with the password still masked.
	if _, err := app.SetProxy(strings.Replace(settings.Proxy, "proxy.lan", "proxy2.lan", 1)); err != nil {
		t.Fatal(err)
	}
	if got := app.proxy(); got != "socks5h://user:secret@proxy2.lan:1080" {
		t.Errorf("masked password not kept: %q", got)
	}
	app.SetProxy("socks5h://user:secret@proxy.lan:1080")

	reloaded, _ := loadSettings(app.db, "/d")
	if reloaded.Proxy != "socks5h://user:secret@proxy.lan:1080" {
		t.Errorf("proxy not persisted: %q", reloaded.Proxy)
	}

	if settings, err := app.SetProxy(""); err != nil || settings.Proxy != "" || app.proxy() != "" {
		t.Errorf("clearing proxy: %+v, %v", settings, err)
	}
}
