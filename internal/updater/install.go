package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Install describes this copy of the app and how to update it.
type Install struct {
	Kind InstallKind
	// Target is what an update replaces: the .app bundle, the executable,
	// or (for the Windows installer) the executable inside the install
	// directory.
	Target string
	// Note says why Kind is InstallManual.
	Note string
}

const (
	macOldPrefix = ".ytgrabber-old-"
	oldSuffix    = ".old"
)

// Detect works out how the app at exe (os.Executable, symlinks resolved)
// was installed on goos.
func Detect(exe string, goos string) Install {
	switch goos {
	case "darwin":
		marker := ".app" + string(filepath.Separator) + filepath.Join("Contents", "MacOS") + string(filepath.Separator)
		i := strings.Index(exe, marker)
		if i < 0 {
			return manual("приложение запущено не из ytgrabber.app")
		}
		bundle := exe[:i+len(".app")]
		if strings.Contains(bundle, "/AppTranslocation/") {
			return manual("macOS запустил приложение из временной копии: перенесите ytgrabber.app в «Программы» и запустите снова")
		}
		if !writableDir(filepath.Dir(bundle)) {
			return manual("нет прав на запись в папку " + filepath.Dir(bundle))
		}
		return Install{Kind: InstallMacApp, Target: bundle}

	case "windows":
		dir := filepath.Dir(exe)
		if fileExists(filepath.Join(dir, "uninstall.exe")) {
			return Install{Kind: InstallWindowsSetup, Target: exe}
		}
		if !writableDir(dir) {
			return manual("нет прав на запись в папку " + dir)
		}
		return Install{Kind: InstallWindowsPortable, Target: exe}

	case "linux":
		if os.Getenv("APPIMAGE") != "" {
			return manual("AppImage обновляется заменой файла вручную")
		}
		if !writableDir(filepath.Dir(exe)) {
			return manual("нет прав на запись в папку " + filepath.Dir(exe) + " (приложение установлено системой)")
		}
		return Install{Kind: InstallLinuxBinary, Target: exe}
	}
	return manual("автообновление не поддерживается на этой системе")
}

func manual(note string) Install {
	return Install{Kind: InstallManual, Note: note}
}

// Relaunch is what to start once the app has exited: the new version, or
// for InstallWindowsSetup the installer that then starts it.
type Relaunch struct {
	Kind      InstallKind
	Path      string // the app to start
	Installer string // InstallWindowsSetup only
}

// Apply puts the downloaded update (an asset named by AssetName) in place
// of inst.Target. The running app keeps working off the old files until
// it exits; the old copy is removed on the next start (CleanupLeftovers).
// workDir is a private scratch directory.
func Apply(inst Install, downloaded string, workDir string) (Relaunch, error) {
	switch inst.Kind {
	case InstallWindowsSetup:
		// The installer can't overwrite the running executable: it runs
		// after the app has exited (see Start).
		return Relaunch{Kind: inst.Kind, Path: inst.Target, Installer: downloaded}, nil

	case InstallWindowsPortable:
		if err := replaceFile(inst.Target, downloaded, true); err != nil {
			return Relaunch{}, err
		}
		return Relaunch{Kind: inst.Kind, Path: inst.Target}, nil

	case InstallLinuxBinary:
		binary := filepath.Join(workDir, "ytgrabber")
		if err := extractTarFile(downloaded, "ytgrabber", binary); err != nil {
			return Relaunch{}, err
		}
		if err := replaceFile(inst.Target, binary, false); err != nil {
			return Relaunch{}, err
		}
		return Relaunch{Kind: inst.Kind, Path: inst.Target}, nil

	case InstallMacApp:
		if err := replaceBundle(inst.Target, downloaded); err != nil {
			return Relaunch{}, err
		}
		return Relaunch{Kind: inst.Kind, Path: inst.Target}, nil
	}
	return Relaunch{}, errors.New(inst.Note)
}

// replaceFile swaps in a new executable. On Windows a running executable
// can't be overwritten but can be renamed, so it moves aside first (keepOld).
func replaceFile(target string, source string, keepOld bool) error {
	staged := target + ".new"
	if err := copyFile(source, staged, 0o755); err != nil {
		return fmt.Errorf("не удалось подготовить новую версию: %w", err)
	}
	if keepOld {
		old := target + oldSuffix
		os.Remove(old)
		if err := os.Rename(target, old); err != nil {
			os.Remove(staged)
			return fmt.Errorf("не удалось заменить %s: %w", target, err)
		}
		if err := os.Rename(staged, target); err != nil {
			os.Rename(old, target)
			os.Remove(staged)
			return fmt.Errorf("не удалось заменить %s: %w", target, err)
		}
		return nil
	}
	if err := os.Rename(staged, target); err != nil {
		os.Remove(staged)
		return fmt.Errorf("не удалось заменить %s: %w", target, err)
	}
	return nil
}

// replaceBundle unpacks the release zip next to the bundle (renames only
// work within one volume) and swaps the bundles.
func replaceBundle(bundle string, zipPath string) error {
	parent := filepath.Dir(bundle)
	stage, err := os.MkdirTemp(parent, ".ytgrabber-update-")
	if err != nil {
		return fmt.Errorf("нет прав на запись в папку %s: %w", parent, err)
	}
	defer os.RemoveAll(stage)

	if err := extractZip(zipPath, stage); err != nil {
		return err
	}
	staged := filepath.Join(stage, filepath.Base(bundle))
	if _, err := os.Stat(filepath.Join(staged, "Contents", "MacOS")); err != nil {
		return fmt.Errorf("в архиве обновления нет %s", filepath.Base(bundle))
	}

	old, err := os.MkdirTemp(parent, macOldPrefix)
	if err != nil {
		return err
	}
	oldBundle := filepath.Join(old, filepath.Base(bundle))
	if err := os.Rename(bundle, oldBundle); err != nil {
		os.Remove(old)
		return fmt.Errorf("не удалось заменить %s: %w", bundle, err)
	}
	if err := os.Rename(staged, bundle); err != nil {
		os.Rename(oldBundle, bundle)
		os.RemoveAll(old)
		return fmt.Errorf("не удалось заменить %s: %w", bundle, err)
	}
	return nil
}

// CleanupLeftovers removes what a previous update moved aside.
func CleanupLeftovers(inst Install) {
	switch inst.Kind {
	case InstallWindowsPortable, InstallWindowsSetup:
		os.Remove(inst.Target + oldSuffix)
	case InstallMacApp:
		entries, err := os.ReadDir(filepath.Dir(inst.Target))
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), macOldPrefix) {
				os.RemoveAll(filepath.Join(filepath.Dir(inst.Target), entry.Name()))
			}
		}
		// A crash mid-update can leave a staging directory too.
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), ".ytgrabber-update-") {
				os.RemoveAll(filepath.Join(filepath.Dir(inst.Target), entry.Name()))
			}
		}
	}
}

const maxExtractedSize = 1 << 30

// extractZip unpacks a zip made by `ditto -c -k --keepParent`, keeping
// executable bits and symlinks, skipping macOS resource-fork entries and
// refusing paths that would escape dest.
func extractZip(zipPath string, dest string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("архив обновления повреждён: %w", err)
	}
	defer reader.Close()

	var written int64
	for _, file := range reader.File {
		name := file.Name
		if strings.HasPrefix(name, "__MACOSX/") || strings.HasPrefix(filepath.Base(name), "._") {
			continue
		}
		path, err := safeJoin(dest, name)
		if err != nil {
			return err
		}
		mode := file.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case mode&os.ModeSymlink != 0:
			link, err := readZipEntry(file, 4096)
			if err != nil {
				return err
			}
			if filepath.IsAbs(string(link)) || strings.Contains(string(link), "..") {
				return fmt.Errorf("недопустимая ссылка в архиве: %s", name)
			}
			os.MkdirAll(filepath.Dir(path), 0o755)
			if err := os.Symlink(string(link), path); err != nil {
				return err
			}
		default:
			written += int64(file.UncompressedSize64)
			if written > maxExtractedSize {
				return errors.New("архив обновления слишком большой")
			}
			if err := writeZipEntry(file, path, mode.Perm()|0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func readZipEntry(file *zip.File, limit int64) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, limit))
}

func writeZipEntry(file *zip.File, path string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(rc, maxExtractedSize)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// extractTarFile writes the file named name (in any directory of the
// archive) from a .tar.gz to dest.
func extractTarFile(archive string, name string, dest string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("архив обновления повреждён: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("в архиве обновления нет %s", name)
		}
		if err != nil {
			return fmt.Errorf("архив обновления повреждён: %w", err)
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != name {
			continue
		}
		out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(tr, maxExtractedSize)); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	}
}

func safeJoin(dest string, name string) (string, error) {
	path := filepath.Join(dest, filepath.FromSlash(name))
	if path != dest && !strings.HasPrefix(path, dest+string(filepath.Separator)) {
		return "", fmt.Errorf("недопустимый путь в архиве: %s", name)
	}
	return path, nil
}

func copyFile(src string, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return os.Chmod(dst, mode)
}

func writableDir(dir string) bool {
	probe, err := os.CreateTemp(dir, ".ytgrabber-write-test-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	probe.Close()
	os.Remove(name)
	return true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
