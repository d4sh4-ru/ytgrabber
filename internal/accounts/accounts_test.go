package accounts

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

var now = time.Unix(1_800_000_000, 0)

func names(cookies []Cookie) []string {
	var result []string
	for _, c := range cookies {
		result = append(result, c.Domain+"/"+c.Name+"="+c.Value)
	}
	slices.Sort(result)
	return result
}

func TestParseCookies(t *testing.T) {
	file := "\xef\xbb\xbf# Netscape HTTP Cookie File\r\n" +
		"# comment\r\n" +
		".youtube.com\tTRUE\t/\tTRUE\t1900000000\tSAPISID\tabc\r\n" +
		"#HttpOnly_.youtube.com\tTRUE\t/\tTRUE\t1900000000.5\t__Secure-3PSID\tdef\r\n" +
		"www.youtube.com\tFALSE\t/\tFALSE\t0\tPREF\r\n" + // value without trailing tab
		"broken line\n" +
		"\n"

	cookies, err := ParseCookies(strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if len(cookies) != 3 {
		t.Fatalf("cookies = %+v", cookies)
	}
	if c := cookies[1]; !c.HTTPOnly || c.Domain != ".youtube.com" || c.Expires != 1900000000 || c.Value != "def" {
		t.Errorf("HttpOnly cookie = %+v", c)
	}
	if c := cookies[2]; c.Value != "" || c.Expires != 0 || c.IncludeSubdomains || c.Secure {
		t.Errorf("session cookie = %+v", c)
	}

	again, err := ParseCookies(strings.NewReader(string(FormatCookies(cookies))))
	if err != nil || !slices.Equal(again, cookies) {
		t.Errorf("round trip = %+v, %v", again, err)
	}
}

func TestParseCookiesRejectsOtherFiles(t *testing.T) {
	for _, content := range []string{"", "hello world\n", "{\"cookies\": []}"} {
		if _, err := ParseCookies(strings.NewReader(content)); err != ErrNotCookiesFile {
			t.Errorf("ParseCookies(%q) error = %v", content, err)
		}
	}
	// A header alone is a valid, empty export.
	if cookies, err := ParseCookies(strings.NewReader("# Netscape HTTP Cookie File\n")); err != nil || len(cookies) != 0 {
		t.Errorf("empty export = %+v, %v", cookies, err)
	}
}

func TestSiteOf(t *testing.T) {
	cases := map[string]string{
		"www.youtube.com":   "youtube.com",
		"music.youtube.com": "youtube.com",
		"youtu.be":          "youtube.com",
		".youtube.com":      "youtube.com",
		"WWW.Instagram.COM": "instagram.com",
		"twitter.com":       "x.com",
		"news.bbc.co.uk":    "bbc.co.uk",
		"user.github.io":    "user.github.io",
	}
	for host, want := range cases {
		if got, err := SiteOf(host); err != nil || got != want {
			t.Errorf("SiteOf(%q) = %q, %v; want %q", host, got, err, want)
		}
	}
	for _, host := range []string{"", "localhost", "127.0.0.1", "co.uk", "::1"} {
		if got, err := SiteOf(host); err == nil {
			t.Errorf("SiteOf(%q) = %q, want an error", host, got)
		}
	}

	for input, want := range map[string]string{
		"instagram.com":                        "instagram.com",
		"https://www.instagram.com/p/abc/":     "instagram.com",
		" https://youtu.be/xyz?t=1 ":           "youtube.com",
		"m.vk.com/video-1_2":                   "vk.com",
		"https://www.youtube.com:443/watch?v=": "youtube.com",
	} {
		if got, err := SiteFromInput(input); err != nil || got != want {
			t.Errorf("SiteFromInput(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
}

// TestFilterCookies: whatever enters a snapshot is limited to the site —
// a cookies.txt export or yt-dlp's jar usually holds every site's cookies.
func TestFilterCookies(t *testing.T) {
	cookies := []Cookie{
		{Domain: ".youtube.com", Path: "/", Name: "SAPISID", Value: "old", Expires: 1900000000},
		{Domain: "accounts.youtube.com", Path: "/", Name: "LSID", Value: "x"},
		{Domain: ".youtu.be", Path: "/", Name: "alias", Value: "x"},
		{Domain: ".google.com", Path: "/", Name: "SID", Value: "foreign"},
		{Domain: "youtube.com.evil.com", Path: "/", Name: "fake", Value: "foreign"},
		{Domain: "notyoutube.com", Path: "/", Name: "fake", Value: "foreign"},
		{Domain: ".youtube.com", Path: "/", Name: "gone", Value: "x", Expires: now.Unix() - 1},
		{Domain: ".youtube.com", Path: "/", Name: "bad", Value: "a\nb"},
		{Domain: ".youtube.com", Path: "/", Name: "SAPISID", Value: "new", Expires: 1900000000},
	}

	got := names(FilterCookies(cookies, "youtube.com", now))
	want := []string{".youtu.be/alias=x", ".youtube.com/SAPISID=new", "accounts.youtube.com/LSID=x"}
	if !slices.Equal(got, want) {
		t.Errorf("filtered = %q, want %q", got, want)
	}
}

func TestLoginState(t *testing.T) {
	logged := []Cookie{{Domain: ".instagram.com", Name: "sessionid", Value: "s"}}
	anonymous := []Cookie{{Domain: ".instagram.com", Name: "csrftoken", Value: "c"}}

	if known, in := LoginState("instagram.com", logged); !known || !in {
		t.Errorf("logged in = %v, %v", known, in)
	}
	if known, in := LoginState("instagram.com", anonymous); !known || in {
		t.Errorf("anonymous = %v, %v", known, in)
	}
	if known, _ := LoginState("example.com", logged); known {
		t.Error("unknown site reported as known")
	}
}

// TestMergeCookies covers two downloads running at once with the same
// snapshot: each one's refresh must survive the other's write-back.
func TestMergeCookies(t *testing.T) {
	base := []Cookie{
		{Domain: ".x.com", Path: "/", Name: "auth_token", Value: "1"},
		{Domain: ".x.com", Path: "/", Name: "ct0", Value: "1"},
		{Domain: ".x.com", Path: "/", Name: "tmp", Value: "1"},
	}
	// Download A rotated ct0 and the site deleted tmp.
	afterA := []Cookie{
		{Domain: ".x.com", Path: "/", Name: "auth_token", Value: "1"},
		{Domain: ".x.com", Path: "/", Name: "ct0", Value: "A"},
	}
	current := MergeCookies(base, base, afterA)
	if got := names(current); !slices.Equal(got, []string{".x.com/auth_token=1", ".x.com/ct0=A"}) {
		t.Fatalf("after A = %q", got)
	}

	// Download B started from base too, set a new cookie and still has tmp.
	afterB := append(slices.Clone(base), Cookie{Domain: ".x.com", Path: "/", Name: "guest", Value: "B"})
	merged := MergeCookies(base, current, afterB)
	// B left ct0 and tmp as they were, so A's rotation and deletion stand.
	want := []string{".x.com/auth_token=1", ".x.com/ct0=A", ".x.com/guest=B"}
	if got := names(merged); !slices.Equal(got, want) {
		t.Errorf("after B = %q, want %q", got, want)
	}

	// yt-dlp writes HttpOnly cookies back without the flag; that is not a
	// change that should override a parallel refresh.
	httpOnlyBase := []Cookie{{Domain: ".x.com", Path: "/", Name: "auth_token", Value: "1", HTTPOnly: true}}
	refreshed := []Cookie{{Domain: ".x.com", Path: "/", Name: "auth_token", Value: "2", HTTPOnly: true}}
	writtenBack := []Cookie{{Domain: ".x.com", Path: "/", Name: "auth_token", Value: "1"}}
	if got := MergeCookies(httpOnlyBase, refreshed, writtenBack); len(got) != 1 || got[0].Value != "2" || !got[0].HTTPOnly {
		t.Errorf("flag-only difference overrode a refresh: %+v", got)
	}

	// A cookie added by another download and unknown to this one is kept.
	added := append(slices.Clone(base), Cookie{Domain: ".x.com", Path: "/", Name: "other", Value: "C"})
	if got := names(MergeCookies(base, added, base)); !slices.Contains(got, ".x.com/other=C") {
		t.Errorf("concurrent addition lost: %q", got)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "accounts")
	store := NewStore(dir)
	cookies := []Cookie{{Domain: ".vk.com", Path: "/", Name: "remixsid", Value: "secret-value", Secure: true, HTTPOnly: true}}

	if err := store.Save("a1", "vk.com", cookies); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "a1.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-value") || strings.Contains(string(raw), "remixsid") {
		t.Error("snapshot is not encrypted")
	}

	// A fresh store (next app start) reads the key from disk.
	loaded, err := NewStore(dir).Load("a1", "vk.com")
	if err != nil || !slices.Equal(loaded, cookies) {
		t.Fatalf("loaded = %+v, %v", loaded, err)
	}

	if runtime.GOOS != "windows" {
		for path, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, "key"): 0o600, filepath.Join(dir, "a1.enc"): 0o600} {
			if info, err := os.Stat(path); err != nil || info.Mode().Perm() != want {
				t.Errorf("%s mode = %v, want %v (%v)", path, info.Mode().Perm(), want, err)
			}
		}
	}

	if err := store.Delete("a1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("a1", "vk.com"); !os.IsNotExist(err) {
		t.Errorf("deleted snapshot load error = %v", err)
	}
	if err := store.Delete("a1"); err != nil {
		t.Errorf("deleting twice: %v", err)
	}
}

// TestStoreBindsSite: a snapshot copied over another account's file must
// not decrypt as that account.
func TestStoreBindsSite(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	store.Save("insta", "instagram.com", []Cookie{{Domain: ".instagram.com", Path: "/", Name: "sessionid", Value: "s"}})
	store.Save("vk", "vk.com", []Cookie{{Domain: ".vk.com", Path: "/", Name: "remixsid", Value: "v"}})

	data, _ := os.ReadFile(filepath.Join(dir, "insta.enc"))
	os.WriteFile(filepath.Join(dir, "vk.enc"), data, 0o600)
	if _, err := store.Load("vk", "vk.com"); err != errCorruptSnapshot {
		t.Errorf("swapped snapshot error = %v", err)
	}

	os.WriteFile(filepath.Join(dir, "insta.enc"), []byte("YTGC1garbage"), 0o600)
	if _, err := store.Load("insta", "instagram.com"); err != errCorruptSnapshot {
		t.Errorf("corrupt snapshot error = %v", err)
	}
}

func TestStoreRejectsDamagedKey(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "key"), []byte("short"), 0o600)
	if err := NewStore(dir).Save("a", "vk.com", nil); err == nil {
		t.Error("saved with a damaged key")
	}
}
