package remote

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestSendCookiesUploadsLikeABrowser: the server gets the same form a
// browser uploads, and its refusals reach the user as it worded them.
func TestSendCookiesUploadsLikeABrowser(t *testing.T) {
	var site, file, auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/accounts" {
			http.NotFound(w, r)
			return
		}
		auth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		site = r.FormValue("site")
		if f, _, err := r.FormFile("file"); err == nil {
			data, _ := io.ReadAll(f)
			file = string(data)
		}
		w.Header().Set("Content-Type", "application/json")
		if site == "bad.example" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"в файле нет действующих cookies"}`))
			return
		}
		w.Write([]byte(`{"id":"srv-1","domain":"example.com"}`))
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)

	c, _ := newClient(t, &memorySecrets{tokens: map[string]string{server.URL: "tok"}})
	if _, err := c.SendCookies(context.Background(), "example.com", []byte("x")); err == nil {
		t.Fatal("sent cookies with no server chosen")
	}
	c.Use(u)

	reply, err := c.SendCookies(context.Background(), "example.com", []byte("# Netscape HTTP Cookie File\n"))
	if err != nil {
		t.Fatal(err)
	}
	var account struct{ ID string }
	if json.Unmarshal(reply, &account) != nil || account.ID != "srv-1" {
		t.Errorf("reply %s", reply)
	}
	if site != "example.com" || !strings.HasPrefix(file, "# Netscape") || auth != "Bearer tok" {
		t.Errorf("server got site %q, file %q, auth %q", site, file, auth)
	}

	_, err = c.SendCookies(context.Background(), "bad.example", []byte("x"))
	if err == nil || err.Error() != "в файле нет действующих cookies" {
		t.Errorf("refusal: %v", err)
	}
}

func TestSendCookiesNeedsASignIn(t *testing.T) {
	c, _ := newClient(t, &memorySecrets{tokens: map[string]string{}})
	u, _ := url.Parse("https://ytg.example")
	c.Use(u)
	if _, err := c.SendCookies(context.Background(), "example.com", []byte("x")); err == nil ||
		!strings.Contains(err.Error(), "войдите") {
		t.Errorf("err = %v", err)
	}
}
