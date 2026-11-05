// Package core is the application itself: storage, the external tools,
// accounts and the download queue. It knows nothing about the window it
// runs in — the desktop adapter (package app) binds it to Wails, and a
// server can expose the same API over HTTP. Events go out through an
// EventSink; anything that needs a window (file dialogs, opening a URL)
// stays in the adapter, which passes the result in.
package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"ytgrabber/internal/accounts"
	"ytgrabber/internal/browsers"
	"ytgrabber/internal/installer"
	"ytgrabber/internal/logging"
	"ytgrabber/internal/platform"
	"ytgrabber/internal/storage"
	"ytgrabber/internal/tools"
	"ytgrabber/internal/updater"
)

// Version is shown in the settings screen. Release builds set it from the
// git tag (see .github/workflows/release.yml):
//
//	wails build -ldflags "-X ytgrabber/internal/core.Version=1.2.3"
var Version = "dev"

const shutdownTimeout = 10 * time.Second

// EventSink delivers events to the frontend: runtime.EventsEmit on the
// desktop, a WebSocket on the server.
type EventSink interface {
	Publish(name string, data ...interface{})
}

// EventFunc adapts a function to EventSink.
type EventFunc func(name string, data ...interface{})

func (f EventFunc) Publish(name string, data ...interface{}) { f(name, data...) }

// App is the application core. Its exported methods are the API the
// frontend uses, through whichever adapter.
type App struct {
	paths      platform.Paths
	db         *storage.DB
	logFile    io.Closer
	startupErr string

	// events reaches the frontend and quit closes the app (after an
	// update is installed or all data deleted); both are no-ops until
	// Start, and in tests.
	events       EventSink
	quit         func()
	shuttingDown atomic.Bool

	// startHelper launches the post-exit cleanup (startCleanupHelper);
	// cleanupPipe is held open until exit for it.
	startHelper func(paths []string) error
	cleanupPipe io.Closer

	// installer downloads missing tools (see InstallTools); cancelInstall
	// stops the running install, if any.
	installer     installer.Installer
	installMu     sync.Mutex
	cancelInstall context.CancelFunc

	// accounts holds the encrypted cookies of connected sites;
	// browserEnv says where to look for browser profiles.
	accounts   *accounts.Store
	browserEnv browsers.Env

	// updater finds and downloads new versions; detectInstall says how
	// this copy can be replaced (both swapped out in tests).
	updater       *updater.Client
	detectInstall func() updater.Install
	updateMu      sync.Mutex
	update        updateState
	stopWatching  context.CancelFunc

	mu       sync.RWMutex // guards everything below
	settings Settings
	tools    tools.Set
	jobs     map[string]*job
	queue    []string
	cancels  map[string]context.CancelCauseFunc
	running  int

	workers sync.WaitGroup
}

// UserID says whose data a call is about. The desktop has a single local
// user; the server will pass the signed-in one. Jobs, the library,
// accounts and the downloads folder belong to a user; tools, updates and
// the app itself are shared.
type UserID string

// LocalUser is the only user of the desktop app.
const LocalUser UserID = "local"

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
		paths:      paths,
		events:     EventFunc(func(string, ...interface{}) {}),
		quit:       func() {},
		jobs:       make(map[string]*job),
		cancels:    make(map[string]context.CancelCauseFunc),
		accounts:   accounts.NewStore(paths.AccountsDir()),
		browserEnv: browsers.CurrentEnv(),

		updater:       newUpdateClient(),
		detectInstall: currentInstall,
		stopWatching:  func() {},
	}
	a.startHelper = a.startCleanupHelper
	a.installer = installer.Installer{
		Dir:       paths.ToolsDir(),
		Client:    &http.Client{Timeout: installTimeout},
		UserAgent: "ytgrabber/" + Version,
	}
	return a
}

// Start sets up file logging, opens the data and starts watching for
// updates. events receives everything the frontend should hear about;
// quit is called when the app has to exit (nil: never).
//
// Logging must not be set up earlier, in main: `wails dev/build` run main
// while generating bindings, which would create the production data
// directory on the developer's machine.
func (a *App) Start(events EventSink, quit func()) {
	if events != nil {
		a.events = events
	}
	if quit != nil {
		a.quit = quit
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
		return
	}

	watchCtx, stop := context.WithCancel(context.Background())
	a.stopWatching = stop
	go a.watchForUpdates(watchCtx)
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

	a.cleanRunDir()

	if err := db.MarkInterruptedJobs(interruptedMessage); err != nil {
		log.Printf("ytgrabber: failed to mark interrupted jobs: %v", err)
	}
	stored, err := db.LoadJobs()
	if err != nil {
		db.Close()
		return fmt.Errorf("не удалось загрузить задачи: %w", err)
	}

	toolSet := a.resolveTools(settings)
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

// Stop stops every download, waits for their goroutines to record the
// interruption, and only then closes the database.
func (a *App) Stop() {
	a.stopWatching()
	a.stopAllDownloads(errShutdown)

	if a.db != nil {
		if err := a.db.Close(); err != nil {
			log.Printf("ytgrabber: failed to close database: %v", err)
		}
	}
	// After everything is closed, so the new version starts clean.
	a.startRelaunch()
	if a.logFile != nil {
		a.logFile.Close()
	}
}

// stopAllDownloads cancels every job and waits for the workers to exit.
// No new jobs are scheduled and no events are sent afterwards.
func (a *App) stopAllDownloads(cause error) {
	a.shuttingDown.Store(true)
	a.CancelToolInstall()

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

// Ready reports why the app can't work, if it can't: adapters check it
// before asking the user for a file the core then couldn't use.
func (a *App) Ready() error {
	return a.ensureReady()
}

// ensureReady guards every API method: without a database the app can
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
	a.events.Publish(name, data...)
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
	a.tools = a.resolveTools(a.settings)
	toolSet := a.tools
	a.mu.Unlock()

	toolSet.FillVersions()
	return withInstallInfo(toolSet.Report())
}

// UpdateYtDlp запускает самообновление yt-dlp и возвращает его вывод.
func (a *App) UpdateYtDlp() (string, error) {
	a.mu.RLock()
	toolSet := a.tools
	a.mu.RUnlock()
	return toolSet.UpdateYtDlp()
}
