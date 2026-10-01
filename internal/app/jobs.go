package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"ytgrabber/internal/media"
	"ytgrabber/internal/model"
	"ytgrabber/internal/tools"
	"ytgrabber/internal/ytdlp"
)

// job is a download plus the bookkeeping the frontend doesn't see.
type job struct {
	model.DownloadJob

	// outputFilename is yt-dlp's planned output name, known before the
	// download starts; used to clean up after a cancelled job.
	outputFilename   string
	lastProgressEmit time.Time
}

// snapshot copies the job so it can be serialised or persisted outside the
// lock while the download goroutine keeps mutating the original. Callers
// must hold a.mu.
func (j *job) snapshot() model.DownloadJob {
	snapshot := j.DownloadJob
	snapshot.Tracks = append([]model.DownloadTrack{}, j.Tracks...)
	return snapshot
}

// Cancellation causes, so runDownload can tell a user removing the job
// apart from the app shutting down or all data being deleted.
var (
	errJobRemoved = errors.New("job removed")
	errShutdown   = errors.New("application shutting down")
	errReset      = errors.New("all data is being deleted")
)

const (
	interruptedMessage   = "Загрузка прервана: приложение было закрыто. Нажмите «Повторить», чтобы продолжить."
	progressEmitInterval = 250 * time.Millisecond
	stderrTailLines      = 50
)

// DownloadVideo ставит загрузку в очередь и возвращает id задачи.
func (a *App) DownloadVideo(rawURL string, quality string) (string, error) {
	if err := a.ensureReady(); err != nil {
		return "", err
	}

	sourceURL, err := ytdlp.NormalizeURL(rawURL)
	if err != nil {
		return "", err
	}
	if !ytdlp.IsValidQuality(quality) {
		return "", fmt.Errorf("неизвестное качество %q", quality)
	}
	if err := a.checkToolsReady(); err != nil {
		return "", err
	}

	j := &job{DownloadJob: model.DownloadJob{
		ID:        uuid.NewString(),
		URL:       sourceURL,
		Quality:   quality,
		Status:    model.StatusPending,
		Tracks:    []model.DownloadTrack{},
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}}

	a.mu.Lock()
	if err := a.db.SaveJob(&j.DownloadJob); err != nil {
		a.mu.Unlock()
		return "", fmt.Errorf("не удалось сохранить задачу: %w", err)
	}
	a.jobs[j.ID] = j
	a.queue = append(a.queue, j.ID)
	snapshot := j.snapshot()
	a.mu.Unlock()

	a.publish("download-status", snapshot)
	a.schedule()
	return j.ID, nil
}

// RetryJob перезапускает задачу, завершившуюся ошибкой.
func (a *App) RetryJob(id string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	if err := a.checkToolsReady(); err != nil {
		return err
	}

	a.mu.Lock()
	j, ok := a.jobs[id]
	if !ok {
		a.mu.Unlock()
		return errors.New("задача не найдена")
	}
	if j.Status != model.StatusError {
		a.mu.Unlock()
		return errors.New("повторить можно только задачу, завершившуюся ошибкой")
	}
	j.Status = model.StatusPending
	j.Error = ""
	j.FilePath = ""
	j.Tracks = []model.DownloadTrack{}
	a.queue = append(a.queue, id)
	a.mu.Unlock()

	a.saveAndPublish(j, "download-status")
	a.schedule()
	return nil
}

// RemoveJob отменяет задачу (если она выполняется), удаляет её из памяти и
// SQLite и подчищает недокачанные файлы. Удаление уже отсутствующей задачи
// не считается ошибкой.
func (a *App) RemoveJob(id string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}

	a.mu.Lock()
	j, exists := a.jobs[id]
	cancel, running := a.cancels[id]
	var snapshot model.DownloadJob
	var outputFilename string
	if exists {
		snapshot = j.snapshot()
		outputFilename = j.outputFilename
	}
	delete(a.jobs, id)
	a.queue = slices.DeleteFunc(a.queue, func(queued string) bool { return queued == id })
	err := a.db.DeleteJob(id)
	a.mu.Unlock()

	if err != nil {
		return fmt.Errorf("не удалось удалить задачу: %w", err)
	}

	if running {
		// runDownload cleans up once the process tree is dead.
		cancel(errJobRemoved)
	} else if exists {
		ytdlp.RemovePartialFiles(snapshot.Tracks, outputFilename, a.isFileReferenced)
	}

	a.publish("job-removed", id)
	return nil
}

// GetJobs возвращает все незавершённые задачи (в очереди, активные и с
// ошибкой) в порядке добавления.
func (a *App) GetJobs() ([]model.DownloadJob, error) {
	if err := a.ensureReady(); err != nil {
		return []model.DownloadJob{}, err
	}

	a.mu.RLock()
	jobs := make([]model.DownloadJob, 0, len(a.jobs))
	for _, j := range a.jobs {
		jobs = append(jobs, j.snapshot())
	}
	a.mu.RUnlock()

	sort.Slice(jobs, func(i, k int) bool {
		return jobs[i].CreatedAt < jobs[k].CreatedAt
	})
	return jobs, nil
}

func (a *App) checkToolsReady() error {
	a.mu.RLock()
	toolSet := a.tools
	a.mu.RUnlock()

	if !toolSet.YtDlp.Found {
		return errors.New("yt-dlp не найден. Установите его или укажите путь в настройках.")
	}
	if !toolSet.Ffmpeg.Found {
		return errors.New("ffmpeg не найден. Установите его или укажите путь в настройках.")
	}
	return nil
}

// schedule starts queued jobs while fewer than MaxConcurrentDownloads are
// running. It is called whenever a slot may have opened up.
func (a *App) schedule() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.shuttingDown.Load() {
		return
	}

	for a.running < a.settings.MaxConcurrentDownloads && len(a.queue) > 0 {
		id := a.queue[0]
		a.queue = a.queue[1:]

		j, ok := a.jobs[id]
		if !ok || j.Status != model.StatusPending {
			continue
		}

		ctx, cancel := context.WithCancelCause(context.Background())
		a.cancels[id] = cancel
		a.running++
		a.workers.Add(1)
		go a.runDownload(ctx, j)
	}
}

func (a *App) runDownload(ctx context.Context, j *job) {
	defer func() {
		a.mu.Lock()
		if cancel, ok := a.cancels[j.ID]; ok {
			cancel(nil)
			delete(a.cancels, j.ID)
		}
		a.running--
		a.mu.Unlock()
		a.workers.Done()
		a.schedule()
	}()

	a.mu.RLock()
	toolSet := a.tools
	downloadsDir := a.settings.DownloadsDir
	a.mu.RUnlock()

	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		a.setError(j, fmt.Sprintf("Не удалось создать папку загрузок %s: %v", downloadsDir, err))
		return
	}

	a.setStatus(j, model.StatusDownloading)

	err := a.execYtDlp(ctx, j, toolSet, downloadsDir)
	if ctx.Err() != nil {
		a.handleCancelled(ctx, j)
		return
	}
	if err != nil {
		a.setError(j, err.Error())
		return
	}

	a.finishJob(ctx, j, toolSet)
}

func (a *App) handleCancelled(ctx context.Context, j *job) {
	cause := context.Cause(ctx)
	if errors.Is(cause, errReset) {
		// ResetAllData cleans up itself once every worker has stopped.
		return
	}
	if errors.Is(cause, errJobRemoved) {
		a.mu.RLock()
		snapshot := j.snapshot()
		outputFilename := j.outputFilename
		a.mu.RUnlock()
		ytdlp.RemovePartialFiles(snapshot.Tracks, outputFilename, a.isFileReferenced)
		return
	}
	// Shutdown: keep .part files so "Повторить" can resume the download.
	a.setError(j, interruptedMessage)
}

func (a *App) execYtDlp(ctx context.Context, j *job, toolSet tools.Set, downloadsDir string) error {
	cmd := toolSet.Command(ctx, toolSet.YtDlp.Path,
		ytdlp.BuildArgs(j.URL, j.Quality, downloadsDir, toolSet.Ffmpeg.Path)...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("не удалось запустить yt-dlp: %w", err)
	}

	stderrTail := ytdlp.NewLineTail(stderrTailLines)
	var stderrDone sync.WaitGroup
	stderrDone.Add(1)
	go func() {
		defer stderrDone.Done()
		readLines(stderr, stderrTail.Add)
	}()

	readLines(stdout, func(line string) {
		a.handleOutputLine(j, line)
	})
	stderrDone.Wait()

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New(ytdlp.ExtractErrorMessage(stderrTail.Lines()))
	}
	return nil
}

// readLines calls fn for every line of r. Unlike bufio.Scanner it has no
// line-length limit, so one huge line can't stall the reader (and with it
// the child process blocked on a full pipe).
func readLines(r io.Reader, fn func(string)) {
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			fn(strings.TrimRight(line, "\r\n"))
		}
		if err != nil {
			return
		}
	}
}

// handleOutputLine feeds one line of yt-dlp's stdout into the job.
func (a *App) handleOutputLine(j *job, line string) {
	event := ytdlp.ParseLine(line)

	switch event.Kind {
	case ytdlp.EventTitle:
		a.mu.Lock()
		j.Title = event.Value
		a.mu.Unlock()
		a.saveAndPublish(j, "download-status")

	case ytdlp.EventFilename:
		a.mu.Lock()
		j.outputFilename = event.Value
		a.mu.Unlock()

	case ytdlp.EventFilepath:
		a.mu.Lock()
		j.FilePath = event.Value
		a.mu.Unlock()

	case ytdlp.EventTrackStart:
		// YouTube serves audio-only streams as opus-in-webm, which is
		// indistinguishable from a video container by extension alone. The
		// "audio" quality already guarantees there's no video stream.
		kind := ytdlp.ClassifyTrack(event.Value)
		if j.Quality == "audio" {
			kind = model.TrackAudio
		}
		a.mu.Lock()
		j.Tracks = append(j.Tracks, model.DownloadTrack{Kind: kind, Filename: event.Value})
		a.mu.Unlock()
		a.publishIfActive(j, "download-progress")

	case ytdlp.EventProgress:
		a.mu.Lock()
		if len(j.Tracks) > 0 {
			j.Tracks[len(j.Tracks)-1].Progress = event.Percent
		}
		now := time.Now()
		shouldEmit := event.Percent >= 100 || now.Sub(j.lastProgressEmit) >= progressEmitInterval
		if shouldEmit {
			j.lastProgressEmit = now
		}
		a.mu.Unlock()
		if shouldEmit {
			a.publishIfActive(j, "download-progress")
		}

	case ytdlp.EventProcessing:
		a.mu.Lock()
		if j.FilePath == "" {
			j.FilePath = event.Value
		}
		a.mu.Unlock()
		a.setStatus(j, model.StatusProcessing)
	}
}

// finishJob moves a successfully completed job into the library: it
// resolves the final file, a thumbnail and the duration, then replaces the
// job row with a video row.
func (a *App) finishJob(ctx context.Context, j *job, toolSet tools.Set) {
	a.mu.Lock()
	if j.FilePath == "" && len(j.Tracks) > 0 {
		j.FilePath = j.Tracks[len(j.Tracks)-1].Filename
	}
	if j.Title == "" {
		j.Title = fileTitle(j.FilePath)
	}
	filePath, title, sourceURL, quality, id := j.FilePath, j.Title, j.URL, j.Quality, j.ID
	a.mu.Unlock()

	if filePath == "" {
		a.setError(j, "yt-dlp не сообщил, куда сохранён файл")
		return
	}
	if _, err := os.Stat(filePath); err != nil {
		a.setError(j, fmt.Sprintf("Итоговый файл не найден: %s", filePath))
		return
	}

	a.setStatus(j, model.StatusProcessing)

	kind := model.MediaVideo
	if quality == "audio" {
		kind = model.MediaAudio
	}

	video := &model.VideoRecord{
		ID:              id,
		Title:           title,
		SourceURL:       sourceURL,
		MediaKind:       kind,
		FilePath:        filePath,
		ThumbnailPath:   media.EnsureThumbnail(ctx, toolSet, filePath, kind),
		DurationSeconds: media.ProbeDuration(ctx, toolSet, filePath),
		DownloadedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	if ctx.Err() != nil {
		return
	}

	a.mu.Lock()
	if _, ok := a.jobs[id]; !ok {
		a.mu.Unlock()
		return
	}
	if err := a.db.SaveVideo(video); err != nil {
		a.mu.Unlock()
		a.setError(j, fmt.Sprintf("Не удалось добавить в библиотеку: %v", err))
		return
	}
	if err := a.db.DeleteJob(id); err != nil {
		log.Printf("ytgrabber: failed to delete finished job %s: %v", id, err)
	}
	j.Status = model.StatusDone
	delete(a.jobs, id)
	snapshot := j.snapshot()
	a.mu.Unlock()

	// Emitted only after the video row is committed and the job row is gone,
	// so a listener that re-queries GetLibrary is guaranteed to see it.
	a.publish("download-status", snapshot)
	a.publish("library-changed")
}

func (a *App) setStatus(j *job, status model.DownloadStatus) {
	a.mu.Lock()
	j.Status = status
	a.mu.Unlock()
	a.saveAndPublish(j, "download-status")
}

func (a *App) setError(j *job, message string) {
	a.mu.Lock()
	j.Status = model.StatusError
	j.Error = message
	a.mu.Unlock()
	a.saveAndPublish(j, "download-status")
}

// saveAndPublish persists the job and notifies the UI — unless the job was
// removed meanwhile, in which case a late update from its goroutine must
// not resurrect it. The existence check and the write share one lock with
// RemoveJob for that reason.
func (a *App) saveAndPublish(j *job, event string) {
	a.mu.Lock()
	if _, ok := a.jobs[j.ID]; !ok {
		a.mu.Unlock()
		return
	}
	snapshot := j.snapshot()
	err := a.db.SaveJob(&snapshot)
	a.mu.Unlock()

	if err != nil {
		log.Printf("ytgrabber: failed to persist job %s: %v", j.ID, err)
	}
	a.publish(event, snapshot)
}

func (a *App) publishIfActive(j *job, event string) {
	a.mu.RLock()
	_, ok := a.jobs[j.ID]
	snapshot := j.snapshot()
	a.mu.RUnlock()

	if ok {
		a.publish(event, snapshot)
	}
}

func (a *App) isFileReferenced(path string) bool {
	referenced, err := a.db.IsFileReferenced(path)
	if err != nil {
		// When in doubt, keep the file.
		return true
	}
	return referenced
}
