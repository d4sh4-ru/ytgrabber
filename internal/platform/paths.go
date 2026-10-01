// Package platform contains everything that differs between macOS, Windows
// and Linux: where data lives, how child processes are started and killed,
// and how to open the system file manager.
package platform

import (
	"bufio"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// Paths describes where the app keeps its state. Everything is resolved
// to absolute paths up front: the library stores absolute file paths, so a
// relative one would silently break as soon as the working directory changes.
type Paths struct {
	DataDir             string   // SQLite database and log file
	DefaultDownloadsDir string   // used until the user picks another folder
	LegacyDownloadsDir  string   // where versions before the path migration saved files
	WebviewDirs         []string // WebView storage outside DataDir, see webviewDataDirs
}

func (p Paths) DBPath() string {
	if IsDevBuild {
		return filepath.Join(p.DataDir, "dev.db")
	}
	return filepath.Join(p.DataDir, "ytgrabber.db")
}

func (p Paths) LogPath() string {
	return filepath.Join(p.DataDir, "ytgrabber.log")
}

// WebviewDataDir is where WebView2 keeps its profile on Windows (see
// main.go); inside DataDir so deleting all data has one place to wipe.
func (p Paths) WebviewDataDir() string {
	return filepath.Join(p.DataDir, "webview")
}

// ResolvePaths decides where the database, logs and downloads live.
// IsDevBuild keeps everything inside the project directory for easy
// debugging/cleanup; a real `wails build` is treated as a production
// install and uses the platform's per-user directories.
func ResolvePaths() Paths {
	home := HomeDir()

	if IsDevBuild {
		downloads := AbsOrSelf("downloads")
		return Paths{
			DataDir:             AbsOrSelf("data"),
			DefaultDownloadsDir: downloads,
			LegacyDownloadsDir:  downloads,
			WebviewDirs:         webviewDataDirs(home),
		}
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}

	return Paths{
		DataDir:             AbsOrSelf(filepath.Join(configDir, "ytgrabber")),
		DefaultDownloadsDir: defaultDownloadsDir(home),
		// Earlier releases used ~/Movies on every OS.
		LegacyDownloadsDir: filepath.Join(home, "Movies", "ytgrabber"),
		WebviewDirs:        webviewDataDirs(home),
	}
}

// defaultDownloadsDir picks the platform's conventional video folder:
// ~/Movies on macOS, the XDG videos dir on Linux, ~/Videos elsewhere.
func defaultDownloadsDir(home string) string {
	switch goruntime.GOOS {
	case "darwin":
		return filepath.Join(home, "Movies", "ytgrabber")
	case "windows":
		return filepath.Join(home, "Videos", "ytgrabber")
	default:
		if dir := xdgUserDir(home, "XDG_VIDEOS_DIR"); dir != "" && dir != home {
			return filepath.Join(dir, "ytgrabber")
		}
		return filepath.Join(home, "Videos", "ytgrabber")
	}
}

// xdgUserDir reads a directory from ~/.config/user-dirs.dirs, the file
// xdg-user-dirs maintains (and which localised desktops rely on, e.g.
// "~/Видео" instead of "~/Videos"). Returns "" when it isn't set.
func xdgUserDir(home string, key string) string {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}

	file, err := os.Open(filepath.Join(configHome, "user-dirs.dirs"))
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		name, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !ok || name != key {
			continue
		}
		value = strings.Trim(value, `"`)
		value = strings.Replace(value, "$HOME", home, 1)
		if filepath.IsAbs(value) {
			return filepath.Clean(value)
		}
	}
	return ""
}

// bundleIDs are the macOS bundle identifiers the app has shipped with;
// WebKit keys its storage by them. The first is current (build/darwin/Info.plist).
var bundleIDs = []string{"io.github.d4sh4-ru.ytgrabber", "com.wails.ytgrabber"}

// webviewDataDirs are the WebView storage folders (localStorage, caches,
// cookies) outside DataDir. Every path is specific to the app — named after
// its bundle id or binary — so removing them can't touch anything else.
func webviewDataDirs(home string) []string {
	switch goruntime.GOOS {
	case "darwin":
		var dirs []string
		for _, id := range bundleIDs {
			dirs = append(dirs,
				filepath.Join(home, "Library", "WebKit", id),
				filepath.Join(home, "Library", "Caches", id),
				filepath.Join(home, "Library", "HTTPStorages", id),
				filepath.Join(home, "Library", "HTTPStorages", id+".binarycookies"),
				filepath.Join(home, "Library", "Saved Application State", id+".savedState"),
				filepath.Join(home, "Library", "Preferences", id+".plist"),
			)
		}
		return dirs
	case "windows":
		// Earlier releases used WebView2's default, %AppData%\<exe name>;
		// current ones use Paths.WebviewDataDir.
		if appData := os.Getenv("AppData"); appData != "" {
			return []string{filepath.Join(appData, "ytgrabber.exe")}
		}
		return nil
	default:
		// WebKitGTK stores data under the program name set in main.go.
		dataHome := os.Getenv("XDG_DATA_HOME")
		if dataHome == "" {
			dataHome = filepath.Join(home, ".local", "share")
		}
		cacheHome := os.Getenv("XDG_CACHE_HOME")
		if cacheHome == "" {
			cacheHome = filepath.Join(home, ".cache")
		}
		return []string{filepath.Join(dataHome, "ytgrabber"), filepath.Join(cacheHome, "ytgrabber")}
	}
}
