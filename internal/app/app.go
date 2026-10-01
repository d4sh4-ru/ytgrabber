// Package app is the backend the frontend talks to: every exported method
// of App is bound by Wails and callable from TypeScript. It wires storage,
// the external tools and the download queue together.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"ytgrabber/internal/logging"
	"ytgrabber/internal/platform"
	"ytgrabber/internal/storage"
	"ytgrabber/internal/tools"
)

// Version is shown in the settings screen. Keep it in sync with
// info.productVersion in wails.json, which feeds the platform bundle
// metadata (Info.plist, Windows version resource).
const Version = "1.0.0"

const shutdownTimeout = 10 * time.Second

type App struct {
	ctx        context.Context
	paths      platform.Paths
	db         *storage.DB
	logFile    io.Closer
	startupErr string

	// emit sends an event to the frontend and quit closes the app; both
	// are no-ops outside Wails (tests).
	emit         func(name string, data ...interface{})
	quit         func()
	shuttingDown atomic.Bool

	// startHelper launches the post-exit cleanup (startCleanupHelper);
	// cleanupPipe is held open until exit for it.
	startHelper func(paths []string) error
	cleanupPipe io.Closer

	mu       sync.RWMutex // guards everything below
	settings Settings
	tools    tools.Set
	jobs     map[string]*job
	queue    []string
	cancels  map[string]context.CancelCauseFunc
	running  int

	workers sync.WaitGroup
}

// AppInfo is general information for the settings screen, plus the startup
// error (if any) so the UI can explain why nothing works.
type AppInfo struct {
	Version      string `json:"version"`
	Platform     string `json:"platform"`
	DataDir      string `json:"dataDir"`
	LogPath      string `json:"logPath"`
	StartupError string `json:"startupError"`
}

func New(paths platform.Paths) *App {
	a := &App{
		paths:   paths,
		emit:    func(string, ...interface{}) {},
		quit:    func() {},
		jobs:    make(map[string]*job),
		cancels: make(map[string]context.CancelCauseFunc),
	}
	a.startHelper = a.startCleanupHelper
	return a
}

// Hooks returns the Wails lifecycle callbacks. They are handed out by a
// function instead of being exported methods because Wails binds every
// exported method of App to the frontend.
func Hooks(a *App) (onStartup func(context.Context), onShutdown func(context.Context)) {
	return a.startup, a.shutdown
}

// startup also sets up file logging. It must not happen earlier, in main:
// `wails dev/build` run main while generating bindings, which would create
// the production data directory on the developer's machine.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.emit = func(name string, data ...interface{}) {
		runtime.EventsEmit(ctx, name, data...)
	}
	a.quit = func() {
		runtime.Quit(ctx)
	}

	if logFile, err := logging.Setup(a.paths.LogPath()); err != nil {
		log.Printf("ytgrabber: file logging disabled: %v", err)
	} else {
		a.logFile = logFile
	}
	log.Printf("ytgrabber %s starting on %s, data dir %s", Version, platform.Name(), a.paths.DataDir)

	if err := a.init(); err != nil {
		log.Printf("ytgrabber: startup failed: %v", err)
		a.startupErr = err.Error()
	}
}

// init opens the database, loads settings, locates tools and restores the
// job list. Jobs that were running when the app last exited become
// retryable errors.
func (a *App) init() error {
	if err := os.MkdirAll(a.paths.DataDir, 0o755); err != nil {
		return fmt.Errorf("не удалось создать папку данных %s: %w", a.paths.DataDir, err)
	}

	db, err := storage.Open(a.paths.DBPath(), storage.MigrationEnv{LegacyDownloadsDir: a.paths.LegacyDownloadsDir})
	if err != nil {
		return fmt.Errorf("не удалось открыть базу данных %s: %w", a.paths.DBPath(), err)
	}

	settings, err := loadSettings(db, a.paths.DefaultDownloadsDir)
	if err != nil {
		db.Close()
		return fmt.Errorf("не удалось загрузить настройки: %w", err)
	}

	if err := db.MarkInterruptedJobs(interruptedMessage); err != nil {
		log.Printf("ytgrabber: failed to mark interrupted jobs: %v", err)
	}
	stored, err := db.LoadJobs()
	if err != nil {
		db.Close()
		return fmt.Errorf("не удалось загрузить задачи: %w", err)
	}

	toolSet := settings.resolveTools()
	for _, status := range toolSet.List() {
		log.Printf("ytgrabber: %s found=%v path=%q", status.Name, status.Found, status.Path)
	}

	a.mu.Lock()
	a.db = db
	a.settings = settings
	a.tools = toolSet
	for _, stored := range stored {
		a.jobs[stored.ID] = &job{DownloadJob: *stored}
	}
	a.mu.Unlock()
	return nil
}

// shutdown stops every download, waits for their goroutines to record the
// interruption, and only then closes the database.
func (a *App) shutdown(context.Context) {
	a.stopAllDownloads(errShutdown)

	if a.db != nil {
		if err := a.db.Close(); err != nil {
			log.Printf("ytgrabber: failed to close database: %v", err)
		}
	}
	if a.logFile != nil {
		a.logFile.Close()
	}
}

// stopAllDownloads cancels every job and waits for the workers to exit.
// No new jobs are scheduled and no events are sent afterwards.
func (a *App) stopAllDownloads(cause error) {
	a.shuttingDown.Store(true)

	a.mu.Lock()
	a.queue = nil
	for _, cancel := range a.cancels {
		cancel(cause)
	}
	a.mu.Unlock()

	done := make(chan struct{})
	go func() {
		a.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(shutdownTimeout):
		log.Printf("ytgrabber: downloads did not stop within %s", shutdownTimeout)
	}
}

// ensureReady guards every bound method: without a database the app can
// only report why it failed to start.
func (a *App) ensureReady() error {
	if a.db != nil {
		return nil
	}
	if a.startupErr != "" {
		return errors.New(a.startupErr)
	}
	return errors.New("приложение ещё не инициализировано")
}

// publish emits an event unless the app is shutting down — the frontend
// may already be gone by then.
func (a *App) publish(name string, data ...interface{}) {
	if a.shuttingDown.Load() {
		return
	}
	a.emit(name, data...)
}

// GetAppInfo возвращает версию, пути к данным/логу и ошибку запуска.
func (a *App) GetAppInfo() AppInfo {
	return AppInfo{
		Version:      Version,
		Platform:     platform.Name(),
		DataDir:      a.paths.DataDir,
		LogPath:      a.paths.LogPath(),
		StartupError: a.startupErr,
	}
}

// GetDependencies заново ищет внешние программы (пользователь мог
// установить их, не перезапуская приложение) и определяет их версии.
func (a *App) GetDependencies() tools.Report {
	a.mu.Lock()
	a.tools = a.settings.resolveTools()
	toolSet := a.tools
	a.mu.Unlock()

	toolSet.FillVersions()
	return toolSet.Report()
}

// UpdateYtDlp запускает самообновление yt-dlp и возвращает его вывод.
func (a *App) UpdateYtDlp() (string, error) {
	a.mu.RLock()
	toolSet := a.tools
	a.mu.RUnlock()
	return toolSet.UpdateYtDlp()
}
