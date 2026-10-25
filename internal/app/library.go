package app

import (
	"errors"
	"fmt"
	"log"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"ytgrabber/internal/model"
	"ytgrabber/internal/platform"
	"ytgrabber/internal/ytdlp"
)

// GetLibrary возвращает скачанные файлы, отмечая те, что были удалены или
// перемещены вне приложения.
func (a *App) GetLibrary() ([]*model.VideoRecord, error) {
	if err := a.ensureReady(); err != nil {
		return []*model.VideoRecord{}, err
	}

	videos, err := a.db.LoadVideos()
	if err != nil {
		return []*model.VideoRecord{}, fmt.Errorf("не удалось загрузить библиотеку: %w", err)
	}

	for _, video := range videos {
		decorateVideo(video)
		if video.FileMissing && a.repairVideo(video) {
			decorateVideo(video)
		}
	}
	return videos, nil
}

// repairVideo fixes an entry whose title and path were recorded mangled:
// releases before yt-dlp got --encoding utf-8 read non-Latin titles in
// the wrong encoding, so the path pointed at a file that doesn't exist
// while the real one sits next to it. The " [<id>].<ext>" ending of the
// name survives and finds it.
func (a *App) repairVideo(video *model.VideoRecord) bool {
	filePath, ok := ytdlp.FindByID(video.FilePath)
	if !ok || a.isFileReferenced(filePath) {
		return false
	}
	thumbnail := video.ThumbnailPath
	if found, ok := ytdlp.FindByID(thumbnail); ok {
		if _, err := os.Stat(thumbnail); err != nil {
			thumbnail = found
		}
	}
	title := video.Title
	if !utf8.ValidString(title) || strings.Contains(title, "?") || strings.TrimSpace(title) == "" {
		title = ytdlp.TitleFromFilename(filePath)
	}

	if err := a.db.RelocateVideo(video.ID, filePath, thumbnail, title); err != nil {
		log.Printf("ytgrabber: failed to repair library entry %s: %v", video.ID, err)
		return false
	}
	log.Printf("ytgrabber: library entry %s found its file again", video.ID)
	video.FilePath, video.ThumbnailPath, video.Title = filePath, thumbnail, title
	return true
}

// DeleteVideo удаляет запись из библиотеки; с deleteFiles=true — ещё и сам
// файл с обложкой (если обложка не используется другой записью).
func (a *App) DeleteVideo(id string, deleteFiles bool) error {
	if err := a.ensureReady(); err != nil {
		return err
	}

	video, err := a.db.GetVideo(id)
	if err != nil {
		return err
	}

	if err := a.db.DeleteVideo(id); err != nil {
		return fmt.Errorf("не удалось удалить запись: %w", err)
	}

	if deleteFiles {
		for _, path := range []string{video.FilePath, video.ThumbnailPath} {
			if path == "" || a.isFileReferenced(path) {
				continue
			}
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Printf("ytgrabber: failed to delete %s: %v", path, err)
				return fmt.Errorf("запись удалена, но файл удалить не удалось: %v", err)
			}
		}
	}

	a.publish("library-changed")
	return nil
}

const (
	// resumeMinSeconds: stopping this early isn't worth resuming.
	resumeMinSeconds = 5
	// Closer than this to the end, or past resumeWatchedShare of it, counts
	// as watched: the next play starts over, as on video sites.
	resumeEndMarginSeconds = 10
	resumeWatchedShare     = 0.95
)

// SavePlaybackPosition запоминает, на какой секунде остановилось
// воспроизведение, и возвращает сохранённое значение: 0, если досмотрено
// почти до конца или почти не начато (тогда в следующий раз — с начала).
func (a *App) SavePlaybackPosition(id string, seconds float64) (float64, error) {
	if err := a.ensureReady(); err != nil {
		return 0, err
	}
	video, err := a.db.GetVideo(id)
	if err != nil {
		return 0, err
	}
	position := resumePosition(seconds, video.DurationSeconds)
	if err := a.db.SavePosition(id, position); err != nil {
		return 0, fmt.Errorf("не удалось сохранить позицию: %w", err)
	}
	return position, nil
}

// resumePosition is where the next playback should start.
func resumePosition(seconds float64, duration float64) float64 {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < resumeMinSeconds {
		return 0
	}
	if duration > 0 && (seconds >= duration-resumeEndMarginSeconds || seconds >= duration*resumeWatchedShare) {
		return 0
	}
	return seconds
}

// RevealVideo показывает файл в Finder / Проводнике / файловом менеджере.
func (a *App) RevealVideo(id string) error {
	if err := a.ensureReady(); err != nil {
		return err
	}

	video, err := a.db.GetVideo(id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(video.FilePath); err != nil {
		return fmt.Errorf("файл не найден: %s", video.FilePath)
	}
	if err := platform.RevealInFileManager(video.FilePath); err != nil {
		return fmt.Errorf("не удалось открыть файловый менеджер: %w", err)
	}
	return nil
}

// decorateVideo fills the fields computed at read time: whether the file
// is still there and the URLs the frontend loads it by.
func decorateVideo(video *model.VideoRecord) {
	_, err := os.Stat(video.FilePath)
	video.FileMissing = err != nil

	base := mediaRoutePrefix + url.PathEscape(video.ID) + "/"
	// The extension is kept in the URL on purpose: the Angular dev server
	// answers extension-less paths with index.html (SPA fallback) instead
	// of letting Wails fall through to the media handler.
	video.MediaURL = base + "file" + strings.ToLower(filepath.Ext(video.FilePath))
	video.ThumbnailURL = ""
	if video.ThumbnailPath != "" {
		if _, err := os.Stat(video.ThumbnailPath); err == nil {
			video.ThumbnailURL = base + "thumbnail.jpg"
		}
	}
}
