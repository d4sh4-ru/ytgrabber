// Package remote makes the desktop app a client of ytgrabber-server: the
// window keeps calling its own origin, and this package forwards the API
// and the library files to the server with the device's token, and the
// server's events back into the window. The token lives in the system's
// credential store and never reaches the frontend.
package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Config is which server the desktop works with. It's kept in a file of
// its own, next to the database, so the choice is known before anything
// else starts.
type Config struct {
	// Server is the last server connected to; it stays when the app goes
	// back to the local mode, as does its token, to come back without
	// signing in again.
	Server string `json:"server,omitempty"`
	// UseServer: the window works with Server, not the local core.
	UseServer bool `json:"useServer,omitempty"`
}

// LoadConfig reads path; a missing file is the local mode.
func LoadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("%s повреждён: %w", path, err)
	}
	return cfg, nil
}

// SaveConfig writes cfg to path, replacing it in one step.
func SaveConfig(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ParseServer turns what the user typed into the server's base URL:
// scheme, host and an optional path prefix (a server behind a proxy at
// /ytgrabber), without a trailing slash. Without a scheme it's https;
// Check falls back to http if the server doesn't speak it.
func ParseServer(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("укажите адрес сервера")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("не похоже на адрес сервера: %s", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("адрес должен начинаться с http:// или https://")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery, u.Fragment, u.User = "", "", nil
	return u, nil
}
