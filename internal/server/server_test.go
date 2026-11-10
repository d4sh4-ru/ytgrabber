package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"ytgrabber/internal/core"
	"ytgrabber/internal/model"
	"ytgrabber/internal/platform"
)

const setupCode = "secret-setup-code"

type fixture struct {
	t      *testing.T
	srv    *Server
	http   *httptest.Server
	webDir string
	// token is the admin's device: the fixture sets the server up.
	token string
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

	srv, err := New(app, Config{SetupCode: setupCode, WebDir: webDir})
	if err != nil {
		t.Fatal(err)
	}
	app.Start(srv.Events, nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		app.Stop()
	})
	f := &fixture{t: t, srv: srv, http: ts, webDir: webDir}
	f.token = f.signIn(setupCode, "Админ")
	return f
}

// signIn enters code on the login screen and returns the new device's
// token: the session cookie's value.
func (f *fixture) signIn(code string, name string) string {
	f.t.Helper()
	body, _ := json.Marshal(loginRequest{Code: code, Name: name, DeviceName: "test"})
	resp := f.do("POST", "/api/v1/session", "", bytes.NewReader(body))
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		f.t.Fatalf("signing in with %q: %d %s", code, resp.StatusCode, data)
	}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == sessionCookie {
			return cookie.Value
		}
	}
	f.t.Fatal("no session cookie")
	return ""
}

// newUser invites a user and signs them in; returns their device token.
func (f *fixture) newUser(name string) string {
	f.t.Helper()
	invite := decode[codeIssued](f.t, f.do("POST", "/api/v1/users/invites", f.token, strings.NewReader(`{}`)))
	return f.signIn(invite.Code, name)
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
		resp := f.do("GET", path, f.token, nil)
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "[]" {
			t.Errorf("%s: %d %s", path, resp.StatusCode, body)
		}
	}
}

func TestJobsAPI(t *testing.T) {
	f := newFixture(t)

	resp := f.do("POST", "/api/v1/jobs", f.token, strings.NewReader(`{"url":"ftp://example.com/v","quality":"best"}`))
	if resp.StatusCode != http.StatusBadRequest || decode[map[string]string](t, resp)["error"] == "" {
		t.Errorf("bad URL: %d", resp.StatusCode)
	}
	if resp := f.do("POST", "/api/v1/jobs", f.token, strings.NewReader(`{`)); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("broken JSON: %d", resp.StatusCode)
	}
	if resp := f.do("POST", "/api/v1/jobs", f.token, strings.NewReader(`{"url":"https://example.com/v","quality":"8k"}`)); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown quality: %d", resp.StatusCode)
	}
	if resp := f.do("DELETE", "/api/v1/jobs/unknown", f.token, nil); resp.StatusCode != http.StatusNoContent {
		t.Errorf("removing an unknown job: %d", resp.StatusCode)
	}
	if resp := f.do("GET", "/api/v1/nope", f.token, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown API path: %d", resp.StatusCode)
	}
}

func TestSettingsAPI(t *testing.T) {
	f := newFixture(t)
	resp := f.do("PUT", "/api/v1/settings/max-concurrent-downloads", f.token, strings.NewReader(`{"value":3}`))
	if settings := decode[core.Settings](t, resp); settings.MaxConcurrentDownloads != 3 {
		t.Errorf("settings = %+v", settings)
	}
	if settings := decode[core.Settings](t, f.do("GET", "/api/v1/settings", f.token, nil)); settings.MaxConcurrentDownloads != 3 {
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
	req.Header.Set("Authorization", "Bearer "+f.token)
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

	if accounts := decode[[]map[string]any](t, f.do("GET", "/api/v1/accounts", f.token, nil)); len(accounts) != 1 {
		t.Errorf("accounts = %v", accounts)
	}
}

func TestSessionCookie(t *testing.T) {
	f := newFixture(t)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	link := decode[codeIssued](t, f.do("POST", "/api/v1/devices/link", f.token, nil))

	post := func(code string) *http.Response {
		resp, err := client.Post(f.http.URL+"/api/v1/session", "application/json", strings.NewReader(`{"code":"`+code+`","deviceName":"Телефон"}`))
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

	if session := decode[sessionResponse](t, get("/api/v1/session")); session.Authenticated || session.SetupRequired {
		t.Errorf("before login: %+v", session)
	}
	if resp := post("wrong"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong code: %d", resp.StatusCode)
	}
	// Codes are forgiving about case, spaces and dashes.
	resp := post(" " + strings.ToLower(strings.ReplaceAll(link.Code, "-", "")) + " ")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	cookie := resp.Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Value == f.token {
		t.Errorf("session cookie = %+v", cookie)
	}
	if session := decode[sessionResponse](t, get("/api/v1/session")); !session.Authenticated || session.User.Name != "Админ" || session.User.Role != model.RoleAdmin {
		t.Errorf("after login: %+v", session)
	}
	if resp := post(link.Code); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a link code used twice: %d", resp.StatusCode)
	}

	if resp := get("/api/v1/library"); resp.StatusCode != http.StatusOK {
		t.Errorf("API with the cookie: %d", resp.StatusCode)
	}
	// <video src> has only the cookie: an unknown file is 404, not 401.
	if resp := get("/media/unknown/file.mp4"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("media with the cookie: %d", resp.StatusCode)
	}
	devices := decode[[]model.Device](t, f.do("GET", "/api/v1/devices", f.token, nil))
	if len(devices) != 2 || devices[1].Name != "Телефон" || !devices[0].Current || devices[1].Current {
		t.Errorf("devices = %+v", devices)
	}

	// Signing out revokes the device: its token is dead even if kept.
	req, _ := http.NewRequest("DELETE", f.http.URL+"/api/v1/session", nil)
	client.Do(req)
	if resp := get("/api/v1/library"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("API after logout: %d", resp.StatusCode)
	}
	if resp := f.do("GET", "/api/v1/library", cookie.Value, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("the signed-out token: %d", resp.StatusCode)
	}
	if devices := decode[[]model.Device](t, f.do("GET", "/api/v1/devices", f.token, nil)); len(devices) != 1 {
		t.Errorf("devices after logout = %+v", devices)
	}
}

// The setup code makes the first admin and nothing after that.
func TestSetupOnlyOnce(t *testing.T) {
	f := newFixture(t)
	if session := decode[sessionResponse](t, f.do("GET", "/api/v1/session", "", nil)); session.SetupRequired {
		t.Error("setup still required")
	}
	body := strings.NewReader(`{"code":"` + setupCode + `","name":"Чужой"}`)
	if resp := f.do("POST", "/api/v1/session", "", body); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("setup code after setup: %d", resp.StatusCode)
	}
	users := decode[[]userInfo](t, f.do("GET", "/api/v1/users", f.token, nil))
	if len(users) != 1 || users[0].ID != string(core.LocalUser) || users[0].Role != model.RoleAdmin || !users[0].Current {
		t.Errorf("users = %+v", users)
	}
}

func TestLoginRateLimit(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	f.srv.auth.now = func() time.Time { return now }
	link := decode[codeIssued](t, f.do("POST", "/api/v1/devices/link", f.token, nil))

	check := func(code string) int {
		return f.do("POST", "/api/v1/session/code", "", strings.NewReader(`{"code":"`+code+`"}`)).StatusCode
	}
	for range maxFailures {
		check("guess")
	}
	if status := check(link.Code); status != http.StatusTooManyRequests {
		t.Errorf("right code while limited: %d", status)
	}
	login := f.do("POST", "/api/v1/session", "", strings.NewReader(`{"code":"`+link.Code+`"}`))
	if login.StatusCode != http.StatusTooManyRequests {
		t.Errorf("login while limited: %d", login.StatusCode)
	}
	now = now.Add(failureWindow + time.Second)
	if status := check(link.Code); status != http.StatusOK {
		t.Errorf("right code after the window: %d", status)
	}
}

// An invite makes a user with their own data; a regular user can't do
// what only the admin may.
func TestInviteAndRoles(t *testing.T) {
	f := newFixture(t)
	invite := decode[codeIssued](t, f.do("POST", "/api/v1/users/invites", f.token, strings.NewReader(`{"role":"user"}`)))

	kind := decode[codeResponse](t, f.do("POST", "/api/v1/session/code", "", strings.NewReader(`{"code":"`+invite.Code+`"}`)))
	if kind.Kind != model.CodeInvite {
		t.Errorf("code kind = %+v", kind)
	}
	// No name: the invite isn't spent.
	if resp := f.do("POST", "/api/v1/session", "", strings.NewReader(`{"code":"`+invite.Code+`"}`)); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invite without a name: %d", resp.StatusCode)
	}
	if resp := f.do("POST", "/api/v1/session", "", strings.NewReader(`{"code":"`+invite.Code+`","name":"админ"}`)); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a taken name: %d", resp.StatusCode)
	}
	bob := f.signIn(invite.Code, "Боб")

	session := decode[sessionResponse](t, f.do("GET", "/api/v1/session", bob, nil))
	if session.User == nil || session.User.Name != "Боб" || session.User.Role != model.RoleUser {
		t.Errorf("bob's session = %+v", session)
	}
	for _, req := range []struct{ method, path, body string }{
		{"GET", "/api/v1/users", ""},
		{"POST", "/api/v1/users/invites", `{}`},
		{"POST", "/api/v1/tools/install", `{}`},
		{"POST", "/api/v1/tools/yt-dlp/update", ""},
		{"PUT", "/api/v1/settings/max-concurrent-downloads", `{"value":4}`},
	} {
		var body io.Reader
		if req.body != "" {
			body = strings.NewReader(req.body)
		}
		if resp := f.do(req.method, req.path, bob, body); resp.StatusCode != http.StatusForbidden {
			t.Errorf("bob %s %s: %d, want 403", req.method, req.path, resp.StatusCode)
		}
	}
	if resp := f.do("GET", "/api/v1/tools", bob, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("bob reading the tools: %d", resp.StatusCode)
	}
	if resp := f.do("DELETE", "/api/v1/users/"+string(core.LocalUser), f.token, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("the admin removing themselves: %d", resp.StatusCode)
	}
}

// A device link made by an admin signs the user back in on a new device.
func TestAdminLinksLostUser(t *testing.T) {
	f := newFixture(t)
	bob := f.newUser("Боб")
	bobID := decode[sessionResponse](t, f.do("GET", "/api/v1/session", bob, nil)).User.ID

	link := decode[codeIssued](t, f.do("POST", "/api/v1/users/"+bobID+"/link", f.token, nil))
	kind := decode[codeResponse](t, f.do("POST", "/api/v1/session/code", "", strings.NewReader(`{"code":"`+link.Code+`"}`)))
	if kind.Kind != model.CodeLink || kind.UserName != "Боб" {
		t.Errorf("link = %+v", kind)
	}
	again := f.signIn(link.Code, "")
	if session := decode[sessionResponse](t, f.do("GET", "/api/v1/session", again, nil)); session.User.ID != bobID {
		t.Errorf("signed in as %+v", session.User)
	}
	if resp := f.do("POST", "/api/v1/users/nobody/link", f.token, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("a link for nobody: %d", resp.StatusCode)
	}
}

// An API token is a device like any other.
func TestAPIToken(t *testing.T) {
	f := newFixture(t)
	created := decode[newDeviceResponse](t, f.do("POST", "/api/v1/devices", f.token, strings.NewReader(`{"name":"backup script"}`)))
	if created.Token == "" || created.Device.Name != "backup script" {
		t.Fatalf("created = %+v", created)
	}
	if resp := f.do("GET", "/api/v1/library", created.Token, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("API token: %d", resp.StatusCode)
	}
	if resp := f.do("DELETE", "/api/v1/devices/"+created.Device.ID, f.token, nil); resp.StatusCode != http.StatusNoContent {
		t.Errorf("revoking: %d", resp.StatusCode)
	}
	if resp := f.do("GET", "/api/v1/library", created.Token, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("revoked token: %d", resp.StatusCode)
	}
	if resp := f.do("DELETE", "/api/v1/devices/"+created.Device.ID, f.token, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("revoking twice: %d", resp.StatusCode)
	}
}

func TestQR(t *testing.T) {
	f := newFixture(t)
	resp := f.do("GET", "/api/v1/qr?text="+url.QueryEscape("https://ytg.example/?code=ABCDE-FGHJK"), f.token, nil)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/svg+xml" || !bytes.HasPrefix(body, []byte("<svg")) {
		t.Errorf("qr: %d %s %.60s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	if resp := f.do("GET", "/api/v1/qr", f.token, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("qr without text: %d", resp.StatusCode)
	}
}

func TestEventsWebSocket(t *testing.T) {
	f := newFixture(t)
	url := "ws" + strings.TrimPrefix(f.http.URL, "http") + "/api/v1/events"

	// Another site's page can't open the socket with the user's cookie.
	header := http.Header{"Authorization": {"Bearer " + f.token}, "Origin": {"https://evil.example"}}
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
	client := hub.subscribe(subscriber{user: core.LocalUser})
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

// A user's events reach only that user's sockets; the rest reach all.
func TestEventsGoToTheirUser(t *testing.T) {
	hub := NewHub()
	aliceClient, bobClient := hub.subscribe(subscriber{user: "alice"}), hub.subscribe(subscriber{user: "bob"})
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

// Another user's job, video or account is 404, as if it didn't exist.
func TestOtherUsersRecordsAreNotFound(t *testing.T) {
	f := newFixture(t)
	alice, bob := f.newUser("Алиса"), f.newUser("Боб")

	// The fake yt-dlp fails at once, so the job stays as an error.
	id := decode[string](t, f.do("POST", "/api/v1/jobs", alice, strings.NewReader(`{"url":"https://example.com/v","quality":"best"}`)))
	var jobs []map[string]any
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		jobs = decode[[]map[string]any](t, f.do("GET", "/api/v1/jobs", alice, nil))
		if len(jobs) == 1 && jobs[0]["status"] == "error" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(jobs) != 1 || jobs[0]["id"] != id || jobs[0]["status"] != "error" {
		t.Fatalf("alice's jobs: %v", jobs)
	}

	for _, token := range []string{bob, f.token} {
		if jobs := decode[[]map[string]any](t, f.do("GET", "/api/v1/jobs", token, nil)); len(jobs) != 0 {
			t.Errorf("someone else sees alice's jobs: %v", jobs)
		}
	}
	for _, req := range []struct{ method, path, body string }{
		{"POST", "/api/v1/jobs/" + id + "/retry", `{}`},
		{"DELETE", "/api/v1/library/" + id, ""},
		{"PUT", "/api/v1/library/" + id + "/position", `{"seconds":10}`},
		{"POST", "/api/v1/accounts/" + id + "/check", ""},
		{"DELETE", "/api/v1/accounts/" + id, ""},
		{"DELETE", "/api/v1/devices/" + id, ""},
		{"GET", "/media/" + id + "/file.mp4", ""},
	} {
		var body io.Reader
		if req.body != "" {
			body = strings.NewReader(req.body)
		}
		if resp := f.do(req.method, req.path, bob, body); resp.StatusCode != http.StatusNotFound {
			t.Errorf("bob %s %s: %d, want 404", req.method, req.path, resp.StatusCode)
		}
	}
	f.do("DELETE", "/api/v1/jobs/"+id, bob, nil)
	if jobs := decode[[]map[string]any](t, f.do("GET", "/api/v1/jobs", alice, nil)); len(jobs) != 1 {
		t.Errorf("bob removed alice's job: %v", jobs)
	}

	// Removing alice takes her data and devices with her.
	users := decode[[]userInfo](t, f.do("GET", "/api/v1/users", f.token, nil))
	aliceID := ""
	for _, user := range users {
		if user.Name == "Алиса" {
			aliceID = user.ID
		}
	}
	if resp := f.do("DELETE", "/api/v1/users/"+aliceID, f.token, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("removing alice: %d", resp.StatusCode)
	}
	if resp := f.do("GET", "/api/v1/jobs", alice, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("alice's device after her removal: %d", resp.StatusCode)
	}
	if jobs, _ := f.srv.app.GetJobs(core.UserID(aliceID)); len(jobs) != 0 {
		t.Errorf("alice's jobs outlived her: %v", jobs)
	}
	if resp := f.do("DELETE", "/api/v1/users/"+aliceID, f.token, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("removing alice twice: %d", resp.StatusCode)
	}
}

// Revoking a device closes its event stream at once.
func TestRevokeClosesEvents(t *testing.T) {
	f := newFixture(t)
	created := decode[newDeviceResponse](t, f.do("POST", "/api/v1/devices", f.token, strings.NewReader(`{"name":"tablet"}`)))
	wsURL := "ws" + strings.TrimPrefix(f.http.URL, "http") + "/api/v1/events"
	header := http.Header{"Authorization": {"Bearer " + created.Token}, "Origin": {f.http.URL}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	closed := make(chan error, 1)
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				closed <- err
				return
			}
		}
	}()
	// The socket is registered right after the upgrade; revoke once it is.
	deadline := time.Now().Add(5 * time.Second)
	for f.srv.Events.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if resp := f.do("DELETE", "/api/v1/devices/"+created.Device.ID, f.token, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoking: %d", resp.StatusCode)
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the revoked device's socket stayed open")
	}
}
