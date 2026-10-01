package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"ytgrabber/internal/installer"
	"ytgrabber/internal/tools"
)

const installTimeout = 15 * time.Minute

// installName maps a tool to what installer.Install takes: ffprobe ships
// in the ffmpeg download.
func installName(tool string) string {
	if tool == tools.Ffprobe {
		return tools.Ffmpeg
	}
	return tool
}

// withInstallInfo marks the tools the app can download for this platform.
func withInstallInfo(report tools.Report) tools.Report {
	for i := range report.Tools {
		name := installName(report.Tools[i].Name)
		if slices.Contains(installer.Tools, name) {
			report.Tools[i].Installable = true
			report.Tools[i].InstallSource = installer.Source(name)
		}
	}
	return report
}

// InstallTools скачивает указанные программы (yt-dlp, ffmpeg, deno) в папку
// приложения — без прав администратора и пакетных менеджеров. Пустой список
// означает «всё, чего не хватает». Ход установки приходит событиями
// "tool-install-progress"; возвращается обновлённый список программ.
func (a *App) InstallTools(names []string) (tools.Report, error) {
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()

	a.installMu.Lock()
	if a.cancelInstall != nil {
		a.installMu.Unlock()
		return a.GetDependencies(), errors.New("установка уже идёт")
	}
	a.cancelInstall = cancel
	a.installMu.Unlock()
	defer func() {
		a.installMu.Lock()
		a.cancelInstall = nil
		a.installMu.Unlock()
	}()

	queue, err := a.installQueue(names)
	if err != nil {
		return a.GetDependencies(), err
	}

	var failures []string
	for _, name := range queue {
		log.Printf("ytgrabber: installing %s into %s", name, a.paths.ToolsDir())
		err := a.installer.Install(ctx, name, func(progress installer.Progress) {
			a.publish("tool-install-progress", progress)
		})
		if err != nil {
			if ctx.Err() != nil {
				err = errors.New("установка отменена")
			}
			log.Printf("ytgrabber: installing %s failed: %v", name, err)
			a.publish("tool-install-progress", installer.Progress{Tool: name, Stage: installer.StageFailed, Error: err.Error()})
			failures = append(failures, fmt.Sprintf("%s: %v", name, err))
			if ctx.Err() != nil {
				break
			}
		}
	}

	report := a.GetDependencies()
	if len(failures) > 0 {
		return report, errors.New(strings.Join(failures, "\n"))
	}
	return report, nil
}

// CancelToolInstall прерывает идущую установку программ.
func (a *App) CancelToolInstall() {
	a.installMu.Lock()
	defer a.installMu.Unlock()
	if a.cancelInstall != nil {
		a.cancelInstall()
	}
}

// installQueue validates the requested names (or picks every missing tool)
// and dedupes them, ffprobe folding into ffmpeg.
func (a *App) installQueue(names []string) ([]string, error) {
	if len(names) == 0 {
		a.mu.RLock()
		for _, status := range a.tools.List() {
			// A broken custom path would still win over an installed copy.
			if !status.Found && !status.Custom {
				names = append(names, status.Name)
			}
		}
		a.mu.RUnlock()
	}

	var queue []string
	for _, name := range names {
		name = installName(name)
		if !slices.Contains(installer.Tools, name) {
			return nil, fmt.Errorf("неизвестная программа %q", name)
		}
		if !slices.Contains(queue, name) {
			queue = append(queue, name)
		}
	}
	if len(queue) == 0 {
		return nil, errors.New("всё уже установлено")
	}
	return queue, nil
}
