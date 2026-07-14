package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type DownloadStatus string

const (
	StatusPending     DownloadStatus = "pending"
	StatusDownloading DownloadStatus = "downloading"
	StatusProcessing  DownloadStatus = "processing"
	StatusDone        DownloadStatus = "done"
	StatusError       DownloadStatus = "error"
)

type TrackKind string

const (
	TrackVideo TrackKind = "video"
	TrackAudio TrackKind = "audio"
)

type DownloadTrack struct {
	Kind     TrackKind `json:"kind"`
	Progress float64   `json:"progress"`
	Filename string    `json:"filename"`
}

type DownloadJob struct {
	ID       string          `json:"id"`
	URL      string          `json:"url"`
	Title    string          `json:"title"`
	Quality  string          `json:"quality"`
	FilePath string          `json:"filePath"`
	Status   DownloadStatus  `json:"status"`
	Tracks   []DownloadTrack `json:"tracks"`
	Error    string          `json:"error,omitempty"`
}

// VideoRecord stores only the downloaded file's basename, not its full
// filesystem path. The frontend serves videos through Wails' asset server
// at "/videos/<filename>" (see main.go), so it never needs — and, inside
// the WKWebView sandbox, never could use — an absolute path.
type VideoRecord struct {
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	Filename        string  `json:"filename"`
	DurationSeconds float64 `json:"durationSeconds"`
	ThumbnailPath   string  `json:"thumbnailPath,omitempty"`
	DownloadedAt    string  `json:"downloadedAt"`
}

type App struct {
	ctx          context.Context
	jobs         map[string]*DownloadJob
	cancels      map[string]context.CancelFunc
	mu           sync.RWMutex
	db           *sqliteDB
	downloadsDir string
}

func NewApp() *App {
	return &App{
		jobs:    make(map[string]*DownloadJob),
		cancels: make(map[string]context.CancelFunc),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	dbPath, downloadsDir := resolveDataPaths()
	a.downloadsDir = downloadsDir

	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		log.Printf("ytgrabber: failed to create downloads dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Printf("ytgrabber: failed to create data dir: %v", err)
	}

	db, err := openDatabase(dbPath)
	if err != nil {
		log.Printf("ytgrabber: failed to open database: %v", err)
		return
	}
	a.db = db
}

func (a *App) shutdown(ctx context.Context) {
	if a.db != nil {
		a.db.Close()
	}
}

// resolveDataPaths decides where the SQLite database and downloaded videos
// live. isDevBuild is true exactly when compiled via `wails dev` (see
// env_dev.go/env_prod.go), which keeps everything inside the project
// directory for easy debugging/cleanup; a real `wails build` is treated as
// a production install and uses the platform's per-user data/media
// directories. It's resolved via a build tag rather than the runtime
// context because main() needs the downloads directory to configure the
// asset server before wails.Run (and thus OnStartup) ever runs.
func resolveDataPaths() (dbPath string, downloadsDir string) {
	if isDevBuild {
		return filepath.Join("data", "dev.db"), "downloads"
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}

	return filepath.Join(configDir, "ytgrabber", "ytgrabber.db"), filepath.Join(homeDir, "Movies", "ytgrabber")
}

var (
	trackDestRe     = regexp.MustCompile(`^\[download\] Destination:\s*(.+)$`)
	trackProgressRe = regexp.MustCompile(`^\[download\]\s+(\d+\.?\d*)%`)
	mergeDestRe     = regexp.MustCompile(`Merging formats into "(.+)"`)
	extractAudioRe  = regexp.MustCompile(`^\[ExtractAudio\] Destination:\s*(.+)$`)
	ytDlpErrorRe    = regexp.MustCompile(`^ERROR:\s*(.+)$`)
)

const maxErrorMessageLength = 300

// DownloadVideo запускает yt-dlp для переданного URL и шлёт события прогресса.
func (a *App) DownloadVideo(url string, quality string) string {
	id := uuid.NewString()

	job := &DownloadJob{
		ID:      id,
		URL:     url,
		Quality: quality,
		Status:  StatusPending,
		Tracks:  []DownloadTrack{},
	}

	a.mu.Lock()
	a.jobs[id] = job
	a.mu.Unlock()

	a.persistJob(job)

	go a.runDownload(job)

	return id
}

func (a *App) runDownload(job *DownloadJob) {
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancels[job.ID] = cancel
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.cancels, job.ID)
		a.mu.Unlock()
		cancel()
	}()

	a.updateStatus(job, StatusDownloading)

	cmd := exec.CommandContext(ctx, "yt-dlp", buildYtDlpArgs(job.URL, job.Quality, a.downloadsDir)...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		a.setError(job, err.Error())
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		a.setError(job, err.Error())
		return
	}

	if err := cmd.Start(); err != nil {
		a.setError(job, err.Error())
		return
	}

	var stderrMu sync.Mutex
	var stderrLines []string
	var stderrDone sync.WaitGroup
	stderrDone.Add(1)
	go func() {
		defer stderrDone.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			stderrMu.Lock()
			stderrLines = append(stderrLines, line)
			stderrMu.Unlock()
		}
	}()

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		a.handleOutputLine(job, scanner.Text())
	}

	stderrDone.Wait()

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			// The job was cancelled/removed on purpose (see RemoveJob) — the
			// removal path already deleted it, so don't resurrect it here.
			return
		}

		stderrMu.Lock()
		message := extractErrorMessage(stderrLines)
		stderrMu.Unlock()
		a.setError(job, message)
		return
	}

	a.finishJob(job)
}

// extractErrorMessage turns yt-dlp's raw stderr into a short, readable
// message: the last "ERROR:" line if yt-dlp printed one (that's its own
// summary of what went wrong), otherwise the last few non-empty lines as a
// fallback. Either way the result is capped so a stack trace can't flood
// the UI.
func extractErrorMessage(stderrLines []string) string {
	var lastError string
	for _, line := range stderrLines {
		if matches := ytDlpErrorRe.FindStringSubmatch(line); matches != nil {
			lastError = strings.TrimSpace(matches[1])
		}
	}
	if lastError != "" {
		return truncateMessage(lastError)
	}

	nonEmpty := make([]string, 0, len(stderrLines))
	for _, line := range stderrLines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			nonEmpty = append(nonEmpty, trimmed)
		}
	}
	if len(nonEmpty) == 0 {
		return "yt-dlp завершился с ошибкой"
	}

	tailStart := 0
	if len(nonEmpty) > 3 {
		tailStart = len(nonEmpty) - 3
	}
	return truncateMessage(strings.Join(nonEmpty[tailStart:], "\n"))
}

func truncateMessage(message string) string {
	runes := []rune(message)
	if len(runes) <= maxErrorMessageLength {
		return message
	}
	return string(runes[:maxErrorMessageLength]) + "…"
}

// buildYtDlpArgs maps the UI quality choice onto yt-dlp format-selector
// flags. Video qualities force an mp4 container via --merge-output-format
// so the player never has to deal with webm; the audio-only path already
// produces mp3 so no container flag is needed there. Video qualities also
// ask yt-dlp for a thumbnail converted to jpg (same webm/WKWebView reason
// as the video container) — ensureThumbnail falls back to an ffmpeg frame
// grab for the sources that don't expose one.
func buildYtDlpArgs(url string, quality string, downloadsDir string) []string {
	args := []string{"--newline", "-o", filepath.Join(downloadsDir, "%(title)s.%(ext)s")}

	switch quality {
	case "1080p":
		args = append(args, "-f", "bv*[height<=1080]+ba/b[height<=1080]", "--merge-output-format", "mp4")
	case "720p":
		args = append(args, "-f", "bv*[height<=720]+ba/b[height<=720]", "--merge-output-format", "mp4")
	case "audio":
		args = append(args, "-x", "--audio-format", "mp3")
	default: // "best"
		args = append(args, "-f", "bv*+ba/b", "--merge-output-format", "mp4")
	}

	if quality != "audio" {
		args = append(args, "--write-thumbnail", "--convert-thumbnails", "jpg")
	}

	return append(args, url)
}

// ensureThumbnail returns the basename of a jpg thumbnail for videoPath. If
// yt-dlp didn't manage to fetch/convert one (some extractors don't expose a
// thumbnail), it falls back to grabbing a frame at the 2s mark via ffmpeg.
func ensureThumbnail(videoPath string) string {
	if videoPath == "" {
		return ""
	}

	thumbnailPath := strings.TrimSuffix(videoPath, filepath.Ext(videoPath)) + ".jpg"
	if _, err := os.Stat(thumbnailPath); err == nil {
		return filepath.Base(thumbnailPath)
	}

	cmd := exec.Command(
		"ffmpeg", "-y",
		"-i", videoPath,
		"-ss", "00:00:02",
		"-vframes", "1",
		"-vf", "scale=320:-1",
		thumbnailPath,
	)
	if err := cmd.Run(); err != nil {
		return ""
	}
	if _, err := os.Stat(thumbnailPath); err != nil {
		return ""
	}
	return filepath.Base(thumbnailPath)
}

// handleOutputLine feeds a single line of yt-dlp's stdout into the job's
// state machine. Note that the "Destination:" marker is only treated as a
// new track when it comes from a "[download]" line — the same marker also
// appears on the "[Merger]"/"[ExtractAudio]" post-processing lines, which
// describe the final merged/converted output rather than another stream to
// track progress for.
func (a *App) handleOutputLine(job *DownloadJob, line string) {
	if matches := trackDestRe.FindStringSubmatch(line); matches != nil {
		a.startTrack(job, matches[1])
		return
	}

	if matches := trackProgressRe.FindStringSubmatch(line); matches != nil {
		percent, _ := strconv.ParseFloat(matches[1], 64)
		a.updateTrackProgress(job, percent)
		return
	}

	if matches := mergeDestRe.FindStringSubmatch(line); matches != nil {
		a.startProcessing(job, matches[1])
		return
	}

	if matches := extractAudioRe.FindStringSubmatch(line); matches != nil {
		a.startProcessing(job, matches[1])
		return
	}
}

func classifyTrack(filename string) TrackKind {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".m4a", ".opus", ".mp3", ".aac", ".ogg", ".wav":
		return TrackAudio
	default:
		return TrackVideo
	}
}

func (a *App) startTrack(job *DownloadJob, filename string) {
	// YouTube serves audio-only streams as opus-in-webm, which is
	// indistinguishable from a video container by extension alone. The
	// "audio" quality already guarantees there's no video stream, so trust
	// that over the extension guess.
	kind := classifyTrack(filename)
	if job.Quality == "audio" {
		kind = TrackAudio
	}

	a.mu.Lock()
	job.Tracks = append(job.Tracks, DownloadTrack{
		Kind:     kind,
		Filename: filename,
	})
	a.mu.Unlock()

	runtime.EventsEmit(a.ctx, "download-progress", job)
}

func (a *App) updateTrackProgress(job *DownloadJob, percent float64) {
	a.mu.Lock()
	if len(job.Tracks) > 0 {
		job.Tracks[len(job.Tracks)-1].Progress = percent
	}
	a.mu.Unlock()

	runtime.EventsEmit(a.ctx, "download-progress", job)
}

// startProcessing marks the job as being merged/converted by ffmpeg and
// records the resulting output path.
func (a *App) startProcessing(job *DownloadJob, filePath string) {
	a.mu.Lock()
	job.Status = StatusProcessing
	job.FilePath = filePath
	a.mu.Unlock()

	a.persistJob(job)
	runtime.EventsEmit(a.ctx, "download-status", job)
}

func (a *App) updateStatus(job *DownloadJob, status DownloadStatus) {
	a.mu.Lock()
	job.Status = status
	a.mu.Unlock()

	a.persistJob(job)
	runtime.EventsEmit(a.ctx, "download-status", job)
}

func (a *App) setError(job *DownloadJob, message string) {
	a.mu.Lock()
	job.Status = StatusError
	job.Error = message
	a.mu.Unlock()

	a.persistJob(job)
	runtime.EventsEmit(a.ctx, "download-status", job)
}

func fileTitle(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func probeDuration(filePath string) float64 {
	if filePath == "" {
		return 0
	}

	out, err := exec.Command(
		"ffprobe", "-v", "quiet",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	).Output()
	if err != nil {
		return 0
	}

	duration, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0
	}
	return duration
}

// finishJob moves a successfully completed job out of the active-jobs table
// and into the videos library, looking up its duration via ffprobe.
func (a *App) finishJob(job *DownloadJob) {
	a.mu.Lock()
	if job.FilePath == "" && len(job.Tracks) > 0 {
		job.FilePath = job.Tracks[len(job.Tracks)-1].Filename
	}
	job.Title = fileTitle(job.FilePath)
	job.Status = StatusDone
	filePath := job.FilePath
	title := job.Title
	quality := job.Quality
	a.mu.Unlock()

	thumbnailFilename := ""
	if quality != "audio" {
		thumbnailFilename = ensureThumbnail(filePath)
	}

	video := &VideoRecord{
		ID:              job.ID,
		Title:           title,
		Filename:        filepath.Base(filePath),
		DurationSeconds: probeDuration(filePath),
		ThumbnailPath:   thumbnailFilename,
		DownloadedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	if a.db != nil {
		if err := a.db.saveVideo(video); err != nil {
			log.Printf("ytgrabber: failed to save video: %v", err)
		}
		if err := a.db.deleteJob(job.ID); err != nil {
			log.Printf("ytgrabber: failed to delete job: %v", err)
		}
	}

	a.mu.Lock()
	delete(a.jobs, job.ID)
	a.mu.Unlock()

	// Emitted only after the video row is committed and the job row is gone,
	// so a frontend listener that reacts to "done" by re-querying GetLibrary
	// is guaranteed to see the new entry.
	runtime.EventsEmit(a.ctx, "download-status", job)
}

func (a *App) persistJob(job *DownloadJob) {
	if a.db == nil {
		return
	}

	a.mu.RLock()
	snapshot := *job
	a.mu.RUnlock()

	if err := a.db.saveJob(&snapshot); err != nil {
		log.Printf("ytgrabber: failed to persist job: %v", err)
	}
}

// RemoveJob cancels an in-flight job (if any) and deletes it from both the
// in-memory map and SQLite. Cancelling is safe to call even when the job
// already finished naturally — context.CancelFunc is a no-op after the
// context is done — so callers don't need to special-case job status here.
func (a *App) RemoveJob(id string) error {
	a.mu.Lock()
	_, exists := a.jobs[id]
	cancel, hasCancel := a.cancels[id]
	a.mu.Unlock()

	if !exists {
		return fmt.Errorf("job %s not found", id)
	}

	if hasCancel {
		cancel()
	}

	a.mu.Lock()
	delete(a.jobs, id)
	a.mu.Unlock()

	if a.db != nil {
		if err := a.db.deleteJob(id); err != nil {
			log.Printf("ytgrabber: failed to delete job %s: %v", id, err)
		}
	}

	runtime.EventsEmit(a.ctx, "job-removed", id)
	return nil
}

// GetJobs возвращает список задач из SQLite — источника правды между
// перезапусками приложения. In-memory map используется только для
// live-обновлений (прогресс, треки) в рамках текущей сессии.
func (a *App) GetJobs() []*DownloadJob {
	if a.db == nil {
		return []*DownloadJob{}
	}

	jobs, err := a.db.loadJobs()
	if err != nil {
		log.Printf("ytgrabber: failed to load jobs: %v", err)
		return []*DownloadJob{}
	}
	return jobs
}

// GetLibrary возвращает список скачанных видео из SQLite.
func (a *App) GetLibrary() []*VideoRecord {
	if a.db == nil {
		return []*VideoRecord{}
	}

	videos, err := a.db.loadVideos()
	if err != nil {
		log.Printf("ytgrabber: failed to load videos: %v", err)
		return []*VideoRecord{}
	}
	return videos
}
