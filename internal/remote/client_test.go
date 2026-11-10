package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// memorySecrets is the credential store in tests.
type memorySecrets struct {
	mu     sync.Mutex
	tokens map[string]string
	err    error
}

func (m *memorySecrets) Get(server string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tokens[server], m.err
}

func (m *memorySecrets) Set(server, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.tokens[server] = token
	return nil
}

func (m *memorySecrets) Delete(server string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, server)
	return m.err
}

// events records what the client sent to the window.
type events struct {
	mu   sync.Mutex
	list []string
	last map[string]any
}

func (e *events) publish(name string, data any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, name)
	e.last[name] = data
}

func (e *events) get(name string) (any, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	data, ok := e.last[name]
	return data, ok
}

// fakeServer is the part of ytgrabber-server the client talks to: one
// device token, a session, a file and the event stream.
type fakeServer struct {
	*httptest.Server
	token     string
	mu        sync.Mutex
	seen      []*http.Request
	revoked   bool
	socketsUp chan *websocket.Conn
}

func newFakeServer(t *testing.T) *fakeServer {
	f := &fakeServer{token: "device-token", socketsUp: make(chan *websocket.Conn, 4)}
	mux := http.NewServeMux()
	authorized := func(r *http.Request) bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return !f.revoked && r.Header.Get("Authorization") == "Bearer "+f.token
	}
	mux.HandleFunc("GET /api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"authenticated": authorized(r)})
	})
	mux.HandleFunc("POST /api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: f.token, HttpOnly: true})
		json.NewEncoder(w).Encode(map[string]any{"authenticated": true})
	})
	mux.HandleFunc("DELETE /api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.revoked = true
		f.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, MaxAge: -1})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, r)
		f.mu.Unlock()
		if !authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
	mux.HandleFunc("GET /media/v/file.mp4", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.ServeContent(w, r, "file.mp4", time.Time{}, strings.NewReader("0123456789"))
	})
	mux.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		f.socketsUp <- conn
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeServer) url(t *testing.T) *url.URL {
	u, err := url.Parse(f.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func newClient(t *testing.T, secrets *memorySecrets) (*Client, *events) {
	ev := &events{last: map[string]any{}}
	return New(secrets, ev.publish), ev
}

func through(c *Client, method, path string, body string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://wails.localhost"+path, strings.NewReader(body))
	for name, values := range header {
		req.Header[name] = values
	}
	rec := httptest.NewRecorder()
	c.ServeHTTP(rec, req)
	return rec
}

// TestProxySignsInAndKeepsTheTokenFromTheWindow: the token from sign-in
// goes to the credential store and into the requests, never to the window.
func TestProxySignsInAndKeepsTheTokenFromTheWindow(t *testing.T) {
	server := newFakeServer(t)
	secrets := &memorySecrets{tokens: map[string]string{}}
	c, _ := newClient(t, secrets)
	c.Use(server.url(t))

	if rec := through(c, "GET", "/api/v1/session", "", nil); !strings.Contains(rec.Body.String(), `"authenticated":false`) {
		t.Fatalf("before sign-in: %s", rec.Body)
	}
	rec := through(c, "POST", "/api/v1/session", `{"code":"x"}`, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("sign-in: %d, cookie %q", rec.Code, rec.Header().Get("Set-Cookie"))
	}
	if secrets.tokens[server.URL] != server.token {
		t.Fatalf("stored tokens: %v", secrets.tokens)
	}

	// The window's own cookie and headers don't reach the server.
	rec = through(c, "POST", "/api/v1/jobs?x=1", `{"url":"u"}`, http.Header{
		"Content-Type": {"application/json"},
		"Cookie":       {"ytg_session=from-the-window"},
		"Ngsw-Bypass":  {"1"},
	})
	if rec.Code != http.StatusOK || rec.Body.String() != `{"url":"u"}` {
		t.Fatalf("proxied call: %d %s", rec.Code, rec.Body)
	}
	seen := server.seen[len(server.seen)-1]
	if seen.Header.Get("Cookie") != "" || seen.Header.Get("Ngsw-Bypass") != "" || seen.URL.RawQuery != "x=1" ||
		seen.Header.Get("Content-Type") != "application/json" {
		t.Errorf("the server got %v %v", seen.URL, seen.Header)
	}

	// A new client for the same server starts signed in.
	again, _ := newClient(t, secrets)
	again.Use(server.url(t))
	if rec := through(again, "GET", "/api/v1/jobs", "", nil); rec.Code != http.StatusOK {
		t.Errorf("after a restart: %d", rec.Code)
	}

	rec = through(c, "DELETE", "/api/v1/session", "", nil)
	if rec.Code != http.StatusNoContent || len(secrets.tokens) != 0 {
		t.Errorf("sign-out: %d, tokens %v", rec.Code, secrets.tokens)
	}
}

func TestProxyStreamsMediaWithRanges(t *testing.T) {
	server := newFakeServer(t)
	c, _ := newClient(t, &memorySecrets{tokens: map[string]string{server.URL: server.token}})
	c.Use(server.url(t))

	rec := through(c, "GET", "/media/v/file.mp4", "", http.Header{"Range": {"bytes=2-4"}})
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "234" ||
		rec.Header().Get("Content-Range") != "bytes 2-4/10" {
		t.Errorf("range: %d %q %v", rec.Code, rec.Body, rec.Header())
	}

	dest := filepath.Join(t.TempDir(), "clip.mp4")
	if err := c.Download(context.Background(), "/media/v/file.mp4", dest); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(dest); string(data) != "0123456789" {
		t.Errorf("downloaded %q", data)
	}
	if err := c.Download(context.Background(), "/api/v1/jobs", dest); err == nil {
		t.Error("downloaded something that isn't a library file")
	}
}

func TestProxyRefusesWhatIsntTheServers(t *testing.T) {
	server := newFakeServer(t)
	c, _ := newClient(t, &memorySecrets{tokens: map[string]string{}})
	if rec := through(c, "GET", "/api/v1/jobs", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("local mode: %d", rec.Code)
	}
	c.Use(server.url(t))
	for _, path := range []string{"/api/v1/events", "/index.html", "/other/thing"} {
		if rec := through(c, "GET", path, "", nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
}

func TestProxyExplainsAnUnreachableServer(t *testing.T) {
	server := newFakeServer(t)
	address := server.url(t)
	server.Close()

	c, ev := newClient(t, &memorySecrets{tokens: map[string]string{}})
	c.Use(address)
	rec := through(c, "GET", "/api/v1/jobs", "", nil)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "VPN") {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	if status, _ := ev.get(StatusEvent); status == nil || status.(Status).Error == "" {
		t.Errorf("status: %#v", status)
	}
}

func TestSavingTheTokenCanFail(t *testing.T) {
	server := newFakeServer(t)
	secrets := &memorySecrets{tokens: map[string]string{}, err: io.ErrUnexpectedEOF}
	c, _ := newClient(t, secrets)
	c.Use(server.url(t))

	through(c, "POST", "/api/v1/session", `{}`, nil)
	// Signed in until the app quits, and told so.
	if rec := through(c, "GET", "/api/v1/jobs", "", nil); rec.Code != http.StatusOK {
		t.Errorf("after sign-in: %d", rec.Code)
	}
	if !strings.Contains(c.Status().Warning, "войти снова") {
		t.Errorf("warning %q", c.Status().Warning)
	}
}

func TestEventsAreForwardedAndReconnect(t *testing.T) {
	server := newFakeServer(t)
	c, ev := newClient(t, &memorySecrets{tokens: map[string]string{server.URL: server.token}})
	c.Use(server.url(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	conn := <-server.socketsUp
	waitFor(t, "connected", func() bool { return c.Status().Connected })
	conn.WriteMessage(websocket.TextMessage, []byte(`{"name":"download-status","data":{"id":"j"}}`))
	waitFor(t, "the event", func() bool {
		data, ok := ev.get(EventPrefix + "download-status")
		return ok && bytes.Equal(data.(json.RawMessage), []byte(`{"id":"j"}`))
	})

	// The server goes away: the window hears it, the client comes back.
	conn.Close()
	waitFor(t, "the drop", func() bool { return c.Status().Error != "" })
	c.Reconnect()
	select {
	case <-server.socketsUp:
	case <-time.After(5 * time.Second):
		t.Fatal("no reconnect")
	}
	waitFor(t, "connected again", func() bool { return c.Status().Connected })

	// Signed out from elsewhere: no point retrying with this token.
	server.mu.Lock()
	server.revoked = true
	server.mu.Unlock()
	c.Reconnect()
	waitFor(t, "signed out", func() bool { return c.Status().Error == errSignedOut.Error() })
}

func TestCheckFindsTheServer(t *testing.T) {
	server := newFakeServer(t)
	c, _ := newClient(t, &memorySecrets{tokens: map[string]string{}})

	// No scheme: https first, then plain http (the test server's).
	got, err := c.Check(context.Background(), strings.TrimPrefix(server.URL, "http://")+"/")
	if err != nil || got.String() != server.URL {
		t.Errorf("Check = %v, %v", got, err)
	}

	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>router</html>"))
	}))
	defer other.Close()
	if _, err := c.Check(context.Background(), other.URL); err == nil || !strings.Contains(err.Error(), "не сервер YT Grabber") {
		t.Errorf("another site: %v", err)
	}
	for _, raw := range []string{"", "ftp://x", "http://"} {
		if _, err := ParseServer(raw); err == nil {
			t.Errorf("ParseServer(%q) accepted", raw)
		}
	}
}

func TestConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remote.json")
	if cfg, err := LoadConfig(path); err != nil || cfg.Server != "" {
		t.Fatalf("missing file: %+v %v", cfg, err)
	}
	if err := SaveConfig(path, Config{Server: "https://grabber.example", UseServer: true}); err != nil {
		t.Fatal(err)
	}
	if cfg, err := LoadConfig(path); err != nil || cfg.Server != "https://grabber.example" || !cfg.UseServer {
		t.Errorf("loaded %+v %v", cfg, err)
	}
	os.WriteFile(path, []byte("{"), 0o600)
	if _, err := LoadConfig(path); err == nil {
		t.Error("a broken file loaded")
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
