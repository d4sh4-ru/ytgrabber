// Package browsers finds the user's browser profiles and tells which of
// them are signed in to a site. Scanning only reads cookie names, domains
// and last-access times — never values, and never anything that needs the
// OS keychain. Firefox cookies are stored unencrypted, so for Firefox-based
// browsers the cookies themselves can be read too (ReadFirefoxCookies);
// Chromium-based ones encrypt them and go through yt-dlp.
package browsers

import (
	"os"
	"path/filepath"
	goruntime "runtime"
)

type Engine string

const (
	EngineFirefox  Engine = "firefox"
	EngineChromium Engine = "chromium"
)

// Browser is one browser the scanner knows how to look into.
type Browser struct {
	ID     string // stable id, also sent back by the UI
	Name   string // shown to the user
	Engine Engine
	// YtDlpName is the --cookies-from-browser name; empty when yt-dlp can't
	// read this browser and the user has to export a cookies.txt instead.
	YtDlpName string
	// roots are candidate data directories, relative to the home
	// directory or to an environment variable (see Env).
	roots map[string][]root
}

type root struct {
	base string // "home" or an environment variable such as APPDATA
	path string
}

func home(path string) root       { return root{"home", path} }
func appData(path string) root    { return root{"APPDATA", path} }
func localApp(path string) root   { return root{"LOCALAPPDATA", path} }
func macSupport(path string) root { return home(filepath.Join("Library", "Application Support", path)) }

// known lists the supported browsers. Firefox-family roots hold
// profiles.ini; Chromium-family roots are "User Data" directories.
var known = []Browser{
	{ID: "firefox", Name: "Firefox", Engine: EngineFirefox, YtDlpName: "firefox", roots: map[string][]root{
		"darwin":  {macSupport("Firefox")},
		"windows": {appData("Mozilla/Firefox")},
		"linux": {
			home(".mozilla/firefox"),
			home("snap/firefox/common/.mozilla/firefox"),
			home(".var/app/org.mozilla.firefox/.mozilla/firefox"),
		},
	}},
	{ID: "librewolf", Name: "LibreWolf", Engine: EngineFirefox, roots: map[string][]root{
		"darwin":  {macSupport("librewolf")},
		"windows": {appData("librewolf")},
		"linux":   {home(".librewolf"), home(".var/app/io.gitlab.librewolf-community/.librewolf")},
	}},
	{ID: "zen", Name: "Zen", Engine: EngineFirefox, roots: map[string][]root{
		"darwin":  {macSupport("zen")},
		"windows": {appData("zen")},
		"linux":   {home(".zen"), home(".var/app/app.zen_browser.zen/.zen")},
	}},
	{ID: "chrome", Name: "Chrome", Engine: EngineChromium, YtDlpName: "chrome", roots: map[string][]root{
		"darwin":  {macSupport("Google/Chrome")},
		"windows": {localApp("Google/Chrome/User Data")},
		"linux":   {home(".config/google-chrome")},
	}},
	{ID: "chromium", Name: "Chromium", Engine: EngineChromium, YtDlpName: "chromium", roots: map[string][]root{
		"darwin":  {macSupport("Chromium")},
		"windows": {localApp("Chromium/User Data")},
		"linux":   {home(".config/chromium"), home("snap/chromium/common/chromium")},
	}},
	{ID: "edge", Name: "Edge", Engine: EngineChromium, YtDlpName: "edge", roots: map[string][]root{
		"darwin":  {macSupport("Microsoft Edge")},
		"windows": {localApp("Microsoft/Edge/User Data")},
		"linux":   {home(".config/microsoft-edge")},
	}},
	{ID: "brave", Name: "Brave", Engine: EngineChromium, YtDlpName: "brave", roots: map[string][]root{
		"darwin":  {macSupport("BraveSoftware/Brave-Browser")},
		"windows": {localApp("BraveSoftware/Brave-Browser/User Data")},
		"linux":   {home(".config/BraveSoftware/Brave-Browser")},
	}},
	{ID: "vivaldi", Name: "Vivaldi", Engine: EngineChromium, YtDlpName: "vivaldi", roots: map[string][]root{
		"darwin":  {macSupport("Vivaldi")},
		"windows": {localApp("Vivaldi/User Data")},
		"linux":   {home(".config/vivaldi")},
	}},
	{ID: "opera", Name: "Opera", Engine: EngineChromium, YtDlpName: "opera", roots: map[string][]root{
		"darwin":  {macSupport("com.operasoftware.Opera")},
		"windows": {appData("Opera Software/Opera Stable")},
		"linux":   {home(".config/opera")},
	}},
	{ID: "opera-gx", Name: "Opera GX", Engine: EngineChromium, YtDlpName: "opera", roots: map[string][]root{
		"darwin":  {macSupport("com.operasoftware.OperaGX")},
		"windows": {appData("Opera Software/Opera GX Stable")},
	}},
	// yt-dlp can't decrypt these; the scan still tells the user where the
	// login is, so they know which browser to export cookies.txt from.
	{ID: "yandex", Name: "Яндекс Браузер", Engine: EngineChromium, roots: map[string][]root{
		"darwin":  {macSupport("Yandex/YandexBrowser")},
		"windows": {localApp("Yandex/YandexBrowser/User Data")},
		"linux":   {home(".config/yandex-browser")},
	}},
	{ID: "arc", Name: "Arc", Engine: EngineChromium, roots: map[string][]root{
		"darwin":  {macSupport("Arc/User Data")},
		"windows": {localApp("Packages/TheBrowserCompany.Arc_ttt1ap7aakyb4/LocalCache/Local/Arc/User Data")},
	}},
}

// Env says where per-user directories are; Scan takes it as a parameter so
// tests can point it at a fake home.
type Env struct {
	GOOS string
	Home string
	Vars map[string]string // APPDATA, LOCALAPPDATA
}

// CurrentEnv describes the running system.
func CurrentEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{
		GOOS: goruntime.GOOS,
		Home: home,
		Vars: map[string]string{"APPDATA": os.Getenv("APPDATA"), "LOCALAPPDATA": os.Getenv("LOCALAPPDATA")},
	}
}

func (e Env) resolve(r root) string {
	base := e.Home
	if r.base != "home" {
		base = e.Vars[r.base]
	}
	if base == "" {
		return ""
	}
	return filepath.Join(base, filepath.FromSlash(r.path))
}

// Find returns the browser with the given id.
func Find(id string) (Browser, bool) {
	for _, browser := range known {
		if browser.ID == id {
			return browser, true
		}
	}
	return Browser{}, false
}

// Profile is one browser profile with a cookie database.
type Profile struct {
	Browser Browser
	Name    string // display name: "default-release", "Profile 2" → user-given name
	// Dir is the profile directory; for yt-dlp it is passed as the profile.
	Dir string
	// CookiesPath is the cookie database inside Dir.
	CookiesPath string
	// RootDir is the browser's data directory the profile belongs to
	// (Chromium's "User Data"); for Opera it is Dir itself.
	RootDir string
}

// YtDlpSpec is the --cookies-from-browser value for a Chromium profile.
// The profile is passed as a path, which yt-dlp accepts in place of a
// name and which also tells it where the browser's key (Local State) is.
func (p Profile) YtDlpSpec() string {
	return p.Browser.YtDlpName + ":" + p.Dir
}

// Profiles lists every profile of every known browser found in env.
func Profiles(env Env) []Profile {
	var profiles []Profile
	seen := map[string]bool{}
	for _, browser := range known {
		for _, r := range browser.roots[env.GOOS] {
			dir := env.resolve(r)
			if dir == "" {
				continue
			}
			var found []Profile
			if browser.Engine == EngineFirefox {
				found = firefoxProfiles(browser, dir)
			} else {
				found = chromiumProfiles(browser, dir)
			}
			for _, profile := range found {
				if !seen[profile.CookiesPath] {
					seen[profile.CookiesPath] = true
					profiles = append(profiles, profile)
				}
			}
		}
	}
	return profiles
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
