//go:build windows

package updater

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const createNoWindow = 0x08000000

// Start launches the new version; call it as the app exits.
//
// For an installed app it runs the installer silently first, through
// cmd's `start`: the installer asks for administrator rights, which only
// ShellExecute (behind `start`) can request — CreateProcess would just fail.
// The short ping gives this process time to exit and release its files;
// /D keeps the installation where it is (NSIS wants it last and unquoted).
func Start(r Relaunch) error {
	if r.Kind != InstallWindowsSetup {
		return exec.Command(r.Path).Start()
	}

	shell := os.Getenv("ComSpec")
	if shell == "" {
		shell = "cmd.exe"
	}
	script := `ping -n 3 127.0.0.1 >nul & start "" /wait "` + r.Installer + `" /S /D=` + filepath.Dir(r.Path) +
		` & start "" "` + r.Path + `"`
	cmd := exec.Command(shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// The whole command line, program included. /S: cmd strips only
		// the outer quotes and runs the rest as is.
		CmdLine:       `"` + shell + `" /S /C "` + script + `"`,
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	return cmd.Start()
}
