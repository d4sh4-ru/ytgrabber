//go:build windows

package platform

import (
	"os/exec"
	"strconv"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW from the Win32 API: console tools such
// as yt-dlp and ffmpeg would otherwise flash a console window per launch
// because the GUI app has no console for them to inherit.
const createNoWindow = 0x08000000

func ConfigureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

// KillProcessTree uses taskkill /T to take down yt-dlp together with the
// ffmpeg it spawned; Windows has no process groups to signal as a unit.
func KillProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}

	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	ConfigureCommand(kill)
	if err := kill.Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
