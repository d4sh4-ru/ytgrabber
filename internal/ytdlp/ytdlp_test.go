package ytdlp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ytgrabber/internal/model"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "https://www.youtube.com/watch?v=abc", want: "https://www.youtube.com/watch?v=abc"},
		{in: "  youtu.be/abc  ", want: "https://youtu.be/abc"},
		{in: "http://example.com", want: "http://example.com"},
		{in: "", wantErr: true},
		{in: "   ", wantErr: true},
		{in: "--exec rm -rf ~", wantErr: true},
		{in: "file:///etc/passwd", wantErr: true},
		{in: "ftp://example.com/video", wantErr: true},
		{in: "https://", wantErr: true},
	}

	for _, tt := range tests {
		got, err := NormalizeURL(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("NormalizeURL(%q) = %q, want error", tt.in, got)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestBuildYtDlpArgs(t *testing.T) {
	args := BuildArgs("https://example.com/v", "1080p", "/tmp/100% videos", "/opt/ffmpeg")

	if n := len(args); n < 2 || args[n-2] != "--" || args[n-1] != "https://example.com/v" {
		t.Fatalf("URL must be the last argument after \"--\", got tail %q", args[max(len(args)-2, 0):])
	}
	for _, flag := range []string{"--ignore-config", "--no-playlist", "--newline", "--no-simulate", "--no-quiet"} {
		if !slices.Contains(args, flag) {
			t.Errorf("missing %s", flag)
		}
	}

	output := args[slices.Index(args, "-o")+1]
	if !strings.Contains(output, "100%% videos") {
		t.Errorf("%% in the directory must be escaped, got %q", output)
	}
	if !strings.HasSuffix(output, outputTemplate) {
		t.Errorf("unexpected output template %q", output)
	}

	if i := slices.Index(args, "--ffmpeg-location"); i == -1 || args[i+1] != "/opt/ffmpeg" {
		t.Errorf("ffmpeg location not passed: %q", args)
	}
	if i := slices.Index(args, "-f"); i == -1 || !strings.Contains(args[i+1], "height<=1080") {
		t.Errorf("1080p format selector missing: %q", args)
	}

	audio := BuildArgs("https://example.com/v", "audio", "/tmp", "")
	if !slices.Contains(audio, "-x") || slices.Contains(audio, "--merge-output-format") {
		t.Errorf("audio args wrong: %q", audio)
	}
	if slices.Contains(audio, "--ffmpeg-location") {
		t.Errorf("empty ffmpeg path must not be passed: %q", audio)
	}
}

func TestParseOutputLine(t *testing.T) {
	tests := []struct {
		line string
		kind EventKind
		val  string
		num  float64
	}{
		{line: "YTG_TITLE Me at the zoo", kind: EventTitle, val: "Me at the zoo"},
		{line: "YTG_FILENAME /d/Me at the zoo [id].mp4", kind: EventFilename, val: "/d/Me at the zoo [id].mp4"},
		{line: "YTG_FILEPATH /d/Me at the zoo [id].mp4\r", kind: EventFilepath, val: "/d/Me at the zoo [id].mp4"},
		{line: "[download] Destination: /d/a.f395.mp4", kind: EventTrackStart, val: "/d/a.f395.mp4"},
		{line: "[download]  42.5% of   10.00MiB at    1.00MiB/s ETA 00:05", kind: EventProgress, num: 42.5},
		{line: "[download] 100% of   10.00MiB in 00:00:10", kind: EventProgress, num: 100},
		{line: `[Merger] Merging formats into "/d/a.mp4"`, kind: EventProcessing, val: "/d/a.mp4"},
		{line: "[ExtractAudio] Destination: /d/a.mp3", kind: EventProcessing, val: "/d/a.mp3"},
		{line: "[download] /d/a.mp4 has already been downloaded", kind: EventNone},
		{line: "[youtube] abc: Downloading webpage", kind: EventNone},
	}

	for _, tt := range tests {
		got := ParseLine(tt.line)
		if got.Kind != tt.kind || got.Value != tt.val || got.Percent != tt.num {
			t.Errorf("ParseLine(%q) = %+v, want kind=%v value=%q num=%v", tt.line, got, tt.kind, tt.val, tt.num)
		}
	}
}

func TestExtractErrorMessage(t *testing.T) {
	lines := []string{
		"WARNING: something minor",
		"ERROR: [youtube] abc: Video unavailable",
		"some trailing noise",
	}
	if got := ExtractErrorMessage(lines); got != "[youtube] abc: Video unavailable" {
		t.Errorf("got %q", got)
	}

	if got := ExtractErrorMessage(nil); got == "" {
		t.Error("empty stderr must still produce a message")
	}

	long := strings.Repeat("x", maxErrorMessageLength+50)
	if got := ExtractErrorMessage([]string{long}); len([]rune(got)) != maxErrorMessageLength+1 {
		t.Errorf("message not truncated: %d runes", len([]rune(got)))
	}
}

func TestRemovePartialFiles(t *testing.T) {
	dir := t.TempDir()
	track := filepath.Join(dir, "Clip [abc].f395.mp4")
	output := filepath.Join(dir, "Clip [abc].mp4")
	libraryThumbnail := filepath.Join(dir, "Clip [abc].jpg")

	for _, name := range []string{
		track + ".part",
		track + ".ytdl",
		track + ".part-Frag1",
		track + ".part-Frag2.part",
		filepath.Join(dir, "Clip [abc].temp.mp4"),
		libraryThumbnail,
		filepath.Join(dir, "Other [xyz].mp4"),
	} {
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	RemovePartialFiles(
		[]model.DownloadTrack{{Filename: track}},
		output,
		func(path string) bool { return path == libraryThumbnail },
	)

	entries, _ := os.ReadDir(dir)
	var left []string
	for _, entry := range entries {
		left = append(left, entry.Name())
	}
	want := []string{"Clip [abc].jpg", "Other [xyz].mp4"}
	if !slices.Equal(left, want) {
		t.Errorf("left %q, want %q", left, want)
	}
}
