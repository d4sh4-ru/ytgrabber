package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"time"

	"ytgrabber/internal/model"
	"ytgrabber/internal/platform"
	"ytgrabber/internal/ytdlp"
)

// CleanupHelperArg makes the binary act as the post-exit cleanup helper
// instead of starting the app; main.go checks for it (see RunCleanupHelper).
const CleanupHelperArg = "--ytgrabber-cleanup-after-exit"

const (
	cleanupHelperMaxWait = time.Minute
	cleanupRetries       = 20
	cleanupRetryDelay    = 500 * time.Millisecond
	quitDelay            = 300 * time.Millisecond
)

// StorageInfo is what the "delete all data" section shows, so the user
// knows how much space it frees.
type StorageInfo struct {
	DataDir    string `json:"dataDir"`
	DataBytes  int64  `json:"dataBytes"`
	MediaFiles int    `json:"mediaFiles"`
	MediaBytes int64  `json:"mediaBytes"`
}

// GetStorageInfo считает, сколько места занимают данные приложения и
// скачанные файлы из библиотеки.
func (a *App) GetStorageInfo() StorageInfo {
	info := StorageInfo{DataDir: a.paths.DataDir}

	for _, dir := range append([]string{a.paths.DataDir}, a.paths.WebviewDirs...) {
		info.DataBytes += platform.PathSize(dir)
	}

	for _, path := range a.libraryFiles() {
		if stat, err := os.Stat(path); err == nil && stat.Mode().IsRegular() {
			info.MediaFiles++
			info.MediaBytes += stat.Size()
		}
	}
	return info
}

// ResetAllData удаляет всё, что создало приложение: базу, настройки,
// журнал, кэш WebView, недокачанные файлы и опустевшие папки загрузок. С
// deleteMedia=true удаляются и все файлы из библиотеки. Файлы, которые
// приложение не создавало, не трогаются. После очистки приложение
// закрывается — работать ему больше не с чем.
//
// Работает и когда база не открылась при запуске: для повреждённой базы
// это как раз способ начать с чистого листа.
func (a *App) ResetAllData(deleteMedia bool) error {
	log.Printf("ytgrabber: deleting all data (media files: %v)", deleteMedia)

	a.stopAllDownloads(errReset)

	var failed []string
	remove := func(path string) {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failed = append(failed, path)
		}
	}

	if a.db != nil {
		// Leftovers of unfinished jobs are always junk. The library check
		// keeps finished files safe when deleteMedia is false.
		a.mu.RLock()
		type leftover struct {
			tracks         []model.DownloadTrack
			outputFilename string
		}
		var leftovers []leftover
		for _, j := range a.jobs {
			leftovers = append(leftovers, leftover{j.snapshot().Tracks, j.outputFilename})
		}
		a.mu.RUnlock()
		for _, l := range leftovers {
			ytdlp.RemovePartialFiles(l.tracks, l.outputFilename, a.isFileReferenced)
		}

		if deleteMedia {
			for _, path := range a.libraryFiles() {
				remove(path)
			}
		}

		if err := a.db.Close(); err != nil {
			log.Printf("ytgrabber: failed to close database: %v", err)
		}
		a.db = nil
	}

	// Only folders that are empty now: a downloads folder may be one the
	// user picked and share with other files.
	for _, dir := range []string{a.GetSettings().DownloadsDir, a.paths.DefaultDownloadsDir, a.paths.LegacyDownloadsDir} {
		if dir != "" {
			_ = os.Remove(dir)
		}
	}

	// The log file lives in the data directory, which goes next.
	log.SetOutput(os.Stderr)
	if a.logFile != nil {
		a.logFile.Close()
		a.logFile = nil
	}

	appDirs := append([]string{a.paths.DataDir}, a.paths.WebviewDirs...)
	for _, dir := range appDirs {
		if err := removeAppDir(dir); err != nil {
			log.Printf("ytgrabber: %v (retrying after exit)", err)
		}
	}

	// WebView storage is in use until the process exits (and on Windows
	// locked), and the webview may flush it once more on quit.
	if err := a.startHelper(appDirs); err != nil {
		log.Printf("ytgrabber: failed to start cleanup helper: %v", err)
	}

	go func() {
		// Let this call's result reach the frontend before the window closes.
		time.Sleep(quitDelay)
		a.quit()
	}()

	if len(failed) > 0 {
		return fmt.Errorf("не удалось удалить файлов: %d (например, %s)", len(failed), failed[0])
	}
	return nil
}

// libraryFiles lists the media files and thumbnails of every library entry.
func (a *App) libraryFiles() []string {
	if a.db == nil {
		return nil
	}
	videos, err := a.db.LoadVideos()
	if err != nil {
		log.Printf("ytgrabber: failed to load library: %v", err)
		return nil
	}

	seen := map[string]bool{}
	var files []string
	for _, video := range videos {
		for _, path := range []string{video.FilePath, video.ThumbnailPath} {
			if path != "" && !seen[path] {
				seen[path] = true
				files = append(files, path)
			}
		}
	}
	return files
}

// removeAppDir deletes a directory (or file) that belongs to the app,
// refusing obviously wrong targets as a last line of defence against a
// misconfigured path wiping something that isn't ours.
func removeAppDir(path string) error {
	if !platform.IsSafeToRemove(path) {
		return fmt.Errorf("refusing to delete %q", path)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("delete %s: %w", path, err)
	}
	return nil
}

// startCleanupHelper re-runs this binary as a helper that deletes paths
// once the app has exited. The helper learns about the exit from its
// stdin: the app holds the write end of the pipe, and the OS closes it
// when the process ends — no pid polling, the same on every platform.
func (a *App) startCleanupHelper(paths []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, append([]string{CleanupHelperArg}, paths...)...)
	platform.ConfigureCommand(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	// Keep the pipe open (and referenced, so it isn't closed by a
	// finalizer) until the process exits.
	a.cleanupPipe = stdin
	go func() { _ = cmd.Wait() }()
	return nil
}

// RunCleanupHelper is the helper's entire life: wait for the app to exit,
// then delete the given paths, retrying while files are still locked.
func RunCleanupHelper(paths []string) {
	exited := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		close(exited)
	}()
	select {
	case <-exited:
	case <-time.After(cleanupHelperMaxWait):
	}

	for attempt := 0; attempt < cleanupRetries; attempt++ {
		time.Sleep(cleanupRetryDelay)

		remaining := paths[:0]
		for _, path := range paths {
			if err := removeAppDir(path); err != nil {
				remaining = append(remaining, path)
			}
		}
		paths = remaining
		if len(paths) == 0 {
			return
		}
	}
}
