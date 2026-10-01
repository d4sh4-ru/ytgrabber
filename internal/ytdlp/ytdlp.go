// Package ytdlp knows how to drive yt-dlp: which flags to pass, how to read
// its output and what files it leaves behind.
package ytdlp

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"ytgrabber/internal/model"
)

// Markers for the lines yt-dlp prints via --print. They are parsed instead
// of yt-dlp's human-readable log where possible: the log format isn't a
// stable interface, --print templates are.
const (
	titleMarker    = "YTG_TITLE "
	filenameMarker = "YTG_FILENAME "
	filepathMarker = "YTG_FILEPATH "
)

// outputTemplate names files "<title> [<id>].<ext>": the id keeps two
// different videos with the same title from overwriting each other, and
// the title is capped at 150 bytes to stay under filesystem name limits.
const outputTemplate = "%(title).150B [%(id)s].%(ext)s"

var (
	trackDestRe     = regexp.MustCompile(`^\[download\] Destination:\s*(.+)$`)
	trackProgressRe = regexp.MustCompile(`^\[download\]\s+(\d+\.?\d*)%`)
	mergeDestRe     = regexp.MustCompile(`^\[Merger\] Merging formats into "(.+)"`)
	extractAudioRe  = regexp.MustCompile(`^\[ExtractAudio\] Destination:\s*(.+)$`)
	ytDlpErrorRe    = regexp.MustCompile(`^ERROR:\s*(.+)$`)
)

const maxErrorMessageLength = 300

// Qualities are the values the UI offers; see BuildArgs for what they mean.
var validQualities = map[string]bool{
	"best":  true,
	"1080p": true,
	"720p":  true,
	"audio": true,
}

func IsValidQuality(quality string) bool {
	return validQualities[quality]
}

// NormalizeURL accepts what a user would paste into the form — with or
// without a scheme — and only lets http(s) URLs through. Anything else
// (including strings starting with "-" that yt-dlp would parse as options)
// is rejected here rather than trusted from the frontend.
func NormalizeURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", errors.New("ссылка пустая")
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", errors.New("некорректная ссылка")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("поддерживаются только ссылки http и https")
	}
	if parsed.Hostname() == "" || strings.ContainsAny(parsed.Host, " \t") {
		return "", errors.New("в ссылке нет адреса сайта")
	}
	return parsed.String(), nil
}

// BuildArgs maps the UI quality choice onto yt-dlp flags.
//
//   - --ignore-config: a user's yt-dlp config could change the output
//     template or verbosity and break the parsing below.
//   - --no-playlist: a link to a video inside a playlist downloads just
//     that video; one job always means one file.
//   - --print implies --quiet and --simulate, so both are switched back.
//   - video qualities force an mp4 container and every quality asks for a
//     jpg thumbnail: WebKit (macOS/Linux) can't play webm or show webp.
//   - "--" ends option parsing, so the URL can never be read as a flag.
func BuildArgs(sourceURL string, quality string, downloadsDir string, ffmpegPath string) []string {
	// The directory is part of the -o template, where "%" is special.
	dir := strings.ReplaceAll(downloadsDir, "%", "%%")

	args := []string{
		"--ignore-config",
		"--newline",
		"--no-playlist",
		"--no-quiet",
		"--no-simulate",
		"--no-mtime",
		"--windows-filenames",
		"--print", "before_dl:" + titleMarker + "%(title)s",
		"--print", "before_dl:" + filenameMarker + "%(filename)s",
		"--print", "after_move:" + filepathMarker + "%(filepath)s",
		"-o", filepath.Join(dir, outputTemplate),
	}

	if ffmpegPath != "" {
		args = append(args, "--ffmpeg-location", ffmpegPath)
	}

	switch quality {
	case "1080p":
		args = append(args, "-f", "bv*[height<=1080]+ba/b[height<=1080]", "--merge-output-format", "mp4")
	case "720p":
		args = append(args, "-f", "bv*[height<=720]+ba/b[height<=720]", "--merge-output-format", "mp4")
	case "audio":
		args = append(args, "-f", "ba/b", "-x", "--audio-format", "mp3")
	default: // "best"
		args = append(args, "-f", "bv*+ba/b", "--merge-output-format", "mp4")
	}

	args = append(args, "--write-thumbnail", "--convert-thumbnails", "jpg")

	return append(args, "--", sourceURL)
}

// Event is one meaningful line of yt-dlp's stdout.
type Event struct {
	Kind    EventKind
	Value   string  // title, file name or path, depending on Kind
	Percent float64 // for EventProgress
}

type EventKind int

const (
	EventNone EventKind = iota
	EventTitle
	EventFilename
	EventFilepath
	EventTrackStart
	EventProgress
	EventProcessing
)

// ParseLine classifies a line of yt-dlp's stdout. Note that the
// "Destination:" marker is only treated as a new track when it comes from a
// "[download]" line — the same marker also appears on "[ExtractAudio]"
// lines, which describe the converted output rather than another stream.
func ParseLine(line string) Event {
	line = strings.TrimRight(line, "\r")

	switch {
	case strings.HasPrefix(line, titleMarker):
		return Event{Kind: EventTitle, Value: strings.TrimSpace(strings.TrimPrefix(line, titleMarker))}
	case strings.HasPrefix(line, filenameMarker):
		return Event{Kind: EventFilename, Value: strings.TrimPrefix(line, filenameMarker)}
	case strings.HasPrefix(line, filepathMarker):
		return Event{Kind: EventFilepath, Value: strings.TrimPrefix(line, filepathMarker)}
	}

	if matches := trackDestRe.FindStringSubmatch(line); matches != nil {
		return Event{Kind: EventTrackStart, Value: matches[1]}
	}
	if matches := trackProgressRe.FindStringSubmatch(line); matches != nil {
		percent, _ := strconv.ParseFloat(matches[1], 64)
		return Event{Kind: EventProgress, Percent: percent}
	}
	if matches := mergeDestRe.FindStringSubmatch(line); matches != nil {
		return Event{Kind: EventProcessing, Value: matches[1]}
	}
	if matches := extractAudioRe.FindStringSubmatch(line); matches != nil {
		return Event{Kind: EventProcessing, Value: matches[1]}
	}
	return Event{Kind: EventNone}
}

func ClassifyTrack(filename string) model.TrackKind {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".m4a", ".opus", ".mp3", ".aac", ".ogg", ".wav":
		return model.TrackAudio
	default:
		return model.TrackVideo
	}
}

// ExtractErrorMessage turns yt-dlp's raw stderr into a short, readable
// message: the last "ERROR:" line if yt-dlp printed one (that's its own
// summary of what went wrong), otherwise the last few non-empty lines as a
// fallback. Either way the result is capped so a stack trace can't flood
// the UI.
func ExtractErrorMessage(stderrLines []string) string {
	var lastError string
	for _, line := range stderrLines {
		if matches := ytDlpErrorRe.FindStringSubmatch(strings.TrimRight(line, "\r")); matches != nil {
			lastError = strings.TrimSpace(matches[1])
		}
	}
	if lastError != "" {
		return truncateMessage(lastError)
	}

	nonEmpty := make([]string, 0, len(stderrLines))
	for _, line := range stderrLines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			nonEmpty = append(nonEmpty, trimmed)
		}
	}
	if len(nonEmpty) == 0 {
		return "yt-dlp завершился с ошибкой"
	}

	tailStart := max(len(nonEmpty)-3, 0)
	return truncateMessage(strings.Join(nonEmpty[tailStart:], "\n"))
}

func truncateMessage(message string) string {
	runes := []rune(message)
	if len(runes) <= maxErrorMessageLength {
		return message
	}
	return string(runes[:maxErrorMessageLength]) + "…"
}

// LineTail keeps the last n lines written to it.
type LineTail struct {
	limit int
	lines []string
}

func NewLineTail(limit int) *LineTail {
	return &LineTail{limit: limit}
}

func (t *LineTail) Lines() []string {
	return t.lines
}

func (t *LineTail) Add(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > t.limit {
		t.lines = t.lines[len(t.lines)-t.limit:]
	}
}

func StripExt(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path))
}

// RemovePartialFiles deletes what an abandoned download left behind:
// per-format streams, .part/.ytdl resume files, fragment files, the
// merger's temp output and a fetched thumbnail. Files already in the
// library are never touched (isReferenced), since a re-download of an
// existing video shares their names.
func RemovePartialFiles(tracks []model.DownloadTrack, outputFilename string, isReferenced func(string) bool) {
	var candidates []string
	var fragmentPrefixes []string

	for _, track := range tracks {
		candidates = append(candidates, track.Filename, track.Filename+".part", track.Filename+".ytdl")
		fragmentPrefixes = append(fragmentPrefixes, track.Filename+".part-Frag")
	}
	if outputFilename != "" {
		stem := StripExt(outputFilename)
		candidates = append(candidates,
			stem+".temp"+filepath.Ext(outputFilename),
			stem+".jpg", stem+".webp", stem+".png",
		)
	}

	for _, path := range candidates {
		if path == "" || isReferenced(path) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			continue
		}
	}

	// Fragment names contain "[id]", which filepath.Glob would read as a
	// character class, so match prefixes by hand.
	for _, prefix := range fragmentPrefixes {
		entries, err := os.ReadDir(filepath.Dir(prefix))
		if err != nil {
			continue
		}
		base := filepath.Base(prefix)
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), base) {
				_ = os.Remove(filepath.Join(filepath.Dir(prefix), entry.Name()))
			}
		}
	}
}
