package server

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ytgrabber/internal/model"
)

// addVideo puts a file into the library of the user token signs in as.
func (f *fixture) addVideo(token string, id string, kind model.MediaKind) {
	f.t.Helper()
	session := decode[sessionResponse](f.t, f.do("GET", "/api/v1/session", token, nil))
	db, err := f.srv.app.Users()
	if err != nil {
		f.t.Fatal(err)
	}
	path := filepath.Join(f.t.TempDir(), id+".mp4")
	os.WriteFile(path, []byte("0123456789"), 0o644)
	err = db.SaveVideo(&model.VideoRecord{
		UserID: session.User.ID, ID: id, Title: "Клип <b>" + id + "</b>", MediaKind: kind,
		FilePath: path, DownloadedAt: time.Now().Format(time.RFC3339),
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) share(body string) newShareResponse {
	f.t.Helper()
	resp := f.do("POST", "/api/v1/shares", f.token, strings.NewReader(body))
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		f.t.Fatalf("sharing %s: %d %s", body, resp.StatusCode, data)
	}
	return decode[newShareResponse](f.t, resp)
}

// visit is a browser without a session: it sends only the cookies given
// and doesn't follow redirects.
func (f *fixture) visit(method, path string, form url.Values, cookies ...*http.Cookie) (*http.Response, string) {
	f.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequest(method, f.http.URL+path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, string(data)
}

func TestShareWithoutCode(t *testing.T) {
	f := newFixture(t)
	f.addVideo(f.token, "v1", model.MediaVideo)

	created := f.share(`{"videoId":"v1"}`)
	if created.Code != "" || created.Share.HasCode || !strings.HasPrefix(created.Path, "/s/") {
		t.Fatalf("created: %+v", created)
	}

	resp, page := f.visit("GET", created.Path, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, "<video") {
		t.Fatalf("page: %d %s", resp.StatusCode, page)
	}
	if strings.Contains(page, "<b>v1</b>") || !strings.Contains(page, "&lt;b&gt;v1") {
		t.Error("the title isn't escaped")
	}
	if strings.Contains(page, "download=1") {
		t.Error("download offered though not allowed")
	}
	if resp.Header.Get("X-Robots-Tag") == "" || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Errorf("headers: %v", resp.Header)
	}

	req, _ := http.NewRequest("GET", f.http.URL+created.Path+"/file.mp4", nil)
	req.Header.Set("Range", "bytes=2-4")
	media, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(media.Body)
	media.Body.Close()
	if media.StatusCode != http.StatusPartialContent || string(data) != "234" {
		t.Errorf("range: %d %q", media.StatusCode, data)
	}
	if resp, _ := f.visit("GET", created.Path+"/file.mp4?download=1", nil); resp.Header.Get("Content-Disposition") != "" {
		t.Error("downloaded though not allowed")
	}

	shares := decode[[]model.Share](t, f.do("GET", "/api/v1/shares", f.token, nil))
	if len(shares) != 1 || shares[0].Views != 1 || shares[0].VideoID != "v1" {
		t.Errorf("shares: %+v", shares)
	}

	// Revoked: gone at once, page and file alike.
	f.do("DELETE", "/api/v1/shares/"+created.Share.ID, f.token, nil)
	for _, path := range []string{created.Path, created.Path + "/file.mp4"} {
		if resp, _ := f.visit("GET", path, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s after revoking: %d", path, resp.StatusCode)
		}
	}
	if resp, _ := f.visit("GET", "/s/made-up-token", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown link: %d", resp.StatusCode)
	}
}

func TestShareWithCode(t *testing.T) {
	f := newFixture(t)
	f.addVideo(f.token, "v1", model.MediaVideo)
	created := f.share(`{"videoId":"v1","withCode":true,"allowDownload":true}`)
	if len(created.Code) != shareCodeDigits {
		t.Fatalf("code %q", created.Code)
	}

	// Without the code: the form, and no file.
	resp, page := f.visit("GET", created.Path, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, `name="code"`) || strings.Contains(page, "<video") {
		t.Fatalf("page without code: %d", resp.StatusCode)
	}
	if resp, _ := f.visit("GET", created.Path+"/file.mp4", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("file without code: %d", resp.StatusCode)
	}

	resp, _ = f.visit("POST", created.Path, url.Values{"code": {"000000x"}})
	if resp.StatusCode != http.StatusForbidden || len(resp.Cookies()) != 0 {
		t.Errorf("wrong code: %d %v", resp.StatusCode, resp.Cookies())
	}

	resp, _ = f.visit("POST", created.Path, url.Values{"code": {created.Code}})
	if resp.StatusCode != http.StatusSeeOther || len(resp.Cookies()) != 1 {
		t.Fatalf("right code: %d %v", resp.StatusCode, resp.Cookies())
	}
	cookie := resp.Cookies()[0]
	if cookie.Path != created.Path || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie: %+v", cookie)
	}
	resp, page = f.visit("GET", created.Path, nil, cookie)
	if !strings.Contains(page, "<video") || !strings.Contains(page, "download=1") {
		t.Errorf("page with the cookie: %d", resp.StatusCode)
	}
	resp, _ = f.visit("GET", created.Path+"/file.mp4?download=1", nil, cookie)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Disposition") == "" {
		t.Errorf("download with the cookie: %d", resp.StatusCode)
	}

	// The cookie opens this link only.
	other := f.share(`{"videoId":"v1","withCode":true}`)
	if resp, _ := f.visit("GET", other.Path+"/file.mp4", nil, cookie); resp.StatusCode != http.StatusNotFound {
		t.Errorf("another link with this cookie: %d", resp.StatusCode)
	}
}

func TestShareCodeLockout(t *testing.T) {
	f := newFixture(t)
	f.addVideo(f.token, "v1", model.MediaVideo)
	created := f.share(`{"videoId":"v1","withCode":true}`)

	now := time.Now()
	f.srv.auth.now = func() time.Time { return now }
	for range shareMaxFailures {
		f.visit("POST", created.Path, url.Values{"code": {"wrong"}})
	}
	if resp, _ := f.visit("POST", created.Path, url.Values{"code": {created.Code}}); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("right code while locked: %d", resp.StatusCode)
	}

	now = now.Add(shareLockout + time.Second)
	if resp, _ := f.visit("POST", created.Path, url.Values{"code": {created.Code}}); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("right code after the lockout: %d", resp.StatusCode)
	}
}

func TestShareExpiresAndDiesWithVideo(t *testing.T) {
	f := newFixture(t)
	f.addVideo(f.token, "v1", model.MediaVideo)
	f.addVideo(f.token, "v2", model.MediaVideo)
	expiring := f.share(`{"videoId":"v1"}`)
	deleted := f.share(`{"videoId":"v2"}`)

	f.do("DELETE", "/api/v1/library/v2", f.token, nil)
	if resp, _ := f.visit("GET", deleted.Path, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("link to a deleted video: %d", resp.StatusCode)
	}

	later := time.Now().Add(shareTTL + time.Minute)
	f.srv.auth.now = func() time.Time { return later }
	if resp, _ := f.visit("GET", expiring.Path, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("expired link: %d", resp.StatusCode)
	}
	if shares := decode[[]model.Share](t, f.do("GET", "/api/v1/shares", f.token, nil)); len(shares) != 0 {
		t.Errorf("expired links listed: %+v", shares)
	}
}

func TestShareRules(t *testing.T) {
	f := newFixture(t)
	f.addVideo(f.token, "audio", model.MediaAudio)
	user := f.newUser("Боб")
	f.addVideo(user, "bobs", model.MediaVideo)

	for _, req := range []struct {
		token, body string
		status      int
	}{
		{f.token, `{"videoId":"audio"}`, http.StatusBadRequest},
		{f.token, `{"videoId":"bobs"}`, http.StatusNotFound}, // not in the admin's library
		{user, `{"videoId":"bobs"}`, http.StatusForbidden},   // only admins share
	} {
		if resp := f.do("POST", "/api/v1/shares", req.token, strings.NewReader(req.body)); resp.StatusCode != req.status {
			t.Errorf("%s: %d, want %d", req.body, resp.StatusCode, req.status)
		}
	}
}
