//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ytgrabber/internal/model"
	"ytgrabber/internal/storage"
)

// fakeYtDlp mimics the parts of yt-dlp's output the app relies on. The
// output file name is taken from the -o template, like the real thing.
const fakeYtDlp = `#!/bin/sh
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift ;;
    --) shift; url="$1" ;;
  esac
  shift
done
dir=$(dirname "$out")
case "$url" in
  *fail*) echo "ERROR: [generic] Unsupported URL: $url" >&2; exit 1 ;;
esac
base="$dir/Fake clip [abc]"
echo "YTG_TITLE Fake clip"
echo "YTG_FILENAME $base.mp4"
echo "[download] Destination: $base.f1.mp4"
echo "partial" > "$base.f1.mp4.part"
echo "[download]  50.0% of 1MiB"
case "$url" in
  *slow*) sleep 30 ;;
esac
echo "[download] 100% of 1MiB"
rm -f "$base.f1.mp4.part"
echo "[Merger] Merging formats into \"$base.mp4\""
echo "video" > "$base.mp4"
echo "thumb" > "$base.jpg"
echo "YTG_FILEPATH $base.mp4"
`

func newAppWithFakeTools(t *testing.T) (*App, *recordedEvents) {
	t.Helper()
	app, events := newTestApp(t)

	binDir := t.TempDir()
	ytDlp := filepath.Join(binDir, "yt-dlp")
	ffmpeg := filepath.Join(binDir, "ffmpeg")
	os.WriteFile(ytDlp, []byte(fakeYtDlp), 0o755)
	os.WriteFile(ffmpeg, []byte("#!/bin/sh\nexit 0\n"), 0o755)

	app.settings.YtDlpPath = ytDlp
	app.settings.FfmpegPath = ffmpeg
	app.tools = app.resolveTools(app.settings)
	app.tools.Ffprobe.Found = false // keep the test independent of a real ffprobe
	return app, events
}

func TestDownloadEndToEnd(t *testing.T) {
	app, events := newAppWithFakeTools(t)

	id, err := app.DownloadVideo("https://example.com/watch?v=abc", "best")
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "library entry", func() bool { return events.count("library-changed") > 0 })

	videos, err := app.GetLibrary()
	if err != nil || len(videos) != 1 {
		t.Fatalf("library = %+v, %v", videos, err)
	}
	video := videos[0]
	want := filepath.Join(app.settings.DownloadsDir, "Fake clip [abc].mp4")
	if video.ID != id || video.Title != "Fake clip" || video.FilePath != want || video.FileMissing {
		t.Errorf("video = %+v", video)
	}
	if video.ThumbnailURL == "" || video.SourceURL != "https://example.com/watch?v=abc" {
		t.Errorf("video = %+v", video)
	}
	if jobs, _ := app.GetJobs(); len(jobs) != 0 {
		t.Errorf("finished job still listed: %+v", jobs)
	}
}

func TestDownloadFailureShowsYtDlpError(t *testing.T) {
	app, _ := newAppWithFakeTools(t)

	id, err := app.DownloadVideo("https://example.com/fail", "best")
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "job error", func() bool {
		jobs, _ := app.GetJobs()
		return len(jobs) == 1 && jobs[0].Status == model.StatusError
	})
	jobs, _ := app.GetJobs()
	if jobs[0].ID != id || !strings.Contains(jobs[0].Error, "Unsupported URL") {
		t.Errorf("job = %+v", jobs[0])
	}
}

// TestRemoveRunningJob checks cancellation kills the whole process group
// promptly and removes the partial download.
func TestRemoveRunningJob(t *testing.T) {
	app, _ := newAppWithFakeTools(t)

	id, err := app.DownloadVideo("https://example.com/slow", "best")
	if err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(app.settings.DownloadsDir, "Fake clip [abc].f1.mp4.part")
	waitFor(t, "download to start", func() bool {
		_, err := os.Stat(partial)
		return err == nil
	})

	start := time.Now()
	if err := app.RemoveJob(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "worker to stop", func() bool {
		app.mu.RLock()
		defer app.mu.RUnlock()
		return app.running == 0
	})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("cancellation took %s", elapsed)
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Error("partial file left behind")
	}
	if jobs, _ := app.GetJobs(); len(jobs) != 0 {
		t.Errorf("jobs = %+v", jobs)
	}
}

func TestQueueRespectsConcurrencyLimit(t *testing.T) {
	app, _ := newAppWithFakeTools(t)
	app.settings.MaxConcurrentDownloads = 1

	first, _ := app.DownloadVideo("https://example.com/slow1", "best")
	second, _ := app.DownloadVideo("https://example.com/slow2", "best")

	waitFor(t, "first job to start", func() bool {
		app.mu.RLock()
		defer app.mu.RUnlock()
		return app.jobs[first].Status == model.StatusDownloading
	})
	app.mu.RLock()
	secondStatus := app.jobs[second].Status
	app.mu.RUnlock()
	if secondStatus != model.StatusPending {
		t.Errorf("second job status %s, want pending", secondStatus)
	}

	app.RemoveJob(first)
	waitFor(t, "second job to start", func() bool {
		app.mu.RLock()
		defer app.mu.RUnlock()
		job, ok := app.jobs[second]
		return ok && job.Status == model.StatusDownloading
	})
	app.RemoveJob(second)
}

// TestShutdownMarksRunningJobsInterrupted checks a job cancelled by app
// exit is persisted as a retryable error.
func TestShutdownMarksRunningJobsInterrupted(t *testing.T) {
	app, _ := newAppWithFakeTools(t)

	id, _ := app.DownloadVideo("https://example.com/slow", "best")
	waitFor(t, "download to start", func() bool {
		app.mu.RLock()
		defer app.mu.RUnlock()
		return len(app.jobs[id].Tracks) > 0
	})

	app.shutdown(nil)

	db, err := storage.Open(app.paths.DBPath(), storage.MigrationEnv{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	jobs, _ := db.LoadJobs()
	if len(jobs) != 1 || jobs[0].Status != model.StatusError || jobs[0].Error != interruptedMessage {
		t.Errorf("jobs after shutdown = %+v", jobs)
	}
}
