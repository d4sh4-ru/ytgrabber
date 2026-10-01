package platform

import (
	"io/fs"
	"os"
	"path/filepath"
	goruntime "runtime"
)

// Name is "<os>/<arch>", shown in the settings screen and logs.
func Name() string {
	return goruntime.GOOS + "/" + goruntime.GOARCH
}

func HomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

func AbsOrSelf(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// ExecutableName adds ".exe" on Windows.
func ExecutableName(name string) string {
	if goruntime.GOOS == "windows" && filepath.Ext(name) == "" {
		return name + ".exe"
	}
	return name
}

func IsExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if goruntime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}

// IsSafeToRemove refuses obviously wrong deletion targets — relative
// paths, the filesystem root, the home and per-user config/cache folders —
// as a last line of defence against a misconfigured path wiping something
// that isn't the app's.
func IsSafeToRemove(path string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(path)
	if clean == filepath.Dir(clean) { // filesystem root
		return false
	}

	protected := []string{HomeDir()}
	for _, dir := range []func() (string, error){os.UserConfigDir, os.UserCacheDir} {
		if value, err := dir(); err == nil {
			protected = append(protected, value)
		}
	}
	for _, dir := range protected {
		if dir != "" && filepath.Clean(dir) == clean {
			return false
		}
	}
	return true
}

// PathSize is the total size of the regular files under root (or of root
// itself, if it is a file).
func PathSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}
