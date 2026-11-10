package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"ytgrabber/internal/model"
	"ytgrabber/internal/remote"
)

// Connection is which mode the window works in.
type Connection struct {
	// Server is the server the window works with; "" in the local mode.
	Server string `json:"server"`
	// LastServer is the server to offer when connecting again.
	LastServer string        `json:"lastServer"`
	Status     remote.Status `json:"status"`
}

// GetConnection tells the window, before anything else, whether it talks
// to the local core or to a server.
func (a *App) GetConnection() Connection {
	cfg, _ := remote.LoadConfig(a.remoteConfig)
	return Connection{Server: a.remote.Server(), LastServer: cfg.Server, Status: a.remote.Status()}
}

// ConnectServer switches the window to the server at address, once it's
// sure a ytgrabber server answers there. The window reloads afterwards
// and signs in, unless this device already has a token for that server.
func (a *App) ConnectServer(address string) (Connection, error) {
	if err := a.noLocalDownloads(); err != nil {
		return a.GetConnection(), err
	}
	server, err := a.remote.Check(a.ctx, address)
	if err != nil {
		return a.GetConnection(), err
	}
	if err := remote.SaveConfig(a.remoteConfig, remote.Config{Server: server.String(), UseServer: true}); err != nil {
		return a.GetConnection(), fmt.Errorf("не удалось сохранить адрес сервера: %w", err)
	}
	a.remote.Use(server)
	return a.GetConnection(), nil
}

// UseLocal switches the window back to the local library. The device
// stays signed in to the server, to come back without a code; signing
// out is in the server's device list.
func (a *App) UseLocal() (Connection, error) {
	cfg, _ := remote.LoadConfig(a.remoteConfig)
	cfg.UseServer = false
	if err := remote.SaveConfig(a.remoteConfig, cfg); err != nil {
		return a.GetConnection(), fmt.Errorf("не удалось сохранить настройку: %w", err)
	}
	a.remote.Use(nil)
	return a.GetConnection(), nil
}

// ReconnectServer tries the server again now, instead of after the pause
// that follows a failure.
func (a *App) ReconnectServer() {
	a.remote.Reconnect()
}

// SaveMediaFile saves a library file from the server to this computer:
// a WebView can't save a link's file the way a browser does. fileName is
// the name on the server, offered in the save dialog. Cancelling the
// dialog returns false without an error.
func (a *App) SaveMediaFile(mediaURL string, fileName string) (bool, error) {
	link, err := url.Parse(mediaURL)
	if err != nil {
		return false, fmt.Errorf("неверная ссылка на файл: %w", err)
	}
	dest, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:            "Скачать на компьютер",
		DefaultDirectory: existingDirOrHome(a.core.GetSettings(user).DownloadsDir),
		DefaultFilename:  baseName(fileName),
	})
	if err != nil {
		return false, fmt.Errorf("не удалось открыть диалог: %w", err)
	}
	if dest == "" {
		return false, nil
	}
	if err := a.remote.Download(a.ctx, link.Path, dest); err != nil {
		return false, err
	}
	return true, nil
}

// GetLocalAccounts lists the sites connected on this computer, which the
// window can't see while it works with a server, to send them there.
func (a *App) GetLocalAccounts() ([]*model.Account, error) {
	return a.core.GetAccounts(user)
}

// SendAccountToServer connects the server's account of a site with the
// cookies of the same site on this computer: the server has no browsers
// to take them from. The cookies go straight to the server; the window
// gets back the server's account.
func (a *App) SendAccountToServer(id string) (*model.Account, error) {
	if a.remote.Server() == "" {
		return nil, errors.New("приложение не подключено к серверу")
	}
	local, cookies, err := a.core.AccountCookies(user, id)
	if err != nil {
		return nil, err
	}
	reply, err := a.remote.SendCookies(a.ctx, local.Domain, cookies)
	if err != nil {
		return nil, err
	}
	var account model.Account
	if err := json.Unmarshal(reply, &account); err != nil {
		return nil, fmt.Errorf("непонятный ответ сервера: %w", err)
	}
	return &account, nil
}

// noLocalDownloads: switching to a server hides the local queue, so it
// waits until the queue is done rather than let downloads run unseen.
func (a *App) noLocalDownloads() error {
	jobs, err := a.core.GetJobs(user)
	if err != nil {
		return nil // no local data to hide
	}
	active := 0
	for _, job := range jobs {
		if slices.Contains(model.ActiveStatuses, job.Status) {
			active++
		}
	}
	if active > 0 {
		return errors.New("сначала дождитесь окончания загрузок или уберите их: в режиме сервера локальная очередь не видна")
	}
	return nil
}

// baseName is the file name part of a server path, which may be a
// Windows one.
func baseName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	if name == "" || name == "." || name == ".." {
		return "файл"
	}
	return filepath.Clean(name)
}
