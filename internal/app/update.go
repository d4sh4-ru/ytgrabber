package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"ytgrabber/internal/accounts"
	"ytgrabber/internal/model"
	"ytgrabber/internal/updater"
)

const (
	updateCheckTimeout    = 20 * time.Second
	updateDownloadTimeout = 15 * time.Minute
	updateCheckInterval   = 24 * time.Hour
	updateProgressEvery   = 250 * time.Millisecond
)

// UpdateInfo is what the UI knows about a newer version.
type UpdateInfo struct {
	CurrentVersion string `json:"currentVersion"`
	Available      bool   `json:"available"`
	Version        string `json:"version"`
	Notes          string `json:"notes"`
	PageURL        string `json:"pageUrl"`
	PublishedAt    string `json:"publishedAt"`
	Prerelease     bool   `json:"prerelease"`
	// CanInstall means the update can be installed from the app;
	// otherwise InstallNote says why and the UI offers the release page.
	CanInstall  bool   `json:"canInstall"`
	InstallNote string `json:"installNote"`
	CheckedAt   string `json:"checkedAt"`
}

// UpdateProgress is sent as "update-progress" while an update installs.
type UpdateProgress struct {
	Stage string `json:"stage"` // downloading, verifying, installing
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

// updateState is the latest check result and an install in progress.
type updateState struct {
	release    *updater.Release
	info       UpdateInfo
	installing bool
	// relaunch is started by shutdown once the app has exited.
	relaunch *updater.Relaunch
}

func newUpdateClient() *updater.Client {
	return &updater.Client{
		HTTP:      &http.Client{Timeout: updateDownloadTimeout},
		UserAgent: "ytgrabber/" + Version,
	}
}

// currentInstall says how this copy of the app can be updated.
func currentInstall() updater.Install {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return updater.Install{Kind: updater.InstallManual, Note: "не удалось определить, где установлено приложение"}
	}
	return updater.Detect(exe, goruntime.GOOS)
}

// GetUpdateInfo возвращает результат последней проверки обновлений.
func (a *App) GetUpdateInfo() UpdateInfo {
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	info := a.update.info
	info.CurrentVersion = Version
	return info
}

// CheckForUpdates спрашивает GitHub о новой версии.
func (a *App) CheckForUpdates() (UpdateInfo, error) {
	if Version == "dev" {
		return a.GetUpdateInfo(), errors.New("в сборке для разработки обновления не проверяются")
	}
	current, err := updater.ParseVersion(Version)
	if err != nil {
		return a.GetUpdateInfo(), fmt.Errorf("версия приложения %q не похожа на X.Y.Z", Version)
	}

	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()
	// A pre-release build is offered newer pre-releases too.
	prerelease := a.GetSettings().PrereleaseUpdates || current.IsPrerelease()
	release, err := a.updater.Latest(ctx, prerelease)
	if err != nil {
		return a.GetUpdateInfo(), err
	}

	info := UpdateInfo{CurrentVersion: Version, CheckedAt: timestamp()}
	if release != nil && release.Version.Compare(current) > 0 {
		info.Available = true
		info.Version = release.Version.String()
		info.Notes = updater.ReleaseNotes(release.Notes)
		info.PageURL = release.PageURL
		info.PublishedAt = release.PublishedAt
		info.Prerelease = release.Prerelease
		info.CanInstall, info.InstallNote = a.canInstall(release)
	} else {
		release = nil
	}

	a.updateMu.Lock()
	a.update.release = release
	a.update.info = info
	a.updateMu.Unlock()
	if info.Available {
		log.Printf("ytgrabber: update %s available (installable: %v %s)", info.Version, info.CanInstall, info.InstallNote)
	}
	return info, nil
}

// canInstall says whether release can be installed from the app.
func (a *App) canInstall(release *updater.Release) (bool, string) {
	inst := a.detectInstall()
	if inst.Kind == updater.InstallManual {
		return false, inst.Note
	}
	if _, ok := updater.ParsePublicKey(updater.PublicKey); !ok {
		return false, "эта сборка не умеет проверять подпись обновлений"
	}
	name := updater.AssetName(release.Version, goruntime.GOOS, goruntime.GOARCH, inst.Kind)
	if _, ok := release.Asset(name); name == "" || !ok {
		return false, "в релизе нет сборки для этой системы"
	}
	if _, ok := release.Asset("SHA256SUMS.txt.sig"); !ok {
		return false, "релиз не подписан"
	}
	return true, ""
}

// InstallUpdate скачивает найденное обновление, проверяет подпись и
// контрольную сумму, ставит его на место текущей версии и перезапускает
// приложение. Пока идут загрузки, обновление не начинается.
func (a *App) InstallUpdate() error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	if a.hasActiveJobs() {
		return errors.New("дождитесь окончания загрузок или отмените их, затем обновите приложение")
	}

	a.updateMu.Lock()
	release := a.update.release
	if release == nil {
		a.updateMu.Unlock()
		return errors.New("обновление не найдено: проверьте ещё раз")
	}
	if a.update.installing {
		a.updateMu.Unlock()
		return errors.New("обновление уже устанавливается")
	}
	a.update.installing = true
	a.updateMu.Unlock()

	relaunch, err := a.downloadAndApply(release)

	a.updateMu.Lock()
	a.update.installing = false
	if err == nil {
		a.update.relaunch = &relaunch
	}
	a.updateMu.Unlock()
	if err != nil {
		log.Printf("ytgrabber: update to %s failed: %v", release.Version, err)
		return err
	}

	log.Printf("ytgrabber: update to %s installed, restarting", release.Version)
	go func() {
		// Let this call's result reach the frontend before the window closes.
		time.Sleep(quitDelay)
		a.quit()
	}()
	return nil
}

func (a *App) downloadAndApply(release *updater.Release) (updater.Relaunch, error) {
	inst := a.detectInstall()
	if ok, note := a.canInstall(release); !ok {
		return updater.Relaunch{}, errors.New(note)
	}
	key, _ := updater.ParsePublicKey(updater.PublicKey)

	workDir := a.paths.UpdateDir()
	if err := removeAppDir(workDir); err != nil {
		return updater.Relaunch{}, err
	}
	if err := accounts.EnsurePrivateDir(workDir); err != nil {
		return updater.Relaunch{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), updateDownloadTimeout)
	defer cancel()
	a.publish("update-progress", UpdateProgress{Stage: "verifying"})
	sums, err := a.updater.VerifiedChecksums(ctx, release, key)
	if err != nil {
		return updater.Relaunch{}, err
	}
	name := updater.AssetName(release.Version, goruntime.GOOS, goruntime.GOARCH, inst.Kind)
	asset, _ := release.Asset(name)
	want, ok := sums[name]
	if !ok {
		return updater.Relaunch{}, fmt.Errorf("%s нет в списке контрольных сумм", name)
	}

	var lastEmit time.Time
	downloaded := filepath.Join(workDir, name)
	err = a.updater.Download(ctx, asset, want, downloaded, func(done, total int64) {
		if now := time.Now(); now.Sub(lastEmit) >= updateProgressEvery || done == total {
			lastEmit = now
			a.publish("update-progress", UpdateProgress{Stage: "downloading", Done: done, Total: total})
		}
	})
	if err != nil {
		return updater.Relaunch{}, err
	}

	a.publish("update-progress", UpdateProgress{Stage: "installing"})
	return updater.Apply(inst, downloaded, workDir)
}

func (a *App) hasActiveJobs() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, j := range a.jobs {
		if slices.Contains(model.ActiveStatuses, j.Status) {
			return true
		}
	}
	return false
}

// OpenReleasePage открывает страницу найденного релиза в браузере.
func (a *App) OpenReleasePage() error {
	url := a.GetUpdateInfo().PageURL
	if url == "" {
		url = "https://github.com/" + updater.Repo + "/releases/latest"
	}
	runtime.BrowserOpenURL(a.ctx, url)
	return nil
}

// SetUpdateSettings включает автоматическую проверку обновлений и
// тестовые (pre-release) версии.
func (a *App) SetUpdateSettings(autoCheck bool, prerelease bool) (Settings, error) {
	if err := a.ensureReady(); err != nil {
		return a.GetSettings(), err
	}
	if _, err := a.updateSettings(func(s *Settings) { s.AutoCheckUpdates = autoCheck }, settingUpdateAutoCheck, boolSetting(autoCheck)); err != nil {
		return a.GetSettings(), err
	}
	return a.updateSettings(func(s *Settings) { s.PrereleaseUpdates = prerelease }, settingUpdatePrerelease, boolSetting(prerelease))
}

// watchForUpdates checks at startup and then daily while the app runs,
// unless the user turned it off; a found update is announced with
// "update-available". Network errors only go to the log.
func (a *App) watchForUpdates(ctx context.Context) {
	if Version == "dev" {
		return
	}
	// What the last update downloaded (the Windows installer has finished
	// by the time the new version starts) and moved aside.
	if err := removeAppDir(a.paths.UpdateDir()); err != nil {
		log.Printf("ytgrabber: %v", err)
	}
	updater.CleanupLeftovers(a.detectInstall())
	ticker := time.NewTicker(updateCheckInterval)
	defer ticker.Stop()
	for {
		if a.GetSettings().AutoCheckUpdates {
			info, err := a.CheckForUpdates()
			switch {
			case err != nil:
				log.Printf("ytgrabber: update check failed: %v", err)
			case info.Available:
				a.publish("update-available", info)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// startRelaunch runs on shutdown, after everything is closed.
func (a *App) startRelaunch() {
	a.updateMu.Lock()
	relaunch := a.update.relaunch
	a.updateMu.Unlock()
	if relaunch == nil {
		return
	}
	if err := updater.Start(*relaunch); err != nil {
		log.Printf("ytgrabber: failed to start the new version: %v", err)
	}
}
