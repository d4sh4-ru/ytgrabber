package installer

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
)

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, _ := w.Create(name)
		f.Write([]byte(content))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func tarXzOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	xw, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(xw)
	tw.WriteHeader(&tar.Header{Name: "ffmpeg-master/bin/", Typeflag: tar.TypeDir, Mode: 0o755})
	for name, content := range files {
		tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(content))})
		tw.Write([]byte(content))
	}
	tw.Close()
	xw.Close()
	return buf.Bytes()
}

// fakeRelease serves files and rewrites the artifact URLs to point at it.
type fakeRelease struct {
	server *httptest.Server
	files  map[string][]byte
}

func newFakeRelease(t *testing.T, files map[string][]byte) *fakeRelease {
	t.Helper()
	release := &fakeRelease{files: files}
	release.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if target, ok := strings.CutPrefix(r.URL.Path, "/redirect"); ok {
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
			return
		}
		data, ok := release.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	}))
	t.Cleanup(release.server.Close)
	return release
}

func (r *fakeRelease) url(path string) string {
	return r.server.URL + path
}

func installArtifacts(t *testing.T, dir string, list []artifact) error {
	t.Helper()
	in := Installer{Dir: dir}
	var stages []Stage
	err := in.installArtifacts(context.Background(), "tool", list, func(p Progress) {
		stages = append(stages, p.Stage)
	})
	if err == nil && stages[len(stages)-1] != StageDone {
		t.Errorf("last stage = %v", stages)
	}
	return err
}

func TestInstallRawWithChecksumList(t *testing.T) {
	binary := []byte("#!/bin/sh\necho yt-dlp\n")
	release := newFakeRelease(t, map[string][]byte{
		"/yt-dlp_linux": binary,
		"/SHA2-256SUMS": []byte(fmt.Sprintf("%s  yt-dlp_linux.zip\n%s  yt-dlp_linux\n", sha([]byte("other")), sha(binary))),
	})
	dir := t.TempDir()

	err := installArtifacts(t, dir, []artifact{{
		url: release.url("/yt-dlp_linux"), checksumURL: release.url("/SHA2-256SUMS"), checksumName: "yt-dlp_linux",
		archive: archiveNone, files: map[string]string{"yt-dlp_linux": "yt-dlp"},
	}})
	if err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, "yt-dlp"))
	if !bytes.Equal(got, binary) {
		t.Errorf("installed %q", got)
	}
	if info, _ := os.Stat(filepath.Join(dir, "yt-dlp")); goruntime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Error("not executable")
	}
	assertOnly(t, dir, "yt-dlp")
}

func TestInstallZipWithChecksumNextToRedirectTarget(t *testing.T) {
	archive := zipOf(t, map[string]string{"ffmpeg": "binary", "README.txt": "x"})
	release := newFakeRelease(t, map[string][]byte{
		"/download/9.0/ffmpeg.zip":        archive,
		"/download/9.0/ffmpeg.zip.sha256": []byte(sha(archive) + "  ffmpeg.zip\n"),
	})
	dir := t.TempDir()

	err := installArtifacts(t, dir, []artifact{{
		url: release.url("/redirect/download/9.0/ffmpeg.zip"), archive: archiveZip,
		files: map[string]string{"ffmpeg": "ffmpeg"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertOnly(t, dir, "ffmpeg")
}

func TestInstallTarXz(t *testing.T) {
	archive := tarXzOf(t, map[string]string{
		"ffmpeg-master/bin/ffmpeg":  "ffmpeg",
		"ffmpeg-master/bin/ffprobe": "ffprobe",
		"ffmpeg-master/LICENSE":     "gpl",
	})
	release := newFakeRelease(t, map[string][]byte{
		"/ffmpeg.tar.xz":    archive,
		"/checksums.sha256": []byte(sha(archive) + "  ffmpeg.tar.xz\n"),
	})
	dir := t.TempDir()

	err := installArtifacts(t, dir, []artifact{{
		url: release.url("/ffmpeg.tar.xz"), checksumURL: release.url("/checksums.sha256"), checksumName: "ffmpeg.tar.xz",
		archive: archiveTarXz, files: map[string]string{"ffmpeg": "ffmpeg", "ffprobe": "ffprobe"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertOnly(t, dir, "ffmpeg", "ffprobe")
}

// TestChecksumMismatchInstallsNothing also checks an existing working copy
// is left untouched.
func TestChecksumMismatchInstallsNothing(t *testing.T) {
	release := newFakeRelease(t, map[string][]byte{
		"/deno.zip":           zipOf(t, map[string]string{"deno": "tampered"}),
		"/deno.zip.sha256sum": []byte(sha([]byte("original")) + "  deno.zip\n"),
	})
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "deno"), []byte("working copy"), 0o755)

	err := installArtifacts(t, dir, []artifact{{
		url: release.url("/deno.zip"), checksumURL: release.url("/deno.zip.sha256sum"),
		archive: archiveZip, files: map[string]string{"deno": "deno"},
	}})
	if err == nil || !strings.Contains(err.Error(), "контрольная сумма") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "deno")); string(got) != "working copy" {
		t.Errorf("existing file replaced: %q", got)
	}
	assertOnly(t, dir, "deno")
}

func TestMissingArchiveMember(t *testing.T) {
	archive := zipOf(t, map[string]string{"bin/ffmpeg.exe": "x"})
	release := newFakeRelease(t, map[string][]byte{
		"/ff.zip":     archive,
		"/ff.zip.sum": []byte(sha(archive)),
	})
	dir := t.TempDir()

	err := installArtifacts(t, dir, []artifact{{
		url: release.url("/ff.zip"), checksumURL: release.url("/ff.zip.sum"), archive: archiveZip,
		files: map[string]string{"ffmpeg.exe": "ffmpeg.exe", "ffprobe.exe": "ffprobe.exe"},
	}})
	if err == nil || !strings.Contains(err.Error(), "ffprobe.exe") {
		t.Fatalf("err = %v", err)
	}
	assertOnly(t, dir)
}

func TestParseChecksum(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	tests := []struct {
		text, name, want string
	}{
		{hash + "  yt-dlp_macos\n" + strings.Repeat("cd", 32) + "  yt-dlp_macos.zip\n", "yt-dlp_macos", hash},
		{strings.Repeat("cd", 32) + "  yt-dlp_macos.zip\n", "yt-dlp_macos", ""},
		{hash + " *file.zip", "file.zip", hash},
		{"\nAlgorithm : SHA256\nHash      : " + strings.ToUpper(hash) + "\nPath      : C:\\a\\deno.zip\n", "", hash},
		{hash + "  ffmpeg.zip", "", hash},
		{"not a checksum", "", ""},
	}
	for _, tt := range tests {
		if got := parseChecksum(tt.text, tt.name); got != tt.want {
			t.Errorf("parseChecksum(%q, %q) = %q, want %q", tt.text, tt.name, got, tt.want)
		}
	}
}

func TestArtifactsCoverReleasePlatforms(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "linux"} {
		for _, goarch := range []string{"amd64", "arm64"} {
			for _, tool := range Tools {
				list, err := artifacts(tool, goos, goarch)
				if err != nil || len(list) == 0 {
					t.Errorf("%s on %s/%s: %v", tool, goos, goarch, err)
				}
				for _, item := range list {
					if !strings.HasPrefix(item.url, "https://") {
						t.Errorf("%s on %s/%s: insecure URL %s", tool, goos, goarch, item.url)
					}
					for _, installed := range item.files {
						if goos == "windows" && !strings.HasSuffix(installed, ".exe") {
							t.Errorf("%s on windows installs %s without .exe", tool, installed)
						}
					}
				}
			}
		}
	}
	if _, err := artifacts("ffmpeg", "freebsd", "amd64"); err == nil {
		t.Error("unsupported platform accepted")
	}
}

// TestLiveInstall downloads the real tools for this machine and runs them.
// It needs network access, so it only runs on request:
//
//	YTGRABBER_LIVE_INSTALL=1 go test ./internal/installer -run Live -v
func TestLiveInstall(t *testing.T) {
	if os.Getenv("YTGRABBER_LIVE_INSTALL") == "" {
		t.Skip("set YTGRABBER_LIVE_INSTALL=1 to download the real tools")
	}
	dir := t.TempDir()
	in := Installer{Dir: dir, UserAgent: "ytgrabber-test"}

	for _, tool := range Tools {
		if err := in.Install(context.Background(), tool, func(Progress) {}); err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
	}
	for name, flag := range map[string]string{"yt-dlp": "--version", "ffmpeg": "-version", "ffprobe": "-version", "deno": "--version"} {
		if goruntime.GOOS == "windows" {
			name += ".exe"
		}
		out, err := exec.Command(filepath.Join(dir, name), flag).CombinedOutput()
		if err != nil {
			t.Errorf("%s doesn't run: %v\n%s", name, err, out)
			continue
		}
		firstLine, _, _ := strings.Cut(string(out), "\n")
		t.Logf("%s: %s", name, firstLine)
	}
}

func assertOnly(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	if strings.Join(got, ",") != strings.Join(names, ",") {
		t.Errorf("dir contains %v, want %v", got, names)
	}
}
