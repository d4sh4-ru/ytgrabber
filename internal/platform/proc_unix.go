//go:build !windows

package platform

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// ConfigureCommand puts the child in its own process group so that
// KillProcessTree can take down everything it spawned — yt-dlp runs ffmpeg
// for merging/conversion, and killing only yt-dlp would orphan it.
func ConfigureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killRetries are the pauses before each repeated kill of the group. A child
// the group is forking at the moment of the first kill can escape it (seen on
// macOS under load): it lands in the group after the signal went out, lives
// on and holds the output pipe open, so the caller's read never ends.
var killRetries = []time.Duration{
	5 * time.Millisecond,
	20 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
}

func KillProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// A negative pid addresses the whole process group. The repeats stop
	// once the group is empty; while it isn't, its id can't be reused.
	pgid := -cmd.Process.Pid
	err := syscall.Kill(pgid, syscall.SIGKILL)
	if err != nil {
		return err
	}
	for _, pause := range killRetries {
		time.Sleep(pause)
		if errors.Is(syscall.Kill(pgid, syscall.SIGKILL), syscall.ESRCH) {
			break
		}
	}
	return nil
}
