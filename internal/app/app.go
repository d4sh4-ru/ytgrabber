// Package app is the desktop adapter: it binds the application core
// (package core) to Wails. Every exported method of App is bound by Wails
// and callable from TypeScript; each one forwards to the core on behalf of
// the single local user. Only what needs the window lives here: file
// dialogs, opening a URL in the browser, quitting, and delivering events.
//
// Connected to a server (package remote), the window talks to the server
// instead: through the same origin, which the desktop forwards. The core
// keeps running for the app's own updates, but downloads nothing.
package app

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"ytgrabber/internal/core"
	"ytgrabber/internal/platform"
	"ytgrabber/internal/remote"
)

// user is whose data the desktop works with: there is only one.
const user = core.LocalUser

type App struct {
	ctx  context.Context
	core *core.App

	remote       *remote.Client
	remoteConfig string // where remote.Config is kept
	stopRemote   context.CancelFunc
}

func New(paths platform.Paths) *App {
	a := &App{
		core:         core.New(paths),
		remoteConfig: filepath.Join(paths.DataDir, "remote.json"),
		stopRemote:   func() {},
	}
	a.remote = remote.New(remote.SystemSecrets{}, a.emit)
	return a
}

// Hooks returns the Wails lifecycle callbacks. They are handed out by a
// function instead of being exported methods because Wails binds every
// exported method of App to the frontend.
func Hooks(a *App) (onStartup func(context.Context), onShutdown func(context.Context)) {
	return a.startup, a.shutdown
}

// Handler answers what the WebView asks for beyond the built frontend:
// library files (core.MediaHandler) in the local mode; the server's API
// and files, forwarded by package remote, when connected to a server.
func Handler(a *App) http.Handler {
	local := core.MediaHandler(a.core, func(*http.Request) core.UserID { return core.LocalUser })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.remote.Server() != "" {
			a.remote.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		local.ServeHTTP(w, r)
	})
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.core.Start(wailsEvents{ctx: ctx}, func() { runtime.Quit(ctx) })

	remoteCtx, stop := context.WithCancel(ctx)
	a.stopRemote = stop
	go a.remote.Run(remoteCtx)
	cfg, err := remote.LoadConfig(a.remoteConfig)
	if err != nil {
		log.Printf("ytgrabber: %v; starting in the local mode", err)
		return
	}
	if cfg.UseServer {
		if server, err := remote.ParseServer(cfg.Server); err == nil {
			a.remote.Use(server)
		}
	}
}

func (a *App) shutdown(context.Context) {
	a.stopRemote()
	a.core.Stop()
}

// emit sends an event to the window; package remote reports through it.
func (a *App) emit(name string, data any) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, name, data)
	}
}

// wailsEvents is the desktop core.EventSink.
type wailsEvents struct {
	ctx context.Context
}

func (e wailsEvents) Publish(name string, data ...interface{}) {
	runtime.EventsEmit(e.ctx, name, data...)
}

// existingDirOrHome is where the folder dialog opens: the nearest existing
// parent of dir.
func existingDirOrHome(dir string) string {
	for current := dir; current != "" && current != filepath.Dir(current); current = filepath.Dir(current) {
		if info, err := os.Stat(current); err == nil && info.IsDir() {
			return current
		}
	}
	return platform.HomeDir()
}
