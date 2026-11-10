// Package server exposes the application core (package core) over HTTP,
// for browsers and phones on the same VPN: a JSON API under /api/v1, a
// WebSocket with the events the desktop gets from Wails, library files
// under /media/ and the web frontend itself.
//
// Several users share a server, each with their own queue, library and
// site accounts; every browser, phone or script they use is a device
// with a token of its own (auth.go).
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
	"ytgrabber/internal/storage"
)

// maxJSONBody bounds request bodies the API decodes; cookies.txt uploads
// have their own limit (maxCookiesUpload).
const maxJSONBody = 1 << 20

// Config is what New needs besides the core.
type Config struct {
	// SetupCode signs the first admin in while the server has no users;
	// see auth.go.
	SetupCode string
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
	// whoIs says whose data a request works with.
	whoIs func(*http.Request) core.UserID
}

func New(app *core.App, cfg Config) (*Server, error) {
	if cfg.SetupCode == "" {
		return nil, errors.New("server: empty setup code")
	}
	events := NewHub()
	return &Server{
		app:    app,
		auth:   newAuth(app, events, cfg.SetupCode),
		Events: events,
		webDir: cfg.WebDir,
		whoIs:  requestUser,
	}, nil
}

// Handler routes every request: the API table (routes.go), library files
// under /media/ and the frontend. Only the routes marked public and the
// frontend's own files are reachable without a token: the login screen
// is part of the frontend.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	protect := s.auth.require

	for _, rt := range s.routes() {
		var h http.Handler = rt.raw
		if rt.handle != nil {
			h = apiHandler(rt.handle)
		}
		if rt.admin {
			h = requireAdmin(h)
		}
		if !rt.public {
			h = protect(h)
		}
		mux.Handle(rt.method+" "+apiPrefix+rt.path, h)
	}
	mux.Handle("GET /api/v1/openapi.json", http.HandlerFunc(serveSpec))
	mux.Handle("GET "+mediaPrefix, protect(core.MediaHandler(s.app, s.userOf)))

	mux.Handle("/api/", http.NotFoundHandler())
	if s.webDir != "" {
		mux.Handle("/", s.frontend())
	}
	return securityHeaders(mux)
}

// serveEvents opens the event stream of the request's user and device.
func (s *Server) serveEvents(w http.ResponseWriter, r *http.Request) {
	who := subscriber{user: s.userOf(r)}
	if id := identityOf(r); id != nil {
		who.device = id.device.ID
	}
	s.Events.serveWS(w, r, who)
}

// userOf is the user a request acts for.
func (s *Server) userOf(r *http.Request) core.UserID {
	return s.whoIs(r)
}

// mediaPrefix serves library files (core.MediaHandler); the URLs come in
// model.VideoRecord.
const mediaPrefix = "/media/"

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
	return s.app.ImportCookies(s.userOf(r), r.FormValue("site"), file)
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
			} else if isNotFound(err) {
				status = http.StatusNotFound
			}
			writeJSON(w, status, errorResponse{Error: err.Error()})
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

func isNotFound(err error) bool {
	return core.IsNotFound(err) || errors.Is(err, storage.ErrUserNotFound) || errors.Is(err, storage.ErrDeviceNotFound)
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
