//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeYtDlpEncoding behaves like yt-dlp writing to a pipe: without
// --encoding utf-8 it prints the title and path in cp1251, as on a
// Russian Windows, while the file on disk gets its proper name.
const fakeYtDlpEncoding = `#!/bin/sh
enc=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift ;;
    --encoding) enc="$2"; shift ;;
  esac
  shift
done
dir=$(dirname "$out")
name="Привет мир [abc].mp4"
echo "video" > "$dir/$name"
if [ "$enc" = "utf-8" ]; then
  echo "YTG_TITLE Привет мир"
  echo "YTG_FILEPATH $dir/$name"
else
  printf 'YTG_TITLE \317\360\350\342\345\362 \354\350\360\n'
  printf 'YTG_FILEPATH %s/\317\360\350\342\345\362 \354\350\360 [abc].mp4\n' "$dir"
fi
`

func TestNonLatinTitle(t *testing.T) {
	app, events := newAppWithFakeTools(t)
	os.WriteFile(app.settings.YtDlpPath, []byte(fakeYtDlpEncoding), 0o755)

	if _, err := app.DownloadVideo("https://example.com/watch?v=abc", "best", true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "library entry", func() bool { return events.count("library-changed") > 0 })

	videos, _ := app.GetLibrary()
	if len(videos) != 1 {
		t.Fatalf("library = %+v", videos)
	}
	want := filepath.Join(app.settings.DownloadsDir, "Привет мир [abc].mp4")
	if v := videos[0]; v.Title != "Привет мир" || v.FilePath != want || v.FileMissing {
		t.Errorf("video = %+v", v)
	}
}

// TestFinishJobFindsMangledPath: a yt-dlp that ignores --encoding still
// ends up with the right file and a readable title.
func TestFinishJobFindsMangledPath(t *testing.T) {
	app, events := newAppWithFakeTools(t)
	script := strings.Replace(fakeYtDlpEncoding, `if [ "$enc" = "utf-8" ]; then`, `if false; then`, 1)
	os.WriteFile(app.settings.YtDlpPath, []byte(script), 0o755)

	app.DownloadVideo("https://example.com/watch?v=abc", "best", true)
	waitFor(t, "library entry", func() bool { return events.count("library-changed") > 0 })

	videos, _ := app.GetLibrary()
	want := filepath.Join(app.settings.DownloadsDir, "Привет мир [abc].mp4")
	if len(videos) != 1 || videos[0].FilePath != want || videos[0].Title != "Привет мир" {
		t.Errorf("library = %+v", videos)
	}
}
