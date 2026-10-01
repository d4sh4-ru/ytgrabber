// Package installer downloads portable builds of the external tools into
// a folder owned by the app, so a user without Homebrew/winget/apt (or
// admin rights) can get a working setup from inside the GUI. Every
// download is checked against the SHA-256 its publisher lists before
// anything is installed.
package installer

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/ulikunitz/xz"
)

type Stage string

const (
	StageDownloading Stage = "downloading"
	StageVerifying   Stage = "verifying"
	StageExtracting  Stage = "extracting"
	StageDone        Stage = "done"
	StageFailed      Stage = "failed"
)

// Progress is reported while a tool is being installed.
type Progress struct {
	Tool       string `json:"tool"`
	Stage      Stage  `json:"stage"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"` // -1 when the server didn't say
	Error      string `json:"error,omitempty"`
}

const (
	progressInterval = 200 * time.Millisecond
	// maxExtractedSize caps a single extracted file, so a corrupt or
	// malicious archive can't fill the disk.
	maxExtractedSize = 1 << 30
)

// Installer downloads tools into Dir.
type Installer struct {
	Dir       string
	Client    *http.Client
	UserAgent string
	// GOOS/GOARCH default to the running platform; tests override them.
	GOOS, GOARCH string
}

func (in Installer) platform() (string, string) {
	goos, goarch := in.GOOS, in.GOARCH
	if goos == "" {
		goos = goruntime.GOOS
	}
	if goarch == "" {
		goarch = goruntime.GOARCH
	}
	return goos, goarch
}

// Install downloads, verifies and installs tool. Files are written next to
// their destination first and renamed into place only once everything
// checked out, so a failed or cancelled install never leaves a broken
// executable behind.
func (in Installer) Install(ctx context.Context, tool string, report func(Progress)) error {
	goos, goarch := in.platform()
	list, err := artifacts(tool, goos, goarch)
	if err != nil {
		return err
	}
	return in.installArtifacts(ctx, tool, list, report)
}

func (in Installer) installArtifacts(ctx context.Context, tool string, list []artifact, report func(Progress)) error {
	if err := os.MkdirAll(in.Dir, 0o755); err != nil {
		return fmt.Errorf("не удалось создать папку %s: %w", in.Dir, err)
	}

	var staged []stagedFile
	defer func() {
		for _, file := range staged {
			os.Remove(file.temp)
		}
	}()

	for _, item := range list {
		files, err := in.fetch(ctx, tool, item, report)
		staged = append(staged, files...)
		if err != nil {
			return err
		}
	}

	for _, file := range staged {
		if err := replaceFile(file.temp, file.dest); err != nil {
			return fmt.Errorf("не удалось установить %s: %w", filepath.Base(file.dest), err)
		}
	}
	staged = nil
	report(Progress{Tool: tool, Stage: StageDone})
	return nil
}

type stagedFile struct {
	temp string
	dest string
}

func (in Installer) fetch(ctx context.Context, tool string, item artifact, report func(Progress)) ([]stagedFile, error) {
	download, err := os.CreateTemp(in.Dir, ".download-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		download.Close()
		os.Remove(download.Name())
	}()

	finalURL, digest, err := in.download(ctx, item.url, download, func(done, total int64) {
		report(Progress{Tool: tool, Stage: StageDownloading, Downloaded: done, Total: total})
	})
	if err != nil {
		return nil, err
	}

	report(Progress{Tool: tool, Stage: StageVerifying})
	checksumURL := item.checksumURL
	if checksumURL == "" {
		checksumURL = finalURL + ".sha256"
	}
	expected, err := in.fetchChecksum(ctx, checksumURL, item.checksumName)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(expected, digest) {
		return nil, fmt.Errorf("контрольная сумма %s не совпала: файл повреждён или подменён", path.Base(item.url))
	}

	report(Progress{Tool: tool, Stage: StageExtracting})
	return in.extract(download, item)
}

// download streams url into dst, hashing as it goes. It returns the URL
// after redirects and the hex SHA-256 of the body.
func (in Installer) download(ctx context.Context, url string, dst io.Writer, progress func(done, total int64)) (string, string, error) {
	resp, err := in.get(ctx, url)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	hash := sha256.New()
	counter := &progressWriter{total: resp.ContentLength, report: progress}
	if _, err := io.Copy(io.MultiWriter(dst, hash, counter), resp.Body); err != nil {
		return "", "", fmt.Errorf("загрузка %s прервалась: %w", path.Base(url), err)
	}
	progress(counter.done, resp.ContentLength)
	return resp.Request.URL.String(), hex.EncodeToString(hash.Sum(nil)), nil
}

func (in Installer) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if in.UserAgent != "" {
		req.Header.Set("User-Agent", in.UserAgent)
	}

	client := in.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("не удалось скачать %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("не удалось скачать %s: HTTP %d", url, resp.StatusCode)
	}
	return resp, nil
}

var sha256Re = regexp.MustCompile(`(?i)\b[0-9a-f]{64}\b`)

func (in Installer) fetchChecksum(ctx context.Context, url string, name string) (string, error) {
	resp, err := in.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if sum := parseChecksum(string(body), name); sum != "" {
		return sum, nil
	}
	return "", fmt.Errorf("в %s нет контрольной суммы для %s", url, name)
}

// parseChecksum finds a SHA-256 in a checksum file. With name, it reads
// the "<hash>  <file>" line for that file (sha256sum format); without, it
// takes the only hash in the file, whatever the layout (Deno's Windows
// checksums are PowerShell Get-FileHash output).
func parseChecksum(text string, name string) string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		hash := sha256Re.FindString(line)
		if hash == "" {
			continue
		}
		if name == "" {
			return strings.ToLower(hash)
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.TrimPrefix(fields[len(fields)-1], "*") == name {
			return strings.ToLower(hash)
		}
	}
	return ""
}

// extract pulls the wanted files out of the download into temp files next
// to their destinations.
func (in Installer) extract(download *os.File, item artifact) ([]stagedFile, error) {
	var staged []stagedFile
	found := map[string]bool{}

	stage := func(member string, r io.Reader) error {
		dest := filepath.Join(in.Dir, item.files[member])
		temp, err := os.CreateTemp(in.Dir, ".install-*")
		if err != nil {
			return err
		}
		staged = append(staged, stagedFile{temp: temp.Name(), dest: dest})

		written, err := io.Copy(temp, io.LimitReader(r, maxExtractedSize+1))
		closeErr := temp.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if written > maxExtractedSize {
			return fmt.Errorf("%s слишком большой", member)
		}
		found[member] = true
		return os.Chmod(temp.Name(), 0o755)
	}

	var err error
	switch item.archive {
	case archiveNone:
		if _, err = download.Seek(0, io.SeekStart); err == nil {
			for member := range item.files {
				err = stage(member, download)
			}
		}
	case archiveZip:
		err = extractZip(download, item.files, stage)
	case archiveTarXz:
		err = extractTarXz(download, item.files, stage)
	}
	if err != nil {
		return staged, fmt.Errorf("не удалось распаковать %s: %w", path.Base(item.url), err)
	}

	for member := range item.files {
		if !found[member] {
			return staged, fmt.Errorf("в архиве %s нет файла %s", path.Base(item.url), member)
		}
	}
	return staged, nil
}

// Archive members are matched by base name only: the builds nest their
// binaries differently ("bin/ffmpeg.exe", "ffmpeg-…/bin/ffmpeg", "deno").
// Nothing is ever written to a path taken from the archive.
func extractZip(file *os.File, wanted map[string]string, stage func(string, io.Reader) error) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	archive, err := zip.NewReader(file, info.Size())
	if err != nil {
		return err
	}
	for _, entry := range archive.File {
		name := path.Base(entry.Name)
		if _, ok := wanted[name]; !ok || entry.FileInfo().IsDir() {
			continue
		}
		r, err := entry.Open()
		if err != nil {
			return err
		}
		err = stage(name, r)
		r.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarXz(file *os.File, wanted map[string]string, stage func(string, io.Reader) error) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	decompressed, err := xz.NewReader(bufio.NewReader(file))
	if err != nil {
		return err
	}
	archive := tar.NewReader(decompressed)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Base(header.Name)
		if _, ok := wanted[name]; !ok || header.Typeflag != tar.TypeReg {
			continue
		}
		if err := stage(name, archive); err != nil {
			return err
		}
	}
}

// replaceFile moves temp over dest. On Windows a running executable can't
// be overwritten but can be renamed, so the old file is moved aside first.
func replaceFile(temp string, dest string) error {
	if err := os.Rename(temp, dest); err == nil {
		return nil
	}
	old := dest + ".old"
	os.Remove(old)
	if err := os.Rename(dest, old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(temp, dest); err != nil {
		return err
	}
	os.Remove(old)
	return nil
}

type progressWriter struct {
	done       int64
	total      int64
	lastReport time.Time
	report     func(done, total int64)
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.done += int64(len(p))
	if now := time.Now(); now.Sub(w.lastReport) >= progressInterval {
		w.lastReport = now
		w.report(w.done, w.total)
	}
	return len(p), nil
}
