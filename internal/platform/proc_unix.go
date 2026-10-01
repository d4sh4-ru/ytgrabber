//go:build !windows

package platform

import (
	"os/exec"
	"syscall"
)

// ConfigureCommand puts the child in its own process group so that
// KillProcessTree can take down everything it spawned — yt-dlp runs ffmpeg
// for merging/conversion, and killing only yt-dlp would orphan it.
func ConfigureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func KillProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// A negative pid addresses the whole process group.
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
