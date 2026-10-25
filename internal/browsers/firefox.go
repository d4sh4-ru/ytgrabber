package browsers

import (
	"bufio"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ytgrabber/internal/accounts"
)

// firefoxProfiles reads profiles.ini in a Firefox-family data directory.
func firefoxProfiles(browser Browser, dir string) []Profile {
	file, err := os.Open(filepath.Join(dir, "profiles.ini"))
	if err != nil {
		return nil
	}
	defer file.Close()

	var profiles []Profile
	type section struct {
		name, path string
		relative   bool
	}
	var current *section
	flush := func() {
		if current == nil || current.path == "" {
			return
		}
		profileDir := filepath.FromSlash(current.path)
		if current.relative {
			profileDir = filepath.Join(dir, profileDir)
		}
		cookies := filepath.Join(profileDir, "cookies.sqlite")
		if !isFile(cookies) {
			return
		}
		name := current.name
		if name == "" {
			name = filepath.Base(profileDir)
		}
		profiles = append(profiles, Profile{Browser: browser, Name: name, Dir: profileDir, CookiesPath: cookies})
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "["):
			flush()
			current = nil
			if strings.HasPrefix(strings.ToLower(line), "[profile") {
				current = &section{relative: true}
			}
		case current != nil:
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "Name":
				current.name = strings.TrimSpace(value)
			case "Path":
				current.path = strings.TrimSpace(value)
			case "IsRelative":
				current.relative = strings.TrimSpace(value) != "0"
			}
		}
	}
	flush()
	return profiles
}

// firefoxDefaultContainer keeps cookies of ordinary tabs: not of
// containers (userContextId) and not third-party partitioned ones.
const firefoxDefaultContainer = `originAttributes NOT LIKE '%userContextId=%' AND originAttributes NOT LIKE '%partitionKey=%'`

func firefoxNames(db *sql.DB) ([]cookieName, error) {
	rows, err := db.Query(`SELECT host, name, lastAccessed FROM moz_cookies WHERE ` + firefoxDefaultContainer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []cookieName
	for rows.Next() {
		var c cookieName
		var lastAccessed int64 // microseconds since the Unix epoch
		if err := rows.Scan(&c.host, &c.name, &lastAccessed); err != nil {
			return nil, err
		}
		if lastAccessed > 0 {
			c.lastAccess = time.UnixMicro(lastAccessed)
		}
		names = append(names, c)
	}
	return names, rows.Err()
}

// ReadFirefoxCookies reads the cookies of site from a Firefox-family
// profile. Firefox keeps them unencrypted, so neither yt-dlp nor the OS
// keychain is involved. tmpDir receives a short-lived copy of the
// database (Firefox keeps it open).
func ReadFirefoxCookies(profile Profile, site string, tmpDir string, now time.Time) ([]accounts.Cookie, error) {
	if profile.Browser.Engine != EngineFirefox {
		return nil, errNotFirefox
	}
	var cookies []accounts.Cookie
	err := withDatabaseCopy(profile.CookiesPath, tmpDir, func(db *sql.DB) error {
		rows, err := db.Query(`SELECT host, path, isSecure, expiry, name, value, isHttpOnly FROM moz_cookies WHERE ` + firefoxDefaultContainer)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c accounts.Cookie
			var secure, httpOnly int
			if err := rows.Scan(&c.Domain, &c.Path, &secure, &c.Expires, &c.Name, &c.Value, &httpOnly); err != nil {
				return err
			}
			if !accounts.Belongs(c.Domain, site) {
				continue // values of other sites are dropped right away
			}
			c.Secure, c.HTTPOnly = secure != 0, httpOnly != 0
			c.IncludeSubdomains = strings.HasPrefix(c.Domain, ".")
			// Firefox 140+ stores expiry in milliseconds.
			if c.Expires > 100_000_000_000 {
				c.Expires /= 1000
			}
			cookies = append(cookies, c)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return accounts.FilterCookies(cookies, site, now), nil
}
