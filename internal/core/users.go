package core

import (
	"errors"
	"fmt"
	"os"

	"ytgrabber/internal/storage"
)

// Users gives the server the database of its users, devices and one-time
// codes; who may sign in is the server's business, not the core's. The
// desktop has no users.
func (a *App) Users() (*storage.DB, error) {
	if err := a.ensureReady(); err != nil {
		return nil, err
	}
	return a.db, nil
}

// DeleteUserData removes everything user has: queued and running
// downloads, the library with its files, the connected site accounts and,
// for anyone but the local user, their downloads folder.
func (a *App) DeleteUserData(user UserID) error {
	if err := a.ensureReady(); err != nil {
		return err
	}
	var errs []error

	jobs, err := a.GetJobs(user)
	errs = append(errs, err)
	for _, job := range jobs {
		errs = append(errs, a.RemoveJob(user, job.ID))
	}

	videos, err := a.db.LoadVideos(string(user))
	errs = append(errs, err)
	for _, video := range videos {
		errs = append(errs, a.DeleteVideo(user, video.ID, true))
	}

	accounts, err := a.db.LoadAccounts(string(user))
	errs = append(errs, err)
	for _, account := range accounts {
		errs = append(errs, a.RemoveAccount(user, account.ID))
	}

	if user != LocalUser {
		a.mu.RLock()
		dir := a.downloadsDirLocked(user)
		a.mu.RUnlock()
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, fmt.Errorf("не удалось удалить папку %s: %w", dir, err))
		}
	}
	return errors.Join(errs...)
}
