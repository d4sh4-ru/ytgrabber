package core

import (
	"mime"
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
// which <video> needs for seeking. With ?download=1 the file comes as an
// attachment under its own name, for "save to device". userOf says whose
// library a request may read: another user's file is "not found".
func MediaHandler(a *App, userOf func(*http.Request) UserID) http.Handler {
	return mediaHandler{app: a, userOf: userOf}
}

type mediaHandler struct {
	app    *App
	userOf func(*http.Request) UserID
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

	video, err := h.app.db.GetVideo(string(h.userOf(r)), id)
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

	name = filepath.Base(path)
	if r.URL.Query().Get("download") == "1" {
		// FormatMediaType writes non-ASCII names as filename*=utf-8''…
		// (RFC 2231/5987), so Cyrillic titles survive the download.
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	http.ServeContent(w, r, name, info.ModTime(), file)
}
