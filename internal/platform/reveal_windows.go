//go:build windows

package platform

import (
	"os/exec"
	"syscall"
)

// RevealInFileManager opens Explorer with the file selected. The command
// line is set verbatim because Explorer expects `/select,"C:\a b.mp4"`,
// which Go's default argument quoting ("/select,C:\a b.mp4") breaks for
// paths with spaces. Explorer exits with a non-zero code even on success,
// so the process is started without waiting for its status.
func RevealInFileManager(path string) error {
	cmd := exec.Command("explorer")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer /select,"` + path + `"`}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func OpenInFileManager(dir string) error {
	cmd := exec.Command("explorer", dir)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
