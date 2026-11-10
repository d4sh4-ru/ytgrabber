package remote

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// Secrets keeps device tokens, one per server address.
type Secrets interface {
	// Get returns the token for server; "" without an error when there is none.
	Get(server string) (string, error)
	Set(server string, token string) error
	// Delete forgets the token for server; a missing one isn't an error.
	Delete(server string) error
}

// keyringService is how the tokens are labelled in the credential store.
const keyringService = "ytgrabber"

// SystemSecrets keeps the tokens in the system's credential store:
// Keychain on macOS, Credential Manager on Windows, Secret Service
// (GNOME Keyring, KWallet) on Linux.
type SystemSecrets struct{}

func (SystemSecrets) Get(server string) (string, error) {
	token, err := keyring.Get(keyringService, server)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("системное хранилище паролей недоступно: %w", err)
	}
	return token, nil
}

func (SystemSecrets) Set(server string, token string) error {
	if err := keyring.Set(keyringService, server, token); err != nil {
		return fmt.Errorf("не удалось сохранить вход в системном хранилище паролей: %w", err)
	}
	return nil
}

func (SystemSecrets) Delete(server string) error {
	err := keyring.Delete(keyringService, server)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("не удалось удалить вход из системного хранилища паролей: %w", err)
	}
	return nil
}
