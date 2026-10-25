//go:build !windows

package updater

import (
	"os/exec"
	"syscall"
)

// Start launches the new version; call it as the app exits. On macOS the
// bundle is opened through Launch Services, as Finder would.
func Start(r Relaunch) error {
	var cmd *exec.Cmd
	if r.Kind == InstallMacApp {
		cmd = exec.Command("open", "-n", r.Path)
	} else {
		cmd = exec.Command(r.Path)
	}
	// Its own session, so it outlives this process and its terminal.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
