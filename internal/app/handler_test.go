package app

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"ytgrabber/internal/remote"
)

// memorySecrets keeps tokens in memory instead of the system's store.
type memorySecrets map[string]string

func (m memorySecrets) Get(server string) (string, error) { return m[server], nil }
func (m memorySecrets) Set(server, token string) error    { m[server] = token; return nil }
func (m memorySecrets) Delete(server string) error        { delete(m, server); return nil }

func TestHandlerRoutesByMode(t *testing.T) {
	var gotAuth, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer server.Close()
	serverURL, _ := url.Parse(server.URL)

	a := &App{remote: remote.New(memorySecrets{server.URL: "secret"}, func(string, any) {})}
	handler := Handler(a)

	// Local mode: there is no API to forward.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("local /api: got %d, want 404", rec.Code)
	}
	if gotPath != "" {
		t.Fatalf("local mode reached the server: %s", gotPath)
	}

	// Server mode: the API goes to the server with the device's token.
	a.remote.Use(serverURL)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil))
	if rec.Code != http.StatusOK || gotPath != "/api/v1/jobs" || gotAuth != "Bearer secret" {
		t.Fatalf("server /api: code %d, path %q, auth %q", rec.Code, gotPath, gotAuth)
	}

	// And back: nothing reaches the server.
	a.remote.Use(nil)
	gotPath = ""
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil))
	if rec.Code != http.StatusNotFound || gotPath != "" {
		t.Fatalf("back to local: code %d, path %q", rec.Code, gotPath)
	}
}

func TestBaseName(t *testing.T) {
	for in, want := range map[string]string{
		"/data/video/a.mp4":       "a.mp4",
		`C:\Users\me\Видео\b.mkv`: "b.mkv",
		"c.mp3":                   "c.mp3",
		"/data/":                  "файл",
		"":                        "файл",
	} {
		if got := baseName(in); got != want {
			t.Errorf("baseName(%q) = %q, want %q", in, got, want)
		}
	}
}
