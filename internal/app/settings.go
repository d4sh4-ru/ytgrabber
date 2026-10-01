package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"ytgrabber/internal/platform"
	"ytgrabber/internal/storage"
	"ytgrabber/internal/tools"
)

const (
	settingDownloadsDir  = "downloads_dir"
	settingYtDlpPath     = "ytdlp_path"
	settingFfmpegPath    = "ffmpeg_path"
	settingMaxConcurrent = "max_concurrent_downloads"
)

const (
	defaultMaxConcurrentDownloads = 2
	maxConcurrentDownloadsLimit   = 5
)

// Settings are the user-editable preferences. Empty tool paths mean
// "find automatically".
type Settings struct {
	DownloadsDir           string `json:"downloadsDir"`
	YtDlpPath              string `json:"ytDlpPath"`
	FfmpegPath             string `json:"ffmpegPath"`
	MaxConcurrentDownloads int    `json:"maxConcurrentDownloads"`
}

func loadSettings(db *storage.DB, defaultDownloadsDir string) (Settings, error) {
	settings := Settings{
		DownloadsDir:           defaultDownloadsDir,
		MaxConcurrentDownloads: defaultMaxConcurrentDownloads,
	}

	values, err := db.LoadSettings()
	if err != nil {
		return settings, err
	}

	if dir := values[settingDownloadsDir]; dir != "" {
		settings.DownloadsDir = dir
	}
	settings.YtDlpPath = values[settingYtDlpPath]
	settings.FfmpegPath = values[settingFfmpegPath]
	if n, err := strconv.Atoi(values[settingMaxConcurrent]); err == nil {
		settings.MaxConcurrentDownloads = clampConcurrency(n)
	}
	return settings, nil
}

// resolveTools locates the external tools, honouring paths set by the
// user and copies installed by the app (see InstallTools).
func (a *App) resolveTools(s Settings) tools.Set {
	return tools.Resolve(s.YtDlpPath, s.FfmpegPath, a.paths.ToolsDir())
}

func clampConcurrency(n int) int {
	return min(max(n, 1), maxConcurrentDownloadsLimit)
}

// GetSettings возвращает текущие настройки.
func (a *App) GetSettings() Settings {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.settings
}

// ChooseDownloadsDir открывает системный диалог выбора папки загрузок.
// Отмена диалога не считается ошибкой — возвращаются текущие настройки.
func (a *App) ChooseDownloadsDir() (Settings, error) {
	if err := a.ensureReady(); err != nil {
		return a.GetSettings(), err
	}

	current := a.GetSettings()
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "Папка для загрузок",
		DefaultDirectory:     existingDirOrHome(current.DownloadsDir),
		CanCreateDirectories: true,
	})
	if err != nil {
		return current, fmt.Errorf("не удалось открыть диалог: %w", err)
	}
	if dir == "" {
		return current, nil
	}

	if err := ensureWritableDir(dir); err != nil {
		return current, err
	}

	return a.updateSettings(func(s *Settings) {
		s.DownloadsDir = dir
	}, settingDownloadsDir, dir)
}

// ChooseToolPath позволяет указать исполняемый файл yt-dlp или ffmpeg
// вручную, если автоматический поиск его не находит.
func (a *App) ChooseToolPath(tool string) (Settings, error) {
	if err := a.ensureReady(); err != nil {
		return a.GetSettings(), err
	}

	key, err := toolSettingKey(tool)
	if err != nil {
		return a.GetSettings(), err
	}

	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: fmt.Sprintf("Исполняемый файл %s", tool),
	})
	if err != nil {
		return a.GetSettings(), fmt.Errorf("не удалось открыть диалог: %w", err)
	}
	if path == "" {
		return a.GetSettings(), nil
	}
	if !platform.IsExecutableFile(path) {
		return a.GetSettings(), fmt.Errorf("%s не является исполняемым файлом", path)
	}

	return a.setToolPath(tool, key, path)
}

// ResetToolPath возвращает автоматический поиск инструмента.
func (a *App) ResetToolPath(tool string) (Settings, error) {
	if err := a.ensureReady(); err != nil {
		return a.GetSettings(), err
	}

	key, err := toolSettingKey(tool)
	if err != nil {
		return a.GetSettings(), err
	}
	return a.setToolPath(tool, key, "")
}

// SetMaxConcurrentDownloads меняет число одновременных загрузок; задачи в
// очереди подхватываются сразу, если лимит увеличился.
func (a *App) SetMaxConcurrentDownloads(n int) (Settings, error) {
	if err := a.ensureReady(); err != nil {
		return a.GetSettings(), err
	}

	n = clampConcurrency(n)
	settings, err := a.updateSettings(func(s *Settings) {
		s.MaxConcurrentDownloads = n
	}, settingMaxConcurrent, strconv.Itoa(n))
	if err == nil {
		a.schedule()
	}
	return settings, err
}

// OpenDownloadsDir открывает папку загрузок в файловом менеджере.
func (a *App) OpenDownloadsDir() error {
	dir := a.GetSettings().DownloadsDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("не удалось создать папку %s: %w", dir, err)
	}
	if err := platform.OpenInFileManager(dir); err != nil {
		return fmt.Errorf("не удалось открыть папку: %w", err)
	}
	return nil
}

func (a *App) setToolPath(tool string, key string, path string) (Settings, error) {
	return a.updateSettings(func(s *Settings) {
		switch tool {
		case tools.YtDlp:
			s.YtDlpPath = path
		case tools.Ffmpeg:
			s.FfmpegPath = path
		}
	}, key, path)
}

// updateSettings persists one setting and applies it in memory. Tool
// locations are re-resolved on every change since they depend on settings.
func (a *App) updateSettings(apply func(*Settings), key string, value string) (Settings, error) {
	if err := a.db.SetSetting(key, value); err != nil {
		return a.GetSettings(), fmt.Errorf("не удалось сохранить настройку: %w", err)
	}

	a.mu.Lock()
	apply(&a.settings)
	a.tools = a.resolveTools(a.settings)
	settings := a.settings
	a.mu.Unlock()
	return settings, nil
}

func toolSettingKey(tool string) (string, error) {
	switch tool {
	case tools.YtDlp:
		return settingYtDlpPath, nil
	case tools.Ffmpeg:
		return settingFfmpegPath, nil
	default:
		return "", fmt.Errorf("неизвестный инструмент %q", tool)
	}
}

// ensureWritableDir creates dir if needed and checks a file can actually be
// written there — a read-only or permission-restricted folder would
// otherwise only fail once a download finishes.
func ensureWritableDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("не удалось создать папку %s: %w", dir, err)
	}

	probe, err := os.CreateTemp(dir, ".ytgrabber-write-test-*")
	if err != nil {
		return fmt.Errorf("нет прав на запись в папку %s", dir)
	}
	name := probe.Name()
	probe.Close()
	if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("нет прав на запись в папку %s", dir)
	}
	return nil
}

func existingDirOrHome(dir string) string {
	for current := dir; current != "" && current != filepath.Dir(current); current = filepath.Dir(current) {
		if info, err := os.Stat(current); err == nil && info.IsDir() {
			return current
		}
	}
	return platform.HomeDir()
}
