package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ytgrabber/internal/model"
	"ytgrabber/internal/platform"
)

// recordedEvents collects what the backend emits to the frontend.
type recordedEvents struct {
	mu     sync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	name string
	data []interface{}
}

func (r *recordedEvents) emit(name string, data ...interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedEvent{name: name, data: data})
}

func (r *recordedEvents) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, event := range r.events {
		if event.name == name {
			n++
		}
	}
	return n
}

// newTestApp builds an initialised App on a temporary data directory,
// without Wails.
func newTestApp(t *testing.T) (*App, *recordedEvents) {
	t.Helper()
	root := t.TempDir()
	app := New(platform.Paths{
		DataDir:             filepath.Join(root, "data"),
		DefaultDownloadsDir: filepath.Join(root, "downloads"),
		LegacyDownloadsDir:  filepath.Join(root, "legacy"),
	})
	events := &recordedEvents{}
	app.emit = events.emit
	if err := app.init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.shutdown(nil) })
	return app, events
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestInterruptedJobsAreRemovable covers jobs left over from a previous run:
// they must come back as retryable errors and be removable, even though no
// goroutine owns them.
func TestInterruptedJobsAreRemovable(t *testing.T) {
	app, events := newTestApp(t)
	app.db.SaveJob(&model.DownloadJob{ID: "stale", URL: "https://x", Status: model.StatusDownloading})
	app.shutdown(nil)

	app2 := New(app.paths)
	app2.emit = events.emit
	if err := app2.init(); err != nil {
		t.Fatal(err)
	}
	defer app2.shutdown(nil)

	jobs, err := app2.GetJobs()
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs = %+v, %v", jobs, err)
	}
	if jobs[0].Status != model.StatusError || jobs[0].Error != interruptedMessage {
		t.Errorf("stale job = %+v", jobs[0])
	}

	if err := app2.RemoveJob("stale"); err != nil {
		t.Fatal(err)
	}
	if jobs, _ := app2.GetJobs(); len(jobs) != 0 {
		t.Errorf("job not removed: %+v", jobs)
	}
	if stored, _ := app2.db.LoadJobs(); len(stored) != 0 {
		t.Errorf("job still in database: %+v", stored)
	}
	if events.count("job-removed") != 1 {
		t.Error("job-removed not emitted")
	}
}

func TestDownloadVideoValidates(t *testing.T) {
	app, _ := newTestApp(t)
	app.tools.YtDlp.Found = true
	app.tools.Ffmpeg.Found = true

	if _, err := app.DownloadVideo("--exec whoami", "best"); err == nil {
		t.Error("option-like input accepted")
	}
	if _, err := app.DownloadVideo("https://example.com/v", "8k"); err == nil {
		t.Error("unknown quality accepted")
	}

	app.tools.YtDlp.Found = false
	if _, err := app.DownloadVideo("https://example.com/v", "best"); err == nil {
		t.Error("download started without yt-dlp")
	}
}

// TestLateUpdateDoesNotResurrectJob simulates the download goroutine
// reporting progress right after the user removed the job.
func TestLateUpdateDoesNotResurrectJob(t *testing.T) {
	app, _ := newTestApp(t)
	j := &job{DownloadJob: model.DownloadJob{ID: "j", URL: "https://x", Status: model.StatusError, Tracks: []model.DownloadTrack{}}}
	app.jobs[j.ID] = j
	app.db.SaveJob(&j.DownloadJob)

	if err := app.RemoveJob("j"); err != nil {
		t.Fatal(err)
	}
	app.setStatus(j, model.StatusProcessing)

	if stored, _ := app.db.LoadJobs(); len(stored) != 0 {
		t.Errorf("removed job written back: %+v", stored)
	}
}

func TestRetryOnlyFailedJobs(t *testing.T) {
	app, _ := newTestApp(t)
	app.tools.YtDlp.Found = true
	app.tools.Ffmpeg.Found = true
	app.settings.MaxConcurrentDownloads = 0 // keep the job queued

	app.jobs["ok"] = &job{DownloadJob: model.DownloadJob{ID: "ok", Status: model.StatusDownloading}}
	app.jobs["bad"] = &job{DownloadJob: model.DownloadJob{ID: "bad", Status: model.StatusError, Error: "boom", Tracks: []model.DownloadTrack{{Filename: "x"}}}}

	if err := app.RetryJob("ok"); err == nil {
		t.Error("retrying an active job must fail")
	}
	if err := app.RetryJob("bad"); err != nil {
		t.Fatal(err)
	}
	bad := app.jobs["bad"]
	if bad.Status != model.StatusPending || bad.Error != "" || len(bad.Tracks) != 0 || len(app.queue) != 1 {
		t.Errorf("retried job = %+v, queue %v", bad, app.queue)
	}
}

func TestMediaHandler(t *testing.T) {
	app, _ := newTestApp(t)
	dir := t.TempDir()
	media := filepath.Join(dir, "clip.mp4")
	os.WriteFile(media, []byte("0123456789"), 0o644)
	app.db.SaveVideo(&model.VideoRecord{ID: "v1", Title: "Clip", MediaKind: model.MediaVideo, FilePath: media, DownloadedAt: "1"})

	handler := mediaHandler{app: app}

	req := httptest.NewRequest(http.MethodGet, "/media/v1/file.mp4", nil)
	req.Header.Set("Range", "bytes=2-4")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "234" {
		t.Errorf("range request: %d %q", rec.Code, rec.Body.String())
	}

	for _, path := range []string{
		"/media/unknown/file.mp4",
		"/media/v1/thumbnail.jpg", // no thumbnail
		"/media/../../etc/passwd",
		"/videos/clip.mp4",
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, rec.Code)
		}
	}
}

func TestDeleteVideoKeepsSharedThumbnail(t *testing.T) {
	app, events := newTestApp(t)
	dir := t.TempDir()
	video := filepath.Join(dir, "Clip [id].mp4")
	audio := filepath.Join(dir, "Clip [id].mp3")
	thumbnail := filepath.Join(dir, "Clip [id].jpg")
	for _, path := range []string{video, audio, thumbnail} {
		os.WriteFile(path, []byte("x"), 0o644)
	}
	app.db.SaveVideo(&model.VideoRecord{ID: "v", Title: "Clip", MediaKind: model.MediaVideo, FilePath: video, ThumbnailPath: thumbnail, DownloadedAt: "1"})
	app.db.SaveVideo(&model.VideoRecord{ID: "a", Title: "Clip", MediaKind: model.MediaAudio, FilePath: audio, ThumbnailPath: thumbnail, DownloadedAt: "2"})

	if err := app.DeleteVideo("v", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(video); !os.IsNotExist(err) {
		t.Error("video file not deleted")
	}
	if _, err := os.Stat(thumbnail); err != nil {
		t.Error("thumbnail shared with the audio entry was deleted")
	}

	if err := app.DeleteVideo("a", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(thumbnail); !os.IsNotExist(err) {
		t.Error("orphaned thumbnail not deleted")
	}
	if events.count("library-changed") != 2 {
		t.Errorf("library-changed emitted %d times", events.count("library-changed"))
	}
}

func TestGetLibraryMarksMissingFiles(t *testing.T) {
	app, _ := newTestApp(t)
	app.db.SaveVideo(&model.VideoRecord{ID: "gone", Title: "Gone", MediaKind: model.MediaVideo, FilePath: "/nonexistent/x.MP4", DownloadedAt: "1"})

	videos, err := app.GetLibrary()
	if err != nil || len(videos) != 1 {
		t.Fatalf("videos = %+v, %v", videos, err)
	}
	if !videos[0].FileMissing || videos[0].MediaURL != "/media/gone/file.mp4" || videos[0].ThumbnailURL != "" {
		t.Errorf("video = %+v", videos[0])
	}
}
