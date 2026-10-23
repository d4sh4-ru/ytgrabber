package accounts

import (
	"errors"
	"net"
	"net/url"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// aliases map registrable domains that serve a site's content onto the
// domain whose cookies it uses, so a youtu.be link finds the youtube.com
// account.
var aliases = map[string]string{
	"youtu.be":             "youtube.com",
	"youtube-nocookie.com": "youtube.com",
	"twitter.com":          "x.com",
	"vk.ru":                "vk.com",
	"vkvideo.ru":           "vk.com",
	"instagr.am":           "instagram.com",
}

// sessionCookies are the cookies that mean "logged in" on well-known
// sites; any one of them is enough. Other sites only get a cookie count.
var sessionCookies = map[string][]string{
	"youtube.com":   {"SAPISID", "__Secure-3PSID"},
	"instagram.com": {"sessionid"},
	"vk.com":        {"remixsid"},
	"x.com":         {"auth_token"},
	"tiktok.com":    {"sessionid"},
}

// checkURLs are pages that only open for a signed-in user, used to test
// an account with yt-dlp without downloading anything.
var checkURLs = map[string]string{
	"youtube.com": "https://www.youtube.com/feed/history",
}

var errNoSite = errors.New("не удалось определить сайт: укажите ссылку или домен, например instagram.com")

// SiteOf returns the site a host belongs to: its registrable domain
// ("music.youtube.com" → "youtube.com", "bbc.co.uk" stays) with aliases
// applied. Accounts are keyed by it.
func SiteOf(host string) (string, error) {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	host = strings.TrimPrefix(host, ".")
	if host == "" || net.ParseIP(host) != nil {
		return "", errNoSite
	}
	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return "", errNoSite
	}
	if alias, ok := aliases[domain]; ok {
		return alias, nil
	}
	return domain, nil
}

// SiteFromInput accepts what a user would type into "add a site": a link
// or a bare domain.
func SiteFromInput(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", errNoSite
	}
	return SiteOf(parsed.Hostname())
}

// Belongs reports whether a cookie set for domain is sent to site: the
// cookie's registrable domain is the site or one of its aliases.
func Belongs(domain string, site string) bool {
	cookieSite, err := SiteOf(domain)
	return err == nil && cookieSite == site
}

// LoginState says whether cookies contain a session for site. known is
// false for sites without an entry in sessionCookies: for those only the
// cookie count is meaningful.
func LoginState(site string, cookies []Cookie) (known bool, loggedIn bool) {
	names, known := sessionCookies[site]
	if !known {
		return false, false
	}
	for _, c := range cookies {
		for _, name := range names {
			if c.Name == name && c.Value != "" {
				return true, true
			}
		}
	}
	return true, false
}

// IsKnownSite reports whether LoginState can tell a login apart.
func IsKnownSite(site string) bool {
	_, ok := sessionCookies[site]
	return ok
}

// CheckURL returns a page to test site's login on, "" if there is none.
func CheckURL(site string) string {
	return checkURLs[site]
}
