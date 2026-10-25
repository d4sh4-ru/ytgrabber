package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVersionOrder(t *testing.T) {
	// Each version is newer than the one before it (semver.org §11).
	ordered := []string{"0.9.9", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta",
		"1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "v1.0.1", "1.2.0", "1.10.0", "2.0.0"}
	for i := 1; i < len(ordered); i++ {
		older, err1 := ParseVersion(ordered[i-1])
		newer, err2 := ParseVersion(ordered[i])
		if err1 != nil || err2 != nil {
			t.Fatalf("parse: %v %v", err1, err2)
		}
		if newer.Compare(older) != 1 || older.Compare(newer) != -1 {
			t.Errorf("%s should be newer than %s", ordered[i], ordered[i-1])
		}
	}
	if v, _ := ParseVersion("v1.2.3+build.5"); v.String() != "1.2.3" || v.Compare(Version{1, 2, 3, nil}) != 0 {
		t.Errorf("build metadata: %v", v)
	}
	for _, bad := range []string{"dev", "1.2", "1.2.3.4", "01.2.3", "1.2.3-", "a.b.c", ""} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("ParseVersion(%q) accepted", bad)
		}
	}
}

func TestAssetName(t *testing.T) {
	v, _ := ParseVersion("1.3.0-rc.1")
	cases := []struct {
		goos, goarch string
		kind         InstallKind
		want         string
	}{
		{"darwin", "arm64", InstallMacApp, "ytgrabber-1.3.0-rc.1-macos-arm64.zip"},
		{"darwin", "amd64", InstallMacApp, ""},
		{"windows", "amd64", InstallWindowsSetup, "ytgrabber-1.3.0-rc.1-windows-amd64-setup.exe"},
		{"windows", "arm64", InstallWindowsPortable, "ytgrabber-1.3.0-rc.1-windows-arm64-portable.exe"},
		{"linux", "arm64", InstallLinuxBinary, "ytgrabber-1.3.0-rc.1-linux-arm64.tar.gz"},
		{"freebsd", "amd64", InstallManual, ""},
	}
	for _, c := range cases {
		if got := AssetName(v, c.goos, c.goarch, c.kind); got != c.want {
			t.Errorf("AssetName(%s/%s, %s) = %q, want %q", c.goos, c.goarch, c.kind, got, c.want)
		}
	}
}

// fakeGitHub serves a release list and the release files.
type fakeGitHub struct {
	server   *httptest.Server
	releases []map[string]any
	files    map[string][]byte
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{files: map[string][]byte{}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases" {
			json.NewEncoder(w).Encode(f.releases)
			return
		}
		data, ok := f.files[strings.TrimPrefix(r.URL.Path, "/files/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGitHub) client() *Client {
	return &Client{HTTP: f.server.Client(), UserAgent: "test", APIURL: f.server.URL + "/releases"}
}

// addRelease publishes tag with the given files; names starting with
// "!" are listed but missing on the server.
func (f *fakeGitHub) addRelease(tag string, prerelease bool, draft bool, files map[string][]byte) {
	var assets []map[string]any
	for name, data := range files {
		f.files[tag+"/"+name] = data
		assets = append(assets, map[string]any{
			"name": name, "browser_download_url": f.server.URL + "/files/" + tag + "/" + name, "size": len(data),
		})
	}
	f.releases = append(f.releases, map[string]any{
		"tag_name": tag, "name": tag, "body": "## Установка\n...\n## What's Changed\n* feat: всё", "html_url": "https://example/" + tag,
		"draft": draft, "prerelease": prerelease, "assets": assets,
	})
}

func TestLatest(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.addRelease("v1.1.0", false, false, nil)
	gh.addRelease("v1.3.0-rc.1", true, false, nil)
	gh.addRelease("v1.2.0", false, false, nil)
	gh.addRelease("v9.0.0", false, true, nil) // draft
	gh.addRelease("nightly", false, false, nil)

	latest, err := gh.client().Latest(context.Background(), false)
	if err != nil || latest == nil || latest.Version.String() != "1.2.0" {
		t.Fatalf("latest = %+v, %v", latest, err)
	}
	if notes := ReleaseNotes(latest.Notes); notes != "## What's Changed\n* feat: всё" {
		t.Errorf("notes = %q", notes)
	}
	pre, _ := gh.client().Latest(context.Background(), true)
	if pre == nil || pre.Version.String() != "1.3.0-rc.1" {
		t.Errorf("latest with pre-releases = %+v", pre)
	}
}

func signedRelease(t *testing.T, gh *fakeGitHub, tag string, files map[string][]byte, sign func([]byte) []byte) {
	t.Helper()
	var sums bytes.Buffer
	for name, data := range files {
		sum := sha256.Sum256(data)
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	all := map[string][]byte{"SHA256SUMS.txt": sums.Bytes()}
	if sign != nil {
		all["SHA256SUMS.txt.sig"] = []byte(base64.StdEncoding.EncodeToString(sign(sums.Bytes())) + "\n")
	}
	for name, data := range files {
		all[name] = data
	}
	gh.addRelease(tag, false, false, all)
}

func TestDownloadVerifiesSignatureAndChecksum(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	_, otherPrivate, _ := ed25519.GenerateKey(rand.Reader)
	payload := []byte("new build")
	asset := "ytgrabber-2.0.0-linux-amd64.tar.gz"

	gh := newFakeGitHub(t)
	signedRelease(t, gh, "v2.0.0", map[string][]byte{asset: payload}, func(d []byte) []byte { return ed25519.Sign(private, d) })
	client := gh.client()
	release, _ := client.Latest(context.Background(), false)

	sums, err := client.VerifiedChecksums(context.Background(), release, public)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := release.Asset(asset)
	path := filepath.Join(t.TempDir(), asset)
	var progress int64
	if err := client.Download(context.Background(), a, sums[asset], path, func(done, _ int64) { progress = done }); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !bytes.Equal(data, payload) || progress != int64(len(payload)) {
		t.Errorf("downloaded %q, progress %d", data, progress)
	}

	// A checksum that doesn't match the file is refused and the file removed.
	if err := client.Download(context.Background(), a, strings.Repeat("0", 64), path, nil); err == nil {
		t.Error("accepted a file with the wrong checksum")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file with the wrong checksum left behind")
	}

	// A checksum list signed by another key is refused.
	forged := newFakeGitHub(t)
	signedRelease(t, forged, "v2.0.0", map[string][]byte{asset: payload}, func(d []byte) []byte { return ed25519.Sign(otherPrivate, d) })
	forgedRelease, _ := forged.client().Latest(context.Background(), false)
	if _, err := forged.client().VerifiedChecksums(context.Background(), forgedRelease, public); err == nil {
		t.Error("accepted a checksum list signed by another key")
	}

	// So is an unsigned one.
	unsigned := newFakeGitHub(t)
	signedRelease(t, unsigned, "v2.0.0", map[string][]byte{asset: payload}, nil)
	unsignedRelease, _ := unsigned.client().Latest(context.Background(), false)
	if _, err := unsigned.client().VerifiedChecksums(context.Background(), unsignedRelease, public); err != errNoSignature {
		t.Errorf("unsigned release error = %v", err)
	}
}

func TestParsePublicKey(t *testing.T) {
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	if key, ok := ParsePublicKey(base64.StdEncoding.EncodeToString(public)); !ok || !bytes.Equal(key, public) {
		t.Error("valid key rejected")
	}
	for _, bad := range []string{"", "not base64!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, ok := ParsePublicKey(bad); ok {
			t.Errorf("ParsePublicKey(%q) accepted", bad)
		}
	}
}

func TestDetect(t *testing.T) {
	dir := t.TempDir()

	app := filepath.Join(dir, "Applications", "ytgrabber.app")
	os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755)
	if got := Detect(filepath.Join(app, "Contents", "MacOS", "ytgrabber"), "darwin"); got.Kind != InstallMacApp || got.Target != app {
		t.Errorf("macOS bundle = %+v", got)
	}
	if got := Detect(filepath.Join(dir, "ytgrabber"), "darwin"); got.Kind != InstallManual {
		t.Errorf("macOS without a bundle = %+v", got)
	}

	installed := filepath.Join(dir, "Program Files", "YT Grabber")
	os.MkdirAll(installed, 0o755)
	os.WriteFile(filepath.Join(installed, "uninstall.exe"), nil, 0o644)
	if got := Detect(filepath.Join(installed, "ytgrabber.exe"), "windows"); got.Kind != InstallWindowsSetup {
		t.Errorf("installed on Windows = %+v", got)
	}
	if got := Detect(filepath.Join(dir, "ytgrabber.exe"), "windows"); got.Kind != InstallWindowsPortable {
		t.Errorf("portable on Windows = %+v", got)
	}

	if got := Detect(filepath.Join(dir, "ytgrabber"), "linux"); got.Kind != InstallLinuxBinary {
		t.Errorf("Linux = %+v", got)
	}
	t.Setenv("APPIMAGE", "/x.AppImage")
	if got := Detect(filepath.Join(dir, "ytgrabber"), "linux"); got.Kind != InstallManual || got.Note == "" {
		t.Errorf("AppImage = %+v", got)
	}

	if got := Detect(filepath.Join(dir, "ytgrabber"), "freebsd"); got.Kind != InstallManual {
		t.Errorf("unsupported OS = %+v", got)
	}

	if runtime.GOOS != "windows" && os.Getuid() != 0 {
		readOnly := filepath.Join(dir, "ro")
		os.MkdirAll(readOnly, 0o555)
		t.Setenv("APPIMAGE", "")
		if got := Detect(filepath.Join(readOnly, "ytgrabber"), "linux"); got.Kind != InstallManual {
			t.Errorf("read-only Linux install = %+v", got)
		}
	}
}

func TestApplyLinuxBinary(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "bin", "ytgrabber")
	os.MkdirAll(filepath.Dir(target), 0o755)
	os.WriteFile(target, []byte("old"), 0o755)

	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"ytgrabber-2.0.0-linux-amd64/install.sh": "#!/bin/sh", "ytgrabber-2.0.0-linux-amd64/ytgrabber": "new"} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	downloaded := filepath.Join(dir, "update.tar.gz")
	os.WriteFile(downloaded, archive.Bytes(), 0o600)

	relaunch, err := Apply(Install{Kind: InstallLinuxBinary, Target: target}, downloaded, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != "new" || relaunch.Path != target {
		t.Errorf("target = %q, relaunch = %+v", data, relaunch)
	}
	if info, _ := os.Stat(target); runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Errorf("new binary not executable: %v", info.Mode())
	}
}

func TestApplyWindowsPortable(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "ytgrabber.exe")
	os.WriteFile(target, []byte("old"), 0o755)
	downloaded := filepath.Join(t.TempDir(), "portable.exe")
	os.WriteFile(downloaded, []byte("new"), 0o600)

	inst := Install{Kind: InstallWindowsPortable, Target: target}
	if _, err := Apply(inst, downloaded, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != "new" {
		t.Errorf("target = %q", data)
	}
	if data, _ := os.ReadFile(target + ".old"); string(data) != "old" {
		t.Errorf("old copy = %q", data)
	}
	CleanupLeftovers(inst)
	if _, err := os.Stat(target + ".old"); !os.IsNotExist(err) {
		t.Error("old copy not cleaned up")
	}
}

func TestApplyMacApp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bundles with symlinks")
	}
	dir := t.TempDir()
	bundle := filepath.Join(dir, "ytgrabber.app")
	os.MkdirAll(filepath.Join(bundle, "Contents", "MacOS"), 0o755)
	os.WriteFile(filepath.Join(bundle, "Contents", "MacOS", "ytgrabber"), []byte("old"), 0o755)

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	add := func(name string, mode os.FileMode, body string) {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(mode)
		w, _ := zw.CreateHeader(header)
		w.Write([]byte(body))
	}
	add("ytgrabber.app/", os.ModeDir|0o755, "")
	add("ytgrabber.app/Contents/MacOS/ytgrabber", 0o755, "new")
	add("ytgrabber.app/Contents/Info.plist", 0o644, "<plist/>")
	add("ytgrabber.app/Contents/Current", os.ModeSymlink|0o777, "MacOS")
	add("__MACOSX/ytgrabber.app/._Info.plist", 0o644, "fork")
	zw.Close()
	downloaded := filepath.Join(t.TempDir(), "update.zip")
	os.WriteFile(downloaded, archive.Bytes(), 0o600)

	inst := Install{Kind: InstallMacApp, Target: bundle}
	relaunch, err := Apply(inst, downloaded, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(bundle, "Contents", "MacOS", "ytgrabber")
	if data, _ := os.ReadFile(exe); string(data) != "new" || relaunch.Path != bundle {
		t.Errorf("bundle executable = %q, relaunch %+v", data, relaunch)
	}
	if info, _ := os.Stat(exe); info.Mode().Perm()&0o111 == 0 {
		t.Errorf("executable bit lost: %v", info.Mode())
	}
	if link, err := os.Readlink(filepath.Join(bundle, "Contents", "Current")); err != nil || link != "MacOS" {
		t.Errorf("symlink = %q, %v", link, err)
	}

	CleanupLeftovers(inst)
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "ytgrabber.app" {
		t.Errorf("left in the parent folder: %v", entries)
	}
}

func TestExtractZipRejectsEscapes(t *testing.T) {
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	w, _ := zw.Create("../evil")
	w.Write([]byte("x"))
	zw.Close()
	path := filepath.Join(t.TempDir(), "bad.zip")
	os.WriteFile(path, archive.Bytes(), 0o600)

	dest := t.TempDir()
	if err := extractZip(path, dest); err == nil {
		t.Error("extracted a path outside the destination")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "evil")); err == nil {
		t.Error("file written outside the destination")
	}
}
