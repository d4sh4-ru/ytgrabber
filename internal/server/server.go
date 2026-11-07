// Package server exposes the application core (package core) over HTTP,
// for browsers and phones on the same VPN: a JSON API under /api/v1, a
// WebSocket with the events the desktop gets from Wails, library files
// under /media/ and the web frontend itself.
//
// Until users and devices exist (issue #14) there is a single user,
// core.LocalUser, and a single access token from the configuration.
package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"

	"ytgrabber/internal/core"
)

// user is whose data the server works with until there are accounts.
const user = core.LocalUser

// maxJSONBody bounds request bodies the API decodes; cookies.txt uploads
// have their own limit (maxCookiesUpload).
const maxJSONBody = 1 << 20

// Config is what New needs besides the core.
type Config struct {
	// Token grants access to everything; see auth.go.
	Token string
	// WebDir holds the built frontend; empty serves the API only.
	WebDir string
}

// Server is the HTTP side of the app. Events is the core.EventSink to
// pass to core.App.Start.
type Server struct {
	app    *core.App
	auth   *auth
	Events *Hub
	webDir string
}

func New(app *core.App, cfg Config) (*Server, error) {
	if cfg.Token == "" {
		return nil, errors.New("server: empty access token")
	}
	return &Server{
		app:    app,
		auth:   newAuth(cfg.Token),
		Events: NewHub(),
		webDir: cfg.WebDir,
	}, nil
}

// Handler routes every request. Only /api/v1/health, the session
// endpoints and the frontend's own files are reachable without a token:
// the login screen is part of the frontend.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	protect := s.auth.require

	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": core.Version})
	})
	mux.HandleFunc("GET /api/v1/session", s.auth.getSession)
	mux.HandleFunc("POST /api/v1/session", s.auth.login)
	mux.HandleFunc("DELETE /api/v1/session", s.auth.logout)

	mux.Handle("GET /api/v1/events", protect(http.HandlerFunc(s.Events.serveWS)))

	api := func(pattern string, h func(*http.Request) (any, error)) {
		mux.Handle(pattern, protect(apiHandler(h)))
	}

	api("GET /api/v1/info", func(*http.Request) (any, error) {
		return s.app.GetAppInfo(), nil
	})

	// Downloads.
	api("GET /api/v1/jobs", func(*http.Request) (any, error) {
		return s.app.GetJobs(user)
	})
	api("POST /api/v1/jobs", func(r *http.Request) (any, error) {
		var body struct {
			URL        string `json:"url"`
			Quality    string `json:"quality"`
			UseAccount bool   `json:"useAccount"`
		}
		if err := decodeJSON(r, &body); err != nil {
			return nil, err
		}
		return s.app.DownloadVideo(user, body.URL, body.Quality, body.UseAccount)
	})
	api("POST /api/v1/jobs/{id}/retry", func(r *http.Request) (any, error) {
		var body struct {
			UseAccount bool `json:"useAccount"`
		}
		if err := decodeJSON(r, &body); err != nil {
			return nil, err
		}
		return nil, s.app.RetryJob(user, r.PathValue("id"), body.UseAccount)
	})
	api("DELETE /api/v1/jobs/{id}", func(r *http.Request) (any, error) {
		return nil, s.app.RemoveJob(user, r.PathValue("id"))
	})

	// Library.
	api("GET /api/v1/library", func(*http.Request) (any, error) {
		return s.app.GetLibrary(user)
	})
	api("DELETE /api/v1/library/{id}", func(r *http.Request) (any, error) {
		return nil, s.app.DeleteVideo(user, r.PathValue("id"), r.URL.Query().Get("deleteFiles") == "true")
	})
	api("PUT /api/v1/library/{id}/position", func(r *http.Request) (any, error) {
		var body struct {
			Seconds float64 `json:"seconds"`
		}
		if err := decodeJSON(r, &body); err != nil {
			return nil, err
		}
		return s.app.SavePlaybackPosition(user, r.PathValue("id"), body.Seconds)
	})
	mux.Handle("GET /media/", protect(core.MediaHandler(s.app)))

	// Site accounts. Browsers aren't scanned on a server: cookies come
	// as an uploaded cookies.txt.
	api("GET /api/v1/accounts", func(*http.Request) (any, error) {
		return s.app.GetAccounts(user)
	})
	api("POST /api/v1/accounts", s.importCookies)
	api("POST /api/v1/accounts/{id}/check", func(r *http.Request) (any, error) {
		return s.app.CheckAccount(user, r.PathValue("id"))
	})
	api("DELETE /api/v1/accounts/{id}", func(r *http.Request) (any, error) {
		return nil, s.app.RemoveAccount(user, r.PathValue("id"))
	})

	// Settings that make sense on a server: the downloads folder and tool
	// paths are fixed by its configuration.
	api("GET /api/v1/settings", func(*http.Request) (any, error) {
		return s.app.GetSettings(user), nil
	})
	api("PUT /api/v1/settings/max-concurrent-downloads", func(r *http.Request) (any, error) {
		var body struct {
			Value int `json:"value"`
		}
		if err := decodeJSON(r, &body); err != nil {
			return nil, err
		}
		return s.app.SetMaxConcurrentDownloads(body.Value)
	})

	// Tools.
	api("GET /api/v1/tools", func(*http.Request) (any, error) {
		return s.app.GetDependencies(), nil
	})
	api("POST /api/v1/tools/install", func(r *http.Request) (any, error) {
		var body struct {
			Names []string `json:"names"`
		}
		if err := decodeJSON(r, &body); err != nil {
			return nil, err
		}
		return s.app.InstallTools(body.Names)
	})
	api("POST /api/v1/tools/install/cancel", func(*http.Request) (any, error) {
		s.app.CancelToolInstall()
		return nil, nil
	})
	api("POST /api/v1/tools/yt-dlp/update", func(*http.Request) (any, error) {
		return s.app.UpdateYtDlp()
	})

	mux.Handle("/api/", http.NotFoundHandler())
	if s.webDir != "" {
		mux.Handle("/", s.frontend())
	}
	return securityHeaders(mux)
}

// maxCookiesUpload matches the limit accounts.ParseCookies applies.
const maxCookiesUpload = 16 << 20

// importCookies takes a multipart form: "site" (a link or a domain) and
// "file" (cookies.txt).
func (s *Server) importCookies(r *http.Request) (any, error) {
	if err := r.ParseMultipartForm(maxCookiesUpload); err != nil {
		return nil, badRequest("ожидается форма с полями site и file")
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return nil, badRequest("не передан файл cookies.txt")
	}
	defer file.Close()
	return s.app.ImportCookies(user, r.FormValue("site"), file)
}

// frontend serves the built Angular app. Unknown paths get index.html,
// which is never cached so a new image takes effect on reload; the
// hashed bundles can be cached for good.
func (s *Server) frontend() http.Handler {
	files := http.FileServer(http.Dir(s.webDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := path.Clean("/" + r.URL.Path)
		if name != "/" {
			if info, err := os.Stat(filepath.Join(s.webDir, filepath.FromSlash(name))); err == nil && !info.IsDir() {
				if !strings.HasSuffix(name, ".html") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(s.webDir, "index.html"))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// apiHandler turns a handler's result into JSON: the value with 200, or
// {"error": "..."} — core errors are messages meant for the user.
func apiHandler(h func(*http.Request) (any, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, err := h(r)
		if err != nil {
			status := http.StatusBadRequest
			var known httpError
			if errors.As(err, &known) {
				status = known.status
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		if result == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// Lists are arrays even when empty: the frontend iterates them.
		if v := reflect.ValueOf(result); v.Kind() == reflect.Slice && v.IsNil() {
			result = []struct{}{}
		}
		writeJSON(w, http.StatusOK, result)
	})
}

type httpError struct {
	status  int
	message string
}

func (e httpError) Error() string { return e.message }

func badRequest(message string) error {
	return httpError{status: http.StatusBadRequest, message: message}
}

func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxJSONBody))
	if err := decoder.Decode(v); err != nil {
		return badRequest("некорректный JSON в запросе")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("ytgrabber: writing response: %v", err)
	}
}
