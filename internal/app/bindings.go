package app

import (
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"ytgrabber/internal/accounts"
	"ytgrabber/internal/browsers"
	"ytgrabber/internal/core"
	"ytgrabber/internal/model"
	"ytgrabber/internal/tools"
)

// The methods below are what the frontend calls. They are documented in
// package core; comments here only cover what the adapter adds.

// --- App and tools ---

func (a *App) GetAppInfo() core.AppInfo {
	return a.core.GetAppInfo()
}

func (a *App) GetDependencies() tools.Report {
	return a.core.GetDependencies()
}

func (a *App) UpdateYtDlp() (string, error) {
	return a.core.UpdateYtDlp()
}

func (a *App) RollbackYtDlp() (tools.Report, error) {
	return a.core.RollbackYtDlp()
}

func (a *App) InstallTools(names []string) (tools.Report, error) {
	return a.core.InstallTools(names)
}

func (a *App) CancelToolInstall() {
	a.core.CancelToolInstall()
}

func (a *App) GetStorageInfo() core.StorageInfo {
	return a.core.GetStorageInfo()
}

func (a *App) ResetAllData(deleteMedia bool) error {
	return a.core.ResetAllData(deleteMedia)
}

// --- Settings ---

func (a *App) GetSettings() core.Settings {
	return a.core.GetSettings(user)
}

// ChooseDownloadsDir открывает системный диалог выбора папки загрузок.
// Отмена диалога не считается ошибкой — возвращаются текущие настройки.
func (a *App) ChooseDownloadsDir() (core.Settings, error) {
	current := a.core.GetSettings(user)
	if err := a.core.Ready(); err != nil {
		return current, err
	}

	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "Папка для загрузок",
		DefaultDirectory:     existingDirOrHome(current.DownloadsDir),
		CanCreateDirectories: true,
	})
	if err != nil {
		return current, fmt.Errorf("не удалось открыть диалог: %w", err)
	}
	if dir == "" {
		return current, nil
	}
	return a.core.SetDownloadsDir(user, dir)
}

// ChooseToolPath позволяет указать исполняемый файл yt-dlp или ffmpeg
// вручную, если автоматический поиск его не находит.
func (a *App) ChooseToolPath(tool string) (core.Settings, error) {
	current := a.core.GetSettings(user)
	if err := a.core.Ready(); err != nil {
		return current, err
	}
	if tool != tools.YtDlp && tool != tools.Ffmpeg {
		return current, fmt.Errorf("неизвестный инструмент %q", tool)
	}

	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: fmt.Sprintf("Исполняемый файл %s", tool),
	})
	if err != nil {
		return current, fmt.Errorf("не удалось открыть диалог: %w", err)
	}
	if path == "" {
		return current, nil
	}
	return a.core.SetToolPath(tool, path)
}

func (a *App) ResetToolPath(tool string) (core.Settings, error) {
	return a.core.ResetToolPath(tool)
}

func (a *App) SetMaxConcurrentDownloads(n int) (core.Settings, error) {
	return a.core.SetMaxConcurrentDownloads(n)
}

func (a *App) OpenDownloadsDir() error {
	return a.core.OpenDownloadsDir(user)
}

// --- Updates ---

func (a *App) GetUpdateInfo() core.UpdateInfo {
	return a.core.GetUpdateInfo()
}

func (a *App) CheckForUpdates() (core.UpdateInfo, error) {
	return a.core.CheckForUpdates()
}

func (a *App) InstallUpdate() error {
	return a.core.InstallUpdate()
}

// OpenReleasePage открывает страницу найденного релиза в браузере.
func (a *App) OpenReleasePage() error {
	runtime.BrowserOpenURL(a.ctx, a.core.ReleasePageURL())
	return nil
}

func (a *App) SetUpdateSettings(autoCheck bool, prerelease bool) (core.Settings, error) {
	return a.core.SetUpdateSettings(autoCheck, prerelease)
}

// --- Downloads ---

func (a *App) DownloadVideo(rawURL string, quality string, useAccount bool) (string, error) {
	return a.core.DownloadVideo(user, rawURL, quality, useAccount)
}

func (a *App) RetryJob(id string, useAccount bool) error {
	return a.core.RetryJob(user, id, useAccount)
}

func (a *App) RemoveJob(id string) error {
	return a.core.RemoveJob(user, id)
}

func (a *App) GetJobs() ([]model.DownloadJob, error) {
	return a.core.GetJobs(user)
}

// --- Library ---

func (a *App) GetLibrary() ([]*model.VideoRecord, error) {
	return a.core.GetLibrary(user)
}

func (a *App) DeleteVideo(id string, deleteFiles bool) error {
	return a.core.DeleteVideo(user, id, deleteFiles)
}

func (a *App) SavePlaybackPosition(id string, seconds float64) (float64, error) {
	return a.core.SavePlaybackPosition(user, id, seconds)
}

func (a *App) RevealVideo(id string) error {
	return a.core.RevealVideo(user, id)
}

// --- Accounts ---

func (a *App) GetAccounts() ([]*model.Account, error) {
	return a.core.GetAccounts(user)
}

// ImportCookiesFile открывает диалог выбора файла cookies.txt и
// подключает из него аккаунт сайта. Отмена диалога возвращает nil без
// ошибки.
func (a *App) ImportCookiesFile(site string) (*model.Account, error) {
	if err := a.core.Ready(); err != nil {
		return nil, err
	}
	domain, err := accounts.SiteFromInput(site)
	if err != nil {
		return nil, err
	}

	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: fmt.Sprintf("Файл cookies.txt для %s", domain),
		Filters: []runtime.FileFilter{
			{DisplayName: "cookies.txt (*.txt)", Pattern: "*.txt"},
			{DisplayName: "Все файлы", Pattern: "*"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть диалог: %w", err)
	}
	if path == "" {
		return nil, nil
	}
	return a.core.ImportCookiesFile(user, domain, path)
}

func (a *App) RemoveAccount(id string) error {
	return a.core.RemoveAccount(user, id)
}

func (a *App) CheckAccount(id string) (*model.Account, error) {
	return a.core.CheckAccount(user, id)
}

func (a *App) ScanBrowsers(site string) ([]browsers.Match, error) {
	return a.core.ScanBrowsers(site)
}

func (a *App) ConnectBrowserProfile(site string, browserID string, profileDir string) (*model.Account, error) {
	return a.core.ConnectBrowserProfile(user, site, browserID, profileDir)
}

func (a *App) RefreshAccount(id string) (*model.Account, error) {
	return a.core.RefreshAccount(user, id)
}
