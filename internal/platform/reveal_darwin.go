//go:build darwin

package platform

import "os/exec"

func RevealInFileManager(path string) error {
	return exec.Command("open", "-R", path).Run()
}

func OpenInFileManager(dir string) error {
	return exec.Command("open", dir).Run()
}
