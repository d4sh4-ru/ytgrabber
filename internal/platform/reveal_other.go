//go:build !darwin && !windows

package platform

import (
	"net/url"
	"os/exec"
	"path/filepath"
)

// RevealInFileManager asks the desktop's file manager to highlight the file
// through the freedesktop FileManager1 D-Bus interface (Nautilus, Dolphin,
// Nemo, Thunar…) and falls back to just opening the containing folder.
func RevealInFileManager(path string) error {
	fileURI := (&url.URL{Scheme: "file", Path: path}).String()
	err := exec.Command(
		"dbus-send", "--session", "--print-reply",
		"--dest=org.freedesktop.FileManager1",
		"--type=method_call",
		"/org/freedesktop/FileManager1",
		"org.freedesktop.FileManager1.ShowItems",
		"array:string:"+fileURI,
		"string:",
	).Run()
	if err == nil {
		return nil
	}
	return OpenInFileManager(filepath.Dir(path))
}

func OpenInFileManager(dir string) error {
	cmd := exec.Command("xdg-open", dir)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
