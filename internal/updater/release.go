// Package updater keeps the app up to date from its GitHub releases: it
// finds a newer release, downloads the build for this platform, checks it
// against the release's signed checksum list and swaps it in.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Repo is where releases are published (.github/workflows/release.yml).
const Repo = "d4sh4-ru/ytgrabber"

const (
	checksumsAsset = "SHA256SUMS.txt"
	signatureAsset = checksumsAsset + ".sig"
	// maxAPIResponse bounds the release list read from the API.
	maxAPIResponse = 4 << 20
)

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type Release struct {
	Tag         string  `json:"tag_name"`
	Name        string  `json:"name"`
	Notes       string  `json:"body"`
	PageURL     string  `json:"html_url"`
	Draft       bool    `json:"draft"`
	Prerelease  bool    `json:"prerelease"`
	PublishedAt string  `json:"published_at"`
	Assets      []Asset `json:"assets"`

	Version Version `json:"-"`
}

func (r *Release) Asset(name string) (Asset, bool) {
	for _, asset := range r.Assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return Asset{}, false
}

// Client talks to the GitHub API and downloads release assets.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// APIURL is the release list endpoint; tests point it at a fake.
	APIURL string
}

func (c *Client) apiURL() string {
	if c.APIURL != "" {
		return c.APIURL
	}
	return "https://api.github.com/repos/" + Repo + "/releases?per_page=20"
}

// Latest returns the newest published release, skipping pre-releases
// unless includePrerelease is set; nil if there is none.
func (c *Client) Latest(ctx context.Context, includePrerelease bool) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("не удалось связаться с GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, errors.New("GitHub временно ограничил запросы, попробуйте позже")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub ответил %s", resp.Status)
	}

	var releases []Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPIResponse)).Decode(&releases); err != nil {
		return nil, fmt.Errorf("не удалось разобрать ответ GitHub: %w", err)
	}

	var latest *Release
	for i := range releases {
		release := &releases[i]
		if release.Draft || (release.Prerelease && !includePrerelease) {
			continue
		}
		version, err := ParseVersion(release.Tag)
		if err != nil {
			continue // not a version tag
		}
		release.Version = version
		if latest == nil || version.Compare(latest.Version) > 0 {
			latest = release
		}
	}
	return latest, nil
}

// InstallKind is how this copy of the app was installed, which decides
// how an update is applied.
type InstallKind string

const (
	// InstallManual: the app can't replace itself (unknown layout, no
	// write access, development build); the user downloads the update.
	InstallManual InstallKind = "manual"
	// InstallMacApp: ytgrabber.app in a folder the user can write to.
	InstallMacApp InstallKind = "macos-app"
	// InstallWindowsSetup: installed by the NSIS installer, which is run
	// again silently.
	InstallWindowsSetup InstallKind = "windows-setup"
	// InstallWindowsPortable / InstallLinuxBinary: a single executable
	// that is replaced in place.
	InstallWindowsPortable InstallKind = "windows-portable"
	InstallLinuxBinary     InstallKind = "linux-binary"
)

// AssetName is the release file for a platform and install kind, as
// .github/workflows/release.yml names them; "" if there is none.
func AssetName(version Version, goos, goarch string, kind InstallKind) string {
	base := fmt.Sprintf("ytgrabber-%s-", version)
	switch {
	case goos == "darwin" && goarch == "arm64":
		return base + "macos-arm64.zip"
	case goos == "windows" && (goarch == "amd64" || goarch == "arm64"):
		if kind == InstallWindowsPortable {
			return base + "windows-" + goarch + "-portable.exe"
		}
		return base + "windows-" + goarch + "-setup.exe"
	case goos == "linux" && (goarch == "amd64" || goarch == "arm64"):
		return base + "linux-" + goarch + ".tar.gz"
	}
	return ""
}

// ReleaseNotes trims the installation table the release workflow puts in
// front of the generated changelog: the user is updating, not installing.
func ReleaseNotes(body string) string {
	if i := strings.Index(body, "## What's Changed"); i >= 0 {
		return strings.TrimSpace(body[i:])
	}
	return strings.TrimSpace(body)
}
