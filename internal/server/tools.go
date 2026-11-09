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

// updateRetry is how soon a failed yt-dlp update check is tried again
// when the interval is longer.
const updateRetry = time.Hour

// ytDlpUpdater is the part of core.App AutoUpdateYtDlp needs.
type ytDlpUpdater interface {
	UpdateYtDlpIfOutdated(ctx context.Context) (version string, updated bool, err error)
}

// AutoUpdateYtDlp keeps the yt-dlp the server installed up to date: sites
// break old versions within days, and a server has nobody to press
// "update". It checks right away (after EnsureTools, so the tools are
// there) and then every interval until ctx ends.
func AutoUpdateYtDlp(ctx context.Context, app ytDlpUpdater, interval time.Duration) {
	for {
		delay := interval
		version, updated, err := app.UpdateYtDlpIfOutdated(ctx)
		switch {
		case err != nil:
			log.Printf("ytgrabber-server: checking for a yt-dlp update failed: %v", err)
			delay = min(interval, updateRetry)
		case updated:
			log.Printf("ytgrabber-server: yt-dlp updated to %s", version)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}
