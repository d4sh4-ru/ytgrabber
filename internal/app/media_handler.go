package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const mediaRoutePrefix = "/media/"

// MediaHandler serves library files to the WebView under
// /media/<id>/file.<ext> and /media/<id>/thumbnail.jpg; main.go installs it
// as the Wails asset server fallback. Files are looked up by record id, so
// only files that are in the library can ever be served — no path from the
// URL reaches the filesystem. http.ServeContent handles Range requests,
// which <video> needs for seeking.
func MediaHandler(a *App) http.Handler {
	return mediaHandler{app: a}
}

type mediaHandler struct {
	app *App
}

func (h mediaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rest, ok := strings.CutPrefix(r.URL.Path, mediaRoutePrefix)
	if !ok || h.app.db == nil {
		http.NotFound(w, r)
		return
	}
	id, name, ok := strings.Cut(rest, "/")
	if !ok || id == "" {
		http.NotFound(w, r)
		return
	}

	video, err := h.app.db.GetVideo(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	path := video.FilePath
	if strings.HasPrefix(name, "thumbnail") {
		path = video.ThumbnailPath
	}
	if path == "" {
		http.NotFound(w, r)
		return
	}

	file, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
}
