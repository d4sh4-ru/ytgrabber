package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"ytgrabber/internal/platform"
	"ytgrabber/internal/storage"
	"ytgrabber/internal/tools"
)

const (
	settingDownloadsDir  = "downloads_dir"
	settingYtDlpPath     = "ytdlp_path"
	settingFfmpegPath    = "ffmpeg_path"
	settingMaxConcurrent = "max_concurrent_downloads"
	settingProxy         = "proxy"

	settingUpdateAutoCheck  = "update_auto_check"
	settingUpdatePrerelease = "update_prerelease"
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
	// Proxy is the proxy yt-dlp goes through, e.g. socks5://host:1080;
	// empty means a direct connection. Outside core the password in it
	// is masked (see public).
	Proxy string `json:"proxy"`
	// AutoCheckUpdates looks for a new version at startup and daily;
	// PrereleaseUpdates offers test versions too.
	AutoCheckUpdates  bool `json:"autoCheckUpdates"`
	PrereleaseUpdates bool `json:"prereleaseUpdates"`
}

func loadSettings(db *storage.DB, defaultDownloadsDir string) (Settings, error) {
	settings := Settings{
		DownloadsDir:           defaultDownloadsDir,
		MaxConcurrentDownloads: defaultMaxConcurrentDownloads,
		AutoCheckUpdates:       true,
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
	settings.Proxy = values[settingProxy]
	if n, err := strconv.Atoi(values[settingMaxConcurrent]); err == nil {
		settings.MaxConcurrentDownloads = clampConcurrency(n)
	}
	settings.AutoCheckUpdates = values[settingUpdateAutoCheck] != "0"
	settings.PrereleaseUpdates = values[settingUpdatePrerelease] == "1"
	return settings, nil
}

// resolveTools locates the external tools, honouring paths set by the
// user and copies installed by the app (see InstallTools).
func (a *App) resolveTools(s Settings) tools.Set {
	return tools.Resolve(s.YtDlpPath, s.FfmpegPath, a.paths.ToolsDir())
}

// boolSetting stores a flag; "0" rather than "" for false, since an
// empty value deletes the setting (and with it a non-false default).
func boolSetting(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func clampConcurrency(n int) int {
	return min(max(n, 1), maxConcurrentDownloadsLimit)
}

// GetSettings возвращает текущие настройки; папка загрузок — та, куда
// скачивает этот пользователь.
func (a *App) GetSettings(user UserID) Settings {
	a.mu.RLock()
	defer a.mu.RUnlock()
	settings := a.settings.public()
	settings.DownloadsDir = a.downloadsDirLocked(user)
	return settings
}

// public is s as shown to users: the proxy password is masked, since
// every user can read the settings but only the admin sets the proxy.
func (s Settings) public() Settings {
	if u, err := url.Parse(s.Proxy); err == nil && s.Proxy != "" {
		s.Proxy = u.Redacted()
	}
	return s
}

// downloadsDirLocked is where user's downloads go: the downloads folder
// itself for the local user, a folder of their own in it for anyone else.
// Must be called with a.mu held.
func (a *App) downloadsDirLocked(user UserID) string {
	if user == LocalUser {
		return a.settings.DownloadsDir
	}
	return filepath.Join(a.settings.DownloadsDir, userDirName(user))
}

// userDirName is a folder name for user that can't step out of the
// downloads folder: the id itself when it is plain, a hash of it if not.
func userDirName(user UserID) string {
	name := string(user)
	plain := name != "" && len(name) <= 64
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			plain = false
			break
		}
	}
	if plain {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return "user-" + hex.EncodeToString(sum[:8])
}

func (a *App) currentSettings() Settings {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.settings.public()
}

// proxy is the proxy URL yt-dlp should use, "" for none.
func (a *App) proxy() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.settings.Proxy
}

// SetDownloadsDir меняет папку загрузок на dir (выбранную в диалоге
// адаптером) после проверки, что в неё можно писать.
func (a *App) SetDownloadsDir(user UserID, dir string) (Settings, error) {
	if err := a.ensureReady(); err != nil {
		return a.currentSettings(), err
	}
	if dir == "" {
		return a.currentSettings(), errors.New("не указана папка")
	}
	if err := ensureWritableDir(dir); err != nil {
		return a.currentSettings(), err
	}

	return a.updateSettings(func(s *Settings) {
		s.DownloadsDir = dir
	}, settingDownloadsDir, dir)
}

// SetToolPath задаёт исполняемый файл yt-dlp или ffmpeg вручную, если
// автоматический поиск его не находит.
func (a *App) SetToolPath(tool string, path string) (Settings, error) {
	if err := a.ensureReady(); err != nil {
		return a.currentSettings(), err
	}

	key, err := toolSettingKey(tool)
	if err != nil {
		return a.currentSettings(), err
	}
	if !platform.IsExecutableFile(path) {
		return a.currentSettings(), fmt.Errorf("%s не является исполняемым файлом", path)
	}

	return a.setToolPath(tool, key, path)
}

// ResetToolPath возвращает автоматический поиск инструмента.
func (a *App) ResetToolPath(tool string) (Settings, error) {
	if err := a.ensureReady(); err != nil {
		return a.currentSettings(), err
	}

	key, err := toolSettingKey(tool)
	if err != nil {
		return a.currentSettings(), err
	}
	return a.setToolPath(tool, key, "")
}

// SetMaxConcurrentDownloads меняет число одновременных загрузок; задачи в
// очереди подхватываются сразу, если лимит увеличился.
func (a *App) SetMaxConcurrentDownloads(n int) (Settings, error) {
	if err := a.ensureReady(); err != nil {
		return a.currentSettings(), err
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

// SetProxy задаёт прокси для yt-dlp (загрузки и проверка аккаунтов);
// пустая строка — без прокси. Поддерживаются http, https, socks4 и socks5.
func (a *App) SetProxy(proxy string) (Settings, error) {
	if err := a.ensureReady(); err != nil {
		return a.currentSettings(), err
	}

	proxy, err := normalizeProxy(proxy)
	if err != nil {
		return a.currentSettings(), err
	}
	proxy = keepMaskedPassword(proxy, a.proxy())
	return a.updateSettings(func(s *Settings) {
		s.Proxy = proxy
	}, settingProxy, proxy)
}

// proxySchemes are the proxy kinds yt-dlp understands.
var proxySchemes = []string{"http", "https", "socks4", "socks4a", "socks5", "socks5h"}

// normalizeProxy checks proxy is a URL yt-dlp accepts as --proxy.
func normalizeProxy(proxy string) (string, error) {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return "", nil
	}
	u, err := url.Parse(proxy)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return "", errors.New("прокси указывается адресом вида socks5://host:1080 или http://user:pass@host:3128")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if !slices.Contains(proxySchemes, u.Scheme) {
		return "", fmt.Errorf("неподдерживаемый тип прокси %q: доступны %s", u.Scheme, strings.Join(proxySchemes, ", "))
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("в адресе прокси не должно быть пути и параметров")
	}
	u.Path = ""
	return u.String(), nil
}

// keepMaskedPassword puts back the stored password when proxy came from
// the settings screen with it still masked (see Settings.public), so the
// admin can change the host without retyping it.
func keepMaskedPassword(proxy string, current string) string {
	next, nextErr := url.Parse(proxy)
	prev, prevErr := url.Parse(current)
	if nextErr != nil || prevErr != nil || next.User == nil || prev.User == nil ||
		next.User.Username() != prev.User.Username() {
		return proxy
	}
	password, hasPassword := next.User.Password()
	stored, hasStored := prev.User.Password()
	if !hasPassword || !hasStored || password != maskedPassword(prev) {
		return proxy
	}
	next.User = url.UserPassword(next.User.Username(), stored)
	return next.String()
}

// maskedPassword is what url.Redacted puts in place of a password.
func maskedPassword(u *url.URL) string {
	redacted, err := url.Parse(u.Redacted())
	if err != nil || redacted.User == nil {
		return ""
	}
	password, _ := redacted.User.Password()
	return password
}

// OpenDownloadsDir открывает папку загрузок в файловом менеджере.
func (a *App) OpenDownloadsDir(user UserID) error {
	dir := a.GetSettings(user).DownloadsDir
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
		return a.currentSettings(), fmt.Errorf("не удалось сохранить настройку: %w", err)
	}

	a.mu.Lock()
	apply(&a.settings)
	a.tools = a.resolveTools(a.settings)
	settings := a.settings.public()
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
