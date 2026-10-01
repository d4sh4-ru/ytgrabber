package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultDownloadsDirFromXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	os.MkdirAll(filepath.Join(home, ".config"), 0o755)
	os.WriteFile(filepath.Join(home, ".config", "user-dirs.dirs"),
		[]byte("# comment\nXDG_VIDEOS_DIR=\"$HOME/Видео\"\n"), 0o644)

	if got := xdgUserDir(home, "XDG_VIDEOS_DIR"); got != filepath.Join(home, "Видео") {
		t.Errorf("xdgUserDir = %q", got)
	}
	if got := xdgUserDir(home, "XDG_MUSIC_DIR"); got != "" {
		t.Errorf("unset key = %q", got)
	}
}

func TestIsSafeToRemove(t *testing.T) {
	home := HomeDir()
	configDir, _ := os.UserConfigDir()
	for _, path := range []string{"", "relative/dir", "/", home, configDir} {
		if IsSafeToRemove(path) {
			t.Errorf("IsSafeToRemove(%q) = true", path)
		}
	}
	if !IsSafeToRemove(filepath.Join(configDir, "ytgrabber")) {
		t.Error("app data dir must be removable")
	}
}
