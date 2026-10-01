package app

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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
	}
	return videos, nil
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
	if _, err := os.Stat(video.FilePath); err != nil {
		video.FileMissing = true
	}

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

func fileTitle(path string) string {
	return ytdlp.StripExt(filepath.Base(path))
}
