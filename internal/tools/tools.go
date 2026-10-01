// Package tools finds and runs the external programs the app depends on:
// yt-dlp, ffmpeg/ffprobe and deno.
package tools

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"ytgrabber/internal/platform"
)

const (
	YtDlp   = "yt-dlp"
	Ffmpeg  = "ffmpeg"
	Ffprobe = "ffprobe"
	Deno    = "deno"
)

const (
	versionTimeout = 15 * time.Second
	updateTimeout  = 3 * time.Minute
)

// Status describes one external program the app depends on.
type Status struct {
	Name     string `json:"name"`
	Purpose  string `json:"purpose"`
	Required bool   `json:"required"`
	Found    bool   `json:"found"`
	Custom   bool   `json:"custom"`
	// Managed means the app installed this copy itself (see package installer).
	Managed bool `json:"managed"`
	// Installable is set by the app when it can download the tool for this
	// platform; InstallSource says from where.
	Installable   bool   `json:"installable"`
	InstallSource string `json:"installSource"`
	Path          string `json:"path"`
	Version       string `json:"version"`
	Hint          string `json:"hint"`
}

// Report is what the UI shows on the settings screen and uses to decide
// whether downloads can start at all.
type Report struct {
	Ready bool     `json:"ready"`
	Tools []Status `json:"tools"`
}

// Set is the resolved location of every tool.
type Set struct {
	YtDlp   Status
	Ffmpeg  Status
	Ffprobe Status
	Deno    Status
}

func (s Set) Ready() bool {
	return s.YtDlp.Found && s.Ffmpeg.Found
}

func (s Set) List() []Status {
	return []Status{s.YtDlp, s.Ffmpeg, s.Ffprobe, s.Deno}
}

func (s Set) Report() Report {
	return Report{Ready: s.Ready(), Tools: s.List()}
}

func (s *Set) all() []*Status {
	return []*Status{&s.YtDlp, &s.Ffmpeg, &s.Ffprobe, &s.Deno}
}

// Resolve locates every tool. A path chosen by the user in settings wins;
// then a copy the app installed into managedDir; then PATH, then
// well-known install locations (see SearchDirs).
func Resolve(ytDlpPath string, ffmpegPath string, managedDir string) Set {
	set := Set{
		YtDlp:  locate(YtDlp, ytDlpPath, managedDir),
		Ffmpeg: locate(Ffmpeg, ffmpegPath, managedDir),
		Deno:   locate(Deno, "", managedDir),
	}
	set.YtDlp.Required = true
	set.YtDlp.Purpose = "Загрузка видео"
	set.Ffmpeg.Required = true
	set.Ffmpeg.Purpose = "Склейка дорожек, конвертация в mp3, обложки"
	set.Deno.Purpose = "Нужен yt-dlp для YouTube (решение JS-проверок)"

	// ffprobe ships with ffmpeg, so a custom ffmpeg location is the first
	// place to look for it.
	set.Ffprobe = Status{Name: Ffprobe}
	if set.Ffmpeg.Custom && set.Ffmpeg.Found {
		sibling := filepath.Join(filepath.Dir(set.Ffmpeg.Path), platform.ExecutableName(Ffprobe))
		if platform.IsExecutableFile(sibling) {
			set.Ffprobe.Found = true
			set.Ffprobe.Path = sibling
		}
	}
	if !set.Ffprobe.Found {
		set.Ffprobe = locate(Ffprobe, "", managedDir)
	}
	set.Ffprobe.Purpose = "Определение длительности"

	for _, status := range set.all() {
		if !status.Found {
			status.Hint = installHint(status.Name)
		}
	}
	return set
}

func locate(name string, override string, managedDir string) Status {
	status := Status{Name: name}

	if override != "" {
		status.Custom = true
		status.Path = override
		status.Found = platform.IsExecutableFile(override)
		return status
	}

	if managedDir != "" {
		managed := filepath.Join(managedDir, platform.ExecutableName(name))
		if platform.IsExecutableFile(managed) {
			status.Found = true
			status.Managed = true
			status.Path = managed
			return status
		}
	}

	if path, err := exec.LookPath(name); err == nil {
		status.Found = true
		status.Path = platform.AbsOrSelf(path)
		return status
	}

	for _, dir := range SearchDirs() {
		candidate := filepath.Join(dir, platform.ExecutableName(name))
		if platform.IsExecutableFile(candidate) {
			status.Found = true
			status.Path = candidate
			return status
		}
	}
	return status
}

// SearchDirs lists install locations that are commonly missing from PATH.
// A GUI app started from Finder/Dock gets launchd's minimal PATH (no
// Homebrew), and a Windows app started before winget/scoop edited PATH
// won't see the change until relogin. The executable's own directory comes
// first so tools can be shipped next to the binary.
func SearchDirs() []string {
	var dirs []string

	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		dirs = append(dirs, exeDir)
		if goruntime.GOOS == "darwin" {
			dirs = append(dirs, filepath.Join(exeDir, "..", "Resources"))
		}
	}

	home := platform.HomeDir()
	switch goruntime.GOOS {
	case "darwin":
		dirs = append(dirs,
			"/opt/homebrew/bin",
			"/usr/local/bin",
			"/opt/local/bin",
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".deno", "bin"),
		)
	case "windows":
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			dirs = append(dirs, filepath.Join(localAppData, "Microsoft", "WinGet", "Links"))
		}
		if programData := os.Getenv("ProgramData"); programData != "" {
			dirs = append(dirs, filepath.Join(programData, "chocolatey", "bin"))
		}
		dirs = append(dirs,
			filepath.Join(home, "scoop", "shims"),
			filepath.Join(home, ".deno", "bin"),
		)
	default:
		dirs = append(dirs,
			"/usr/local/bin",
			"/usr/bin",
			"/bin",
			"/snap/bin",
			"/home/linuxbrew/.linuxbrew/bin",
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".deno", "bin"),
		)
	}
	return dirs
}

func installHint(name string) string {
	hints := map[string]map[string]string{
		YtDlp: {
			"darwin":  "brew install yt-dlp",
			"windows": "winget install yt-dlp.yt-dlp",
			"linux":   "pipx install yt-dlp (или бинарник с github.com/yt-dlp/yt-dlp/releases)",
		},
		Ffmpeg: {
			"darwin":  "brew install ffmpeg",
			"windows": "winget install Gyan.FFmpeg",
			"linux":   "sudo apt install ffmpeg (или пакет вашего дистрибутива)",
		},
		Deno: {
			"darwin":  "brew install deno",
			"windows": "winget install DenoLand.Deno",
			"linux":   "curl -fsSL https://deno.land/install.sh | sh",
		},
	}
	hints[Ffprobe] = hints[Ffmpeg]

	byOS, ok := hints[name]
	if !ok {
		return ""
	}
	if hint, ok := byOS[goruntime.GOOS]; ok {
		return hint
	}
	return byOS["linux"]
}

// Env is the environment for every child process. PATH is extended with
// the directories of the resolved tools and the well-known install
// locations, because yt-dlp itself looks up ffmpeg and deno on PATH. The
// Python variables force UTF-8 output: on Windows yt-dlp would otherwise
// print titles and file paths in the console's ANSI code page.
func (s Set) Env() []string {
	seen := map[string]bool{}
	var dirs []string
	add := func(dir string) {
		if dir != "" && !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}

	for _, status := range s.List() {
		if status.Found {
			add(filepath.Dir(status.Path))
		}
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		add(dir)
	}
	for _, dir := range SearchDirs() {
		add(dir)
	}

	return append(os.Environ(),
		"PATH="+strings.Join(dirs, string(os.PathListSeparator)),
		"PYTHONIOENCODING=utf-8",
		"PYTHONUTF8=1",
	)
}

// Command builds an exec.Cmd for an external tool that dies together with
// ctx — including any grandchildren, see platform.KillProcessTree.
func (s Set) Command(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = s.Env()
	platform.ConfigureCommand(cmd)
	cmd.Cancel = func() error {
		return platform.KillProcessTree(cmd)
	}
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

// FillVersions queries every found tool for its version in parallel; a
// slow or broken binary only costs its own timeout.
func (s *Set) FillVersions() {
	var wg sync.WaitGroup
	for _, status := range s.all() {
		if !status.Found {
			continue
		}
		wg.Add(1)
		go func(status *Status) {
			defer wg.Done()
			status.Version = s.version(*status)
		}(status)
	}
	wg.Wait()
}

func (s Set) version(status Status) string {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()

	flag := "--version"
	if status.Name == Ffmpeg || status.Name == Ffprobe {
		flag = "-version"
	}

	out, err := s.Command(ctx, status.Path, flag).Output()
	if err != nil {
		return ""
	}
	return parseVersion(status.Name, string(out))
}

// parseVersion extracts the version from a tool's first output line:
// "2026.08.19" (yt-dlp), "ffmpeg version 8.0 Copyright…", "deno 2.5.0 (…)".
func parseVersion(name string, output string) string {
	firstLine, _, _ := strings.Cut(strings.TrimSpace(output), "\n")
	fields := strings.Fields(firstLine)

	switch name {
	case Ffmpeg, Ffprobe:
		if len(fields) >= 3 && fields[1] == "version" {
			// Some builds append their origin: "9.0.2-https://…".
			version, _, _ := strings.Cut(fields[2], "-http")
			return version
		}
	case Deno:
		if len(fields) >= 2 {
			return fields[1]
		}
	default:
		if len(fields) >= 1 {
			return fields[0]
		}
	}
	return strings.TrimSpace(firstLine)
}

// UpdateYtDlp runs `yt-dlp -U`. It only works for the standalone release
// binary; package-manager installs (brew, pip, winget) print how to update
// instead, which is passed on to the user as is.
func (s Set) UpdateYtDlp() (string, error) {
	if !s.YtDlp.Found {
		return "", errors.New("yt-dlp не найден")
	}

	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	out, err := s.Command(ctx, s.YtDlp.Path, "-U").CombinedOutput()
	output := lastLines(string(out), 10)
	if err != nil {
		if output == "" {
			output = err.Error()
		}
		return "", errors.New(output)
	}
	return output, nil
}

func lastLines(text string, count int) string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return strings.Join(lines, "\n")
}
