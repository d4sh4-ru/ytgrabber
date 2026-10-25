package browsers

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// chromiumProfiles lists the profiles of a Chromium "User Data" directory,
// named as the browser shows them (from Local State). Opera keeps its
// only profile in the root directory itself.
func chromiumProfiles(browser Browser, dir string) []Profile {
	names := map[string]string{}
	if data, err := os.ReadFile(filepath.Join(dir, "Local State")); err == nil {
		var state struct {
			Profile struct {
				InfoCache map[string]struct {
					Name string `json:"name"`
				} `json:"info_cache"`
			} `json:"profile"`
		}
		if json.Unmarshal(data, &state) == nil {
			for subdir, info := range state.Profile.InfoCache {
				names[subdir] = info.Name
			}
		}
	}
	if _, ok := names["Default"]; !ok {
		names["Default"] = ""
	}

	subdirs := make([]string, 0, len(names))
	for subdir := range names {
		subdirs = append(subdirs, subdir)
	}
	sort.Strings(subdirs)

	var profiles []Profile
	for _, subdir := range subdirs {
		profileDir := filepath.Join(dir, filepath.Base(subdir))
		if cookies := chromiumCookiesPath(profileDir); cookies != "" {
			name := names[subdir]
			if name == "" {
				name = subdir
			}
			profiles = append(profiles, Profile{Browser: browser, Name: name, Dir: profileDir, CookiesPath: cookies, RootDir: dir})
		}
	}
	if len(profiles) == 0 {
		if cookies := chromiumCookiesPath(dir); cookies != "" {
			profiles = append(profiles, Profile{Browser: browser, Name: "Default", Dir: dir, CookiesPath: cookies, RootDir: dir})
		}
	}
	return profiles
}

// chromiumCookiesPath finds the cookie database of a profile: Network/
// since Chrome 96, the profile root before.
func chromiumCookiesPath(profileDir string) string {
	for _, candidate := range []string{filepath.Join(profileDir, "Network", "Cookies"), filepath.Join(profileDir, "Cookies")} {
		if isFile(candidate) {
			return candidate
		}
	}
	return ""
}

// chromiumEpochOffset is the number of microseconds between 1601-01-01
// (Chromium's epoch) and 1970-01-01.
const chromiumEpochOffset = 11_644_473_600 * 1_000_000

func chromiumNames(db *sql.DB) ([]cookieName, error) {
	rows, err := db.Query(`SELECT host_key, name, last_access_utc FROM cookies`)
	if err != nil && strings.Contains(err.Error(), "last_access_utc") {
		// Forks with an older schema: no activity times, still names.
		rows, err = db.Query(`SELECT host_key, name, 0 FROM cookies`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []cookieName
	for rows.Next() {
		var c cookieName
		var lastAccess int64
		if err := rows.Scan(&c.host, &c.name, &lastAccess); err != nil {
			return nil, err
		}
		if lastAccess > chromiumEpochOffset {
			c.lastAccess = time.UnixMicro(lastAccess - chromiumEpochOffset)
		}
		names = append(names, c)
	}
	return names, rows.Err()
}

// chromiumMajorVersion reads the version the browser last ran with from
// "Last Version" in its data directory; 0 if unknown.
func chromiumMajorVersion(rootDir string) int {
	data, err := os.ReadFile(filepath.Join(rootDir, "Last Version"))
	if err != nil {
		return 0
	}
	major, _, _ := strings.Cut(strings.TrimSpace(string(data)), ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return n
}

// appBoundBrowsers encrypt cookies on Windows with a key bound to the
// browser itself (App-Bound Encryption, since version 127), which no
// other program — yt-dlp included — can decrypt.
var appBoundBrowsers = map[string]bool{"chrome": true, "edge": true, "brave": true}

const appBoundFirstVersion = 127

// HasAppBoundEncryption reports whether yt-dlp can't decrypt profile's
// cookies for that reason. An unknown version gets the benefit of the
// doubt: the extraction itself then reports the failure.
func HasAppBoundEncryption(profile Profile, goos string) bool {
	return goos == "windows" && appBoundBrowsers[profile.Browser.ID] &&
		chromiumMajorVersion(profile.RootDir) >= appBoundFirstVersion
}
