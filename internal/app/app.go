// Package app is the desktop adapter: it binds the application core
// (package core) to Wails. Every exported method of App is bound by Wails
// and callable from TypeScript; each one forwards to the core on behalf of
// the single local user. Only what needs the window lives here: file
// dialogs, opening a URL in the browser, quitting, and delivering events.
package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"ytgrabber/internal/core"
	"ytgrabber/internal/platform"
)

// user is whose data the desktop works with: there is only one.
const user = core.LocalUser

type App struct {
	ctx  context.Context
	core *core.App
}

func New(paths platform.Paths) *App {
	return &App{core: core.New(paths)}
}

// Hooks returns the Wails lifecycle callbacks. They are handed out by a
// function instead of being exported methods because Wails binds every
// exported method of App to the frontend.
func Hooks(a *App) (onStartup func(context.Context), onShutdown func(context.Context)) {
	return a.startup, a.shutdown
}

// MediaHandler serves library files to the WebView; see core.MediaHandler.
func MediaHandler(a *App) http.Handler {
	return core.MediaHandler(a.core, func(*http.Request) core.UserID { return core.LocalUser })
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.core.Start(wailsEvents{ctx: ctx}, func() { runtime.Quit(ctx) })
}

func (a *App) shutdown(context.Context) {
	a.core.Stop()
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
