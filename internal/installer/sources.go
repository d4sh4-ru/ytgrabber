package installer

import (
	"fmt"
	goruntime "runtime"
	"strings"
)

type archiveKind int

const (
	archiveNone  archiveKind = iota // the download is the executable itself
	archiveZip                      // .zip
	archiveTarXz                    // .tar.xz
)

// artifact is one file to download for a tool.
type artifact struct {
	url string
	// checksumURL points at a SHA-256 list. When empty, the checksum lives
	// next to the file the download URL finally redirects to, with a
	// ".sha256" suffix (that's how "latest" links that redirect work).
	checksumURL string
	// checksumName is the entry to look up in a multi-file checksum list;
	// empty means the list has a single hash.
	checksumName string
	archive      archiveKind
	// files maps the base name of an archive member (or, without an
	// archive, the downloaded file) to the name it's installed under.
	files map[string]string
}

// Source describes where a tool is downloaded from, for the UI.
func Source(tool string) string {
	switch tool {
	case "yt-dlp":
		return "github.com/yt-dlp/yt-dlp"
	case "ffmpeg":
		if goruntime.GOOS == "darwin" {
			return "ffmpeg.martin-riedl.de"
		}
		return "github.com/yt-dlp/FFmpeg-Builds"
	case "deno":
		return "github.com/denoland/deno"
	}
	return ""
}

// Tools are the names Install accepts. ffprobe comes with ffmpeg.
var Tools = []string{"yt-dlp", "ffmpeg", "deno"}

// artifacts lists what to download for tool on goos/goarch. Only official
// release channels with published SHA-256 checksums are used: yt-dlp's
// own builds, the ffmpeg builds the yt-dlp project maintains for Windows
// and Linux, Martin Riedl's signed builds for macOS (yt-dlp provides none),
// and Deno's releases.
func artifacts(tool string, goos string, goarch string) ([]artifact, error) {
	exe := func(name string) string {
		if goos == "windows" {
			return name + ".exe"
		}
		return name
	}

	switch tool {
	case "yt-dlp":
		asset := map[string]string{
			"darwin/amd64":  "yt-dlp_macos",
			"darwin/arm64":  "yt-dlp_macos",
			"windows/amd64": "yt-dlp.exe",
			"windows/arm64": "yt-dlp_arm64.exe",
			"linux/amd64":   "yt-dlp_linux",
			"linux/arm64":   "yt-dlp_linux_aarch64",
		}[goos+"/"+goarch]
		if asset == "" {
			break
		}
		const base = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/"
		return []artifact{{
			url:          base + asset,
			checksumURL:  base + "SHA2-256SUMS",
			checksumName: asset,
			archive:      archiveNone,
			files:        map[string]string{asset: exe("yt-dlp")},
		}}, nil

	case "ffmpeg":
		if goos == "darwin" {
			arch := map[string]string{"amd64": "amd64", "arm64": "arm64"}[goarch]
			if arch == "" {
				break
			}
			var list []artifact
			for _, name := range []string{"ffmpeg", "ffprobe"} {
				list = append(list, artifact{
					url:     fmt.Sprintf("https://ffmpeg.martin-riedl.de/redirect/latest/macos/%s/release/%s.zip", arch, name),
					archive: archiveZip,
					files:   map[string]string{name: name},
				})
			}
			return list, nil
		}

		asset := map[string]string{
			"windows/amd64": "ffmpeg-master-latest-win64-gpl.zip",
			"windows/arm64": "ffmpeg-master-latest-winarm64-gpl.zip",
			"linux/amd64":   "ffmpeg-master-latest-linux64-gpl.tar.xz",
			"linux/arm64":   "ffmpeg-master-latest-linuxarm64-gpl.tar.xz",
		}[goos+"/"+goarch]
		if asset == "" {
			break
		}
		const base = "https://github.com/yt-dlp/FFmpeg-Builds/releases/download/latest/"
		kind := archiveZip
		if strings.HasSuffix(asset, ".tar.xz") {
			kind = archiveTarXz
		}
		return []artifact{{
			url:          base + asset,
			checksumURL:  base + "checksums.sha256",
			checksumName: asset,
			archive:      kind,
			files:        map[string]string{exe("ffmpeg"): exe("ffmpeg"), exe("ffprobe"): exe("ffprobe")},
		}}, nil

	case "deno":
		// Deno has no Windows arm64 build; the x64 one runs under emulation.
		target := map[string]string{
			"darwin/amd64":  "x86_64-apple-darwin",
			"darwin/arm64":  "aarch64-apple-darwin",
			"windows/amd64": "x86_64-pc-windows-msvc",
			"windows/arm64": "x86_64-pc-windows-msvc",
			"linux/amd64":   "x86_64-unknown-linux-gnu",
			"linux/arm64":   "aarch64-unknown-linux-gnu",
		}[goos+"/"+goarch]
		if target == "" {
			break
		}
		url := "https://github.com/denoland/deno/releases/latest/download/deno-" + target + ".zip"
		return []artifact{{
			url:         url,
			checksumURL: url + ".sha256sum",
			archive:     archiveZip,
			files:       map[string]string{exe("deno"): exe("deno")},
		}}, nil

	default:
		return nil, fmt.Errorf("неизвестная программа %q", tool)
	}

	return nil, fmt.Errorf("для %s нет готовой сборки под %s/%s — установите вручную", tool, goos, goarch)
}
