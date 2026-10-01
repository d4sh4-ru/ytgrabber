package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"ytgrabber/internal/model"
	"ytgrabber/internal/platform"
)

// resetFixture is an initialised app with one library entry, a partial
// download of an unfinished job, a foreign file in the downloads folder
// and a fake WebView storage folder.
type resetFixture struct {
	app        *App
	media      string
	thumbnail  string
	partial    string
	foreign    string
	webviewDir string
	helperArgs []string
	quitCalled chan struct{}
}

func newResetFixture(t *testing.T) *resetFixture {
	t.Helper()
	root := t.TempDir()
	f := &resetFixture{quitCalled: make(chan struct{}, 1)}
	f.webviewDir = filepath.Join(root, "webkit", "io.github.d4sh4-ru.ytgrabber")
	os.MkdirAll(f.webviewDir, 0o755)
	os.WriteFile(filepath.Join(f.webviewDir, "localstorage.db"), []byte("x"), 0o644)

	f.app = New(platform.Paths{
		DataDir:             filepath.Join(root, "data"),
		DefaultDownloadsDir: filepath.Join(root, "downloads"),
		LegacyDownloadsDir:  filepath.Join(root, "legacy"),
		WebviewDirs:         []string{f.webviewDir},
	})
	f.app.startHelper = func(paths []string) error {
		f.helperArgs = paths
		return nil
	}
	f.app.quit = func() { f.quitCalled <- struct{}{} }
	if err := f.app.init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.app.shutdown(nil) })

	downloads := f.app.settings.DownloadsDir
	os.MkdirAll(downloads, 0o755)
	f.media = filepath.Join(downloads, "Clip [a].mp4")
	f.thumbnail = filepath.Join(downloads, "Clip [a].jpg")
	f.partial = filepath.Join(downloads, "Other [b].f1.mp4.part")
	for _, path := range []string{f.media, f.thumbnail, f.partial} {
		os.WriteFile(path, []byte("12345"), 0o644)
	}
	f.app.db.SaveVideo(&model.VideoRecord{ID: "v", Title: "Clip", MediaKind: model.MediaVideo, FilePath: f.media, ThumbnailPath: f.thumbnail, DownloadedAt: "1"})
	f.app.jobs["j"] = &job{DownloadJob: model.DownloadJob{
		ID: "j", Status: model.StatusError,
		Tracks: []model.DownloadTrack{{Filename: filepath.Join(downloads, "Other [b].f1.mp4")}},
	}}
	return f
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestResetAllDataWithMedia(t *testing.T) {
	f := newResetFixture(t)

	info := f.app.GetStorageInfo()
	if info.MediaFiles != 2 || info.MediaBytes != 10 || info.DataBytes == 0 {
		t.Errorf("storage info = %+v", info)
	}

	if err := f.app.ResetAllData(true); err != nil {
		t.Fatal(err)
	}
	<-f.quitCalled

	for _, path := range []string{f.media, f.thumbnail, f.partial, f.app.paths.DataDir, f.webviewDir} {
		if exists(path) {
			t.Errorf("%s not deleted", path)
		}
	}
	if exists(f.app.settings.DownloadsDir) {
		t.Error("emptied downloads folder not removed")
	}
	if !slices.Contains(f.helperArgs, f.app.paths.DataDir) || !slices.Contains(f.helperArgs, f.webviewDir) {
		t.Errorf("cleanup helper got %q", f.helperArgs)
	}
	if err := f.app.ensureReady(); err == nil {
		t.Error("app still usable after reset")
	}
}

func TestResetAllDataKeepsMediaAndForeignFiles(t *testing.T) {
	f := newResetFixture(t)
	foreign := filepath.Join(f.app.settings.DownloadsDir, "not-ours.txt")
	os.WriteFile(foreign, []byte("x"), 0o644)

	if err := f.app.ResetAllData(false); err != nil {
		t.Fatal(err)
	}
	<-f.quitCalled

	for _, path := range []string{f.media, f.thumbnail, foreign} {
		if !exists(path) {
			t.Errorf("%s deleted", path)
		}
	}
	if exists(f.partial) {
		t.Error("partial download left behind")
	}
	if exists(f.app.paths.DataDir) {
		t.Error("data dir not deleted")
	}
}

// TestResetAllDataWithoutDatabase covers a broken install: the database
// failed to open, and resetting is how the user recovers.
func TestResetAllDataWithoutDatabase(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	os.MkdirAll(dataDir, 0o755)
	os.WriteFile(filepath.Join(dataDir, "ytgrabber.db"), []byte("corrupt"), 0o644)

	app := New(platform.Paths{DataDir: dataDir})
	app.startHelper = func([]string) error { return nil }
	app.startupErr = "broken"

	if err := app.ResetAllData(true); err != nil {
		t.Fatal(err)
	}
	if exists(dataDir) {
		t.Error("data dir not deleted")
	}
}

func TestCleanupHelperDeletesAfterStdinCloses(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "leftover")
	os.MkdirAll(filepath.Join(dir, "nested"), 0o755)

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin := os.Stdin
	os.Stdin = reader
	defer func() { os.Stdin = stdin }()

	done := make(chan struct{})
	go func() {
		RunCleanupHelper([]string{dir})
		close(done)
	}()

	time.Sleep(2 * cleanupRetryDelay)
	if !exists(dir) {
		t.Fatal("deleted before the app exited")
	}
	writer.Close() // the app exits
	<-done
	if exists(dir) {
		t.Error("not deleted after exit")
	}
}
