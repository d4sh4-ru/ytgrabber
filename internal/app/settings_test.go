package app

import (
	"path/filepath"
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
