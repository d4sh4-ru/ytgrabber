// Package media inspects downloaded files with ffmpeg/ffprobe.
package media

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"ytgrabber/internal/model"
	"ytgrabber/internal/tools"
)

// ThumbnailPathFor is where yt-dlp puts the converted thumbnail for a
// media file: same name, .jpg extension.
func ThumbnailPathFor(mediaPath string) string {
	return strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath)) + ".jpg"
}

// EnsureThumbnail returns the path of a jpg thumbnail for a video. If
// yt-dlp didn't manage to fetch one (some extractors don't expose it), it
// falls back to grabbing a frame at the 2s mark via ffmpeg. Audio only uses
// the thumbnail yt-dlp fetched as its cover.
func EnsureThumbnail(ctx context.Context, set tools.Set, mediaPath string, kind model.MediaKind) string {
	thumbnailPath := ThumbnailPathFor(mediaPath)
	if _, err := os.Stat(thumbnailPath); err == nil {
		return thumbnailPath
	}
	if kind != model.MediaVideo || !set.Ffmpeg.Found {
		return ""
	}

	cmd := set.Command(ctx, set.Ffmpeg.Path,
		"-y",
		"-ss", "00:00:02",
		"-i", mediaPath,
		"-frames:v", "1",
		"-vf", "scale=320:-2",
		thumbnailPath,
	)
	if err := cmd.Run(); err != nil {
		return ""
	}
	if _, err := os.Stat(thumbnailPath); err != nil {
		return ""
	}
	return thumbnailPath
}

// ProbeDuration returns the length in seconds, or 0 if it can't be told.
func ProbeDuration(ctx context.Context, set tools.Set, mediaPath string) float64 {
	if !set.Ffprobe.Found {
		return 0
	}

	out, err := set.Command(ctx, set.Ffprobe.Path,
		"-v", "quiet",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		mediaPath,
	).Output()
	if err != nil {
		return 0
	}

	duration, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0
	}
	return duration
}
