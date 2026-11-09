package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
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

// UpdateYtDlp обновляет yt-dlp и возвращает, что получилось. Копию,
// которую поставило приложение, обновляет установщик: последняя версия
// канала с проверкой контрольной суммы, предыдущая остаётся для отката.
// yt-dlp из PATH или по своему пути обновляется сам (yt-dlp -U).
func (a *App) UpdateYtDlp() (string, error) {
	current := a.ytDlpStatus()
	if !current.Found || !current.Managed {
		a.mu.RLock()
		toolSet := a.tools
		a.mu.RUnlock()
		return toolSet.UpdateYtDlp()
	}

	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	latest, err := a.installer.LatestYtDlp(ctx)
	if err != nil {
		return "", err
	}
	// Asking for an update is asking for this version, rolled back or not.
	a.setSkippedYtDlp("")
	if latest == current.Version {
		return fmt.Sprintf("Установлена последняя версия yt-dlp: %s (%s)", latest, a.channel()), nil
	}
	if _, err := a.InstallTools([]string{tools.YtDlp}); err != nil {
		return "", err
	}
	return fmt.Sprintf("yt-dlp обновлён: %s → %s (%s)", current.Version, latest, a.channel()), nil
}

// UpdateYtDlpIfOutdated installs the newest yt-dlp of the channel when
// the copy the app installed itself is older, and returns the version in
// use afterwards. A yt-dlp from PATH or a custom path is left alone: it
// is updated by whoever installed it, and so is a version the user rolled
// back from. Downloads already running finish with the old file (it is
// replaced by a rename); the next ones take the new one.
func (a *App) UpdateYtDlpIfOutdated(ctx context.Context) (version string, updated bool, err error) {
	current := a.ytDlpStatus()
	if !current.Found || !current.Managed {
		return current.Version, false, nil
	}

	latest, err := a.installer.LatestYtDlp(ctx)
	if err != nil {
		return current.Version, false, err
	}
	if latest == current.Version {
		return current.Version, false, nil
	}
	if latest == a.skippedYtDlp() {
		log.Printf("ytgrabber: yt-dlp %s was rolled back, staying on %s", latest, current.Version)
		return current.Version, false, nil
	}

	log.Printf("ytgrabber: updating yt-dlp %s → %s (%s)", current.Version, latest, a.channel())
	if _, err := a.InstallTools([]string{tools.YtDlp}); err != nil {
		return current.Version, false, err
	}
	return latest, true, nil
}

// RollbackYtDlp возвращает версию yt-dlp, которую заменило последнее
// обновление, — на случай, если новая сломала какой-то сайт. Повторный
// откат возвращает новую. Автообновление больше не ставит версию, с
// которой откатились; кнопка «Обновить» ставит.
func (a *App) RollbackYtDlp() (tools.Report, error) {
	current := a.ytDlpStatus()
	if !current.Found || !current.Managed {
		return a.GetDependencies(), errors.New("откатить можно только yt-dlp, установленный приложением")
	}

	a.installMu.Lock()
	defer a.installMu.Unlock()
	if a.cancelInstall != nil {
		return a.GetDependencies(), errors.New("сейчас идёт установка, попробуйте после неё")
	}
	if err := a.installer.Rollback(tools.YtDlp); err != nil {
		return a.GetDependencies(), err
	}

	report := a.GetDependencies()
	for _, status := range report.Tools {
		if status.Name == tools.YtDlp {
			log.Printf("ytgrabber: yt-dlp rolled back %s → %s", current.Version, status.Version)
			// The version just rolled forward to is no longer skipped.
			if status.Version == a.skippedYtDlp() {
				a.setSkippedYtDlp("")
			} else {
				a.setSkippedYtDlp(current.Version)
			}
		}
	}
	return report, nil
}

func (a *App) ytDlpStatus() tools.Status {
	for _, status := range a.GetDependencies().Tools {
		if status.Name == tools.YtDlp {
			return status
		}
	}
	return tools.Status{}
}

// skippedYtDlp is the yt-dlp version the user rolled back from; automatic
// updates don't install it again. It lives next to the tools it is about.
func (a *App) skippedYtDlp() string {
	data, _ := os.ReadFile(a.skippedYtDlpPath())
	return strings.TrimSpace(string(data))
}

func (a *App) setSkippedYtDlp(version string) {
	path := a.skippedYtDlpPath()
	if version == "" {
		os.Remove(path)
		return
	}
	if err := os.WriteFile(path, []byte(version+"\n"), 0o644); err != nil {
		log.Printf("ytgrabber: remembering the rolled back yt-dlp: %v", err)
	}
}

func (a *App) skippedYtDlpPath() string {
	return filepath.Join(a.paths.ToolsDir(), "yt-dlp.skip")
}

func (a *App) channel() installer.Channel {
	if a.installer.Channel == "" {
		return installer.Stable
	}
	return a.installer.Channel
}
