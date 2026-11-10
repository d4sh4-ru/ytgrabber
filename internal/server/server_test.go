package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"ytgrabber/internal/core"
	"ytgrabber/internal/platform"
)

const testToken = "secret-token"

type fixture struct {
	t      *testing.T
	srv    *Server
	http   *httptest.Server
	webDir string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	installFakeTools(t, filepath.Join(dir, "data"))
	app := core.New(platform.Paths{
		DataDir:             filepath.Join(dir, "data"),
		DefaultDownloadsDir: filepath.Join(dir, "media"),
	}, core.WithoutSelfUpdate())

	webDir := filepath.Join(dir, "web")
	os.MkdirAll(filepath.Join(webDir, "assets"), 0o755)
	os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<app-root></app-root>"), 0o644)
	os.WriteFile(filepath.Join(webDir, "assets", "main-abc.js"), []byte("console.log(1)"), 0o644)

	srv, err := New(app, Config{Token: testToken, WebDir: webDir})
	if err != nil {
		t.Fatal(err)
	}
	app.Start(srv.Events, nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		app.Stop()
	})
	return &fixture{t: t, srv: srv, http: ts, webDir: webDir}
}

// installFakeTools puts stand-ins for yt-dlp and ffmpeg into the app's
// tools folder, so a download can be queued whatever is installed on the
// machine. They exit with an error at once: a queued job fails without
// touching the network.
func installFakeTools(t *testing.T, dataDir string) {
	t.Helper()
	toolsDir := platform.Paths{DataDir: dataDir}.ToolsDir()
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"yt-dlp", "ffmpeg"} {
		path := filepath.Join(toolsDir, platform.ExecutableName(name))
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// do sends a request with the token unless token is "".
func (f *fixture) do(method, path, token string, body io.Reader) *http.Response {
	f.t.Helper()
	req, _ := http.NewRequest(method, f.http.URL+path, body)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := f.http.Client().Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decoding %s: %v", resp.Request.URL.Path, err)
	}
	return v
}

func TestAPIRequiresToken(t *testing.T) {
	f := newFixture(t)

	if resp := f.do("GET", "/api/v1/health", "", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("health: %d", resp.StatusCode)
	}
	for _, path := range []string{"/api/v1/jobs", "/api/v1/library", "/api/v1/accounts", "/api/v1/tools", "/media/x/file.mp4", "/api/v1/events"} {
		if resp := f.do("GET", path, "", nil); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without token: %d", path, resp.StatusCode)
		}
		if resp := f.do("GET", path, "wrong", nil); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s with a wrong token: %d", path, resp.StatusCode)
		}
	}
}

func TestEmptyListsAreArrays(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{"/api/v1/jobs", "/api/v1/library", "/api/v1/accounts"} {
		resp := f.do("GET", path, testToken, nil)
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "[]" {
			t.Errorf("%s: %d %s", path, resp.StatusCode, body)
		}
	}
}

func TestJobsAPI(t *testing.T) {
	f := newFixture(t)

	resp := f.do("POST", "/api/v1/jobs", testToken, strings.NewReader(`{"url":"ftp://example.com/v","quality":"best"}`))
	if resp.StatusCode != http.StatusBadRequest || decode[map[string]string](t, resp)["error"] == "" {
		t.Errorf("bad URL: %d", resp.StatusCode)
	}
	if resp := f.do("POST", "/api/v1/jobs", testToken, strings.NewReader(`{`)); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("broken JSON: %d", resp.StatusCode)
	}
	if resp := f.do("POST", "/api/v1/jobs", testToken, strings.NewReader(`{"url":"https://example.com/v","quality":"8k"}`)); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown quality: %d", resp.StatusCode)
	}
	if resp := f.do("DELETE", "/api/v1/jobs/unknown", testToken, nil); resp.StatusCode != http.StatusNoContent {
		t.Errorf("removing an unknown job: %d", resp.StatusCode)
	}
	if resp := f.do("GET", "/api/v1/nope", testToken, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown API path: %d", resp.StatusCode)
	}
}

func TestSettingsAPI(t *testing.T) {
	f := newFixture(t)
	resp := f.do("PUT", "/api/v1/settings/max-concurrent-downloads", testToken, strings.NewReader(`{"value":3}`))
	if settings := decode[core.Settings](t, resp); settings.MaxConcurrentDownloads != 3 {
		t.Errorf("settings = %+v", settings)
	}
	if settings := decode[core.Settings](t, f.do("GET", "/api/v1/settings", testToken, nil)); settings.MaxConcurrentDownloads != 3 {
		t.Errorf("setting not kept: %+v", settings)
	}
}

func TestImportCookiesUpload(t *testing.T) {
	f := newFixture(t)

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	form.WriteField("site", "youtube.com")
	file, _ := form.CreateFormFile("file", "cookies.txt")
	file.Write([]byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tSAPISID\tx\n"))
	form.Close()

	req, _ := http.NewRequest("POST", f.http.URL+"/api/v1/accounts", &body)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := f.http.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	account := decode[map[string]any](t, resp)
	if resp.StatusCode != http.StatusOK || account["domain"] != "youtube.com" || account["loggedIn"] != true {
		t.Errorf("upload: %d %v", resp.StatusCode, account)
	}

	if accounts := decode[[]map[string]any](t, f.do("GET", "/api/v1/accounts", testToken, nil)); len(accounts) != 1 {
		t.Errorf("accounts = %v", accounts)
	}
}

func TestSessionCookie(t *testing.T) {
	f := newFixture(t)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	post := func(token string) *http.Response {
		resp, err := client.Post(f.http.URL+"/api/v1/session", "application/json", strings.NewReader(`{"token":"`+token+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}
	get := func(path string) *http.Response {
		resp, err := client.Get(f.http.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}

	if session := decode[map[string]bool](t, get("/api/v1/session")); session["authenticated"] {
		t.Error("authenticated before login")
	}
	if resp := post("wrong"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: %d", resp.StatusCode)
	}
	resp := post(testToken)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	cookie := resp.Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || strings.Contains(cookie.Value, testToken) {
		t.Errorf("session cookie = %+v", cookie)
	}

	if resp := get("/api/v1/library"); resp.StatusCode != http.StatusOK {
		t.Errorf("API with the cookie: %d", resp.StatusCode)
	}
	// <video src> has only the cookie: an unknown file is 404, not 401.
	if resp := get("/media/unknown/file.mp4"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("media with the cookie: %d", resp.StatusCode)
	}

	req, _ := http.NewRequest("DELETE", f.http.URL+"/api/v1/session", nil)
	client.Do(req)
	if resp := get("/api/v1/library"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("API after logout: %d", resp.StatusCode)
	}
}

func TestLoginRateLimit(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	f.srv.auth.now = func() time.Time { return now }

	login := func(token string) int {
		return f.do("POST", "/api/v1/session", "", strings.NewReader(`{"token":"`+token+`"}`)).StatusCode
	}
	for range maxFailures {
		login("guess")
	}
	if status := login(testToken); status != http.StatusTooManyRequests {
		t.Errorf("right token while limited: %d", status)
	}
	now = now.Add(failureWindow + time.Second)
	if status := login(testToken); status != http.StatusOK {
		t.Errorf("right token after the window: %d", status)
	}
}

func TestEventsWebSocket(t *testing.T) {
	f := newFixture(t)
	url := "ws" + strings.TrimPrefix(f.http.URL, "http") + "/api/v1/events"

	// Another site's page can't open the socket with the user's cookie.
	header := http.Header{"Authorization": {"Bearer " + testToken}, "Origin": {"https://evil.example"}}
	if _, resp, err := websocket.DefaultDialer.Dial(url, header); err == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign origin accepted: %v", err)
	}

	header.Set("Origin", f.http.URL)
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// The hub registers the client right after the upgrade; publish until
	// the event arrives.
	received := make(chan Event, 1)
	go func() {
		var event Event
		if conn.ReadJSON(&event) == nil {
			received <- event
		}
	}()
	deadline := time.After(5 * time.Second)
	for {
		f.srv.Events.Publish("download-status", map[string]string{"id": "j1"})
		select {
		case event := <-received:
			if event.Name != "download-status" || event.Data.(map[string]any)["id"] != "j1" {
				t.Errorf("event = %+v", event)
			}
			return
		case <-deadline:
			t.Fatal("event not delivered")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestSlowClientIsDropped(t *testing.T) {
	hub := NewHub()
	client := hub.subscribe(core.LocalUser)
	for range clientBuffer + 1 {
		hub.Publish("download-progress", 1)
	}
	for range client {
	}
	// Closed rather than blocking the publisher; unsubscribing again is safe.
	hub.unsubscribe(client)
}

func TestFrontend(t *testing.T) {
	f := newFixture(t)

	for _, path := range []string{"/", "/library", "/settings/tools"} {
		resp := f.do("GET", path, "", nil)
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK || string(body) != "<app-root></app-root>" || resp.Header.Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: %d %q %q", path, resp.StatusCode, body, resp.Header.Get("Cache-Control"))
		}
	}
	resp := f.do("GET", "/assets/main-abc.js", "", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("bundle: %d %q", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}
	if resp := f.do("GET", "/../data/token", "", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("path traversal must fall back to index.html, got %d", resp.StatusCode)
	}
}

// asUser makes every request act for the user named in X-Test-User, as
// the session will once there are users (issue #14).
func (f *fixture) asUser() {
	f.srv.whoIs = func(r *http.Request) core.UserID {
		if user := r.Header.Get("X-Test-User"); user != "" {
			return core.UserID(user)
		}
		return core.LocalUser
	}
}

func (f *fixture) doAs(user core.UserID, method, path string, body io.Reader) *http.Response {
	f.t.Helper()
	req, _ := http.NewRequest(method, f.http.URL+path, body)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Test-User", string(user))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := f.http.Client().Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// Another user's job, video or account is 404, as if it didn't exist.
func TestOtherUsersRecordsAreNotFound(t *testing.T) {
	f := newFixture(t)
	f.asUser()

	// The fake yt-dlp fails at once, so the job stays as an error.
	id := decode[string](t, f.doAs("alice", "POST", "/api/v1/jobs", strings.NewReader(`{"url":"https://example.com/v","quality":"best"}`)))
	var jobs []map[string]any
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		jobs = decode[[]map[string]any](t, f.doAs("alice", "GET", "/api/v1/jobs", nil))
		if len(jobs) == 1 && jobs[0]["status"] == "error" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(jobs) != 1 || jobs[0]["id"] != id || jobs[0]["status"] != "error" {
		t.Fatalf("alice's jobs: %v", jobs)
	}

	if jobs := decode[[]map[string]any](t, f.doAs("bob", "GET", "/api/v1/jobs", nil)); len(jobs) != 0 {
		t.Errorf("bob sees alice's jobs: %v", jobs)
	}
	for _, req := range []struct{ method, path, body string }{
		{"POST", "/api/v1/jobs/" + id + "/retry", `{}`},
		{"DELETE", "/api/v1/library/" + id, ""},
		{"PUT", "/api/v1/library/" + id + "/position", `{"seconds":10}`},
		{"POST", "/api/v1/accounts/" + id + "/check", ""},
		{"DELETE", "/api/v1/accounts/" + id, ""},
		{"GET", "/media/" + id + "/file.mp4", ""},
	} {
		var body io.Reader
		if req.body != "" {
			body = strings.NewReader(req.body)
		}
		if resp := f.doAs("bob", req.method, req.path, body); resp.StatusCode != http.StatusNotFound {
			t.Errorf("bob %s %s: %d, want 404", req.method, req.path, resp.StatusCode)
		}
	}
	f.doAs("bob", "DELETE", "/api/v1/jobs/"+id, nil)
	if jobs := decode[[]map[string]any](t, f.doAs("alice", "GET", "/api/v1/jobs", nil)); len(jobs) != 1 {
		t.Errorf("bob removed alice's job: %v", jobs)
	}
}

// A user's events reach only that user's sockets; the rest reach all.
func TestEventsGoToTheirUser(t *testing.T) {
	hub := NewHub()
	aliceClient, bobClient := hub.subscribe("alice"), hub.subscribe("bob")
	hub.PublishTo("alice", "library-changed")
	hub.Publish("tool-install-progress", 1)

	received := func(client chan []byte) []string {
		var names []string
		for {
			select {
			case message := <-client:
				var event Event
				json.Unmarshal(message, &event)
				names = append(names, event.Name)
			default:
				return names
			}
		}
	}
	if got := received(aliceClient); strings.Join(got, ",") != "library-changed,tool-install-progress" {
		t.Errorf("alice got %v", got)
	}
	if got := received(bobClient); strings.Join(got, ",") != "tool-install-progress" {
		t.Errorf("bob got %v", got)
	}
}
