package server

import (
	"context"
	"log"
	"time"

	"ytgrabber/internal/core"
)

// Retry delays for EnsureTools: the first start may have no network yet.
const (
	firstRetry = 30 * time.Second
	maxRetry   = 30 * time.Minute
)

// EnsureTools installs the missing required tools (yt-dlp, ffmpeg, deno)
// into the data dir and keeps retrying with backoff until they are there
// or ctx ends. The API works meanwhile; the frontend sees the install
// progress through tool-install-progress events.
func EnsureTools(ctx context.Context, app *core.App) {
	delay := firstRetry
	for {
		if !missingInstallable(app) {
			return
		}
		if _, err := app.InstallTools(nil); err != nil {
			log.Printf("ytgrabber-server: installing tools failed, retrying in %s: %v", delay, err)
		} else if !missingInstallable(app) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, maxRetry)
	}
}

func missingInstallable(app *core.App) bool {
	for _, tool := range app.GetDependencies().Tools {
		if tool.Required && !tool.Found && !tool.Custom && tool.Installable {
			return true
		}
	}
	return false
}
