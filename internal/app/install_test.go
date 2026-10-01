package app

import (
	"slices"
	"testing"

	"ytgrabber/internal/tools"
)

func TestInstallQueue(t *testing.T) {
	app, _ := newTestApp(t)
	app.tools = tools.Set{
		YtDlp:   tools.Status{Name: tools.YtDlp, Found: true},
		Ffmpeg:  tools.Status{Name: tools.Ffmpeg},
		Ffprobe: tools.Status{Name: tools.Ffprobe},
		Deno:    tools.Status{Name: tools.Deno},
	}

	queue, err := app.installQueue(nil)
	if err != nil || !slices.Equal(queue, []string{"ffmpeg", "deno"}) {
		t.Errorf("missing tools = %v, %v", queue, err)
	}

	queue, err = app.installQueue([]string{"ffprobe", "ffmpeg", "yt-dlp"})
	if err != nil || !slices.Equal(queue, []string{"ffmpeg", "yt-dlp"}) {
		t.Errorf("explicit = %v, %v", queue, err)
	}

	if _, err := app.installQueue([]string{"rm"}); err == nil {
		t.Error("unknown tool accepted")
	}

	app.tools.Ffmpeg.Found, app.tools.Ffprobe.Found, app.tools.Deno.Found = true, true, true
	if _, err := app.installQueue(nil); err == nil {
		t.Error("nothing to install must be an error")
	}
}

func TestInstallRejectsConcurrentRuns(t *testing.T) {
	app, _ := newTestApp(t)
	app.cancelInstall = func() {}

	if _, err := app.InstallTools([]string{"deno"}); err == nil {
		t.Error("second install started while one is running")
	}
}

func TestReportMarksInstallableTools(t *testing.T) {
	report := withInstallInfo(tools.Report{Tools: []tools.Status{{Name: "ffprobe"}, {Name: "yt-dlp"}, {Name: "other"}}})
	if !report.Tools[0].Installable || report.Tools[0].InstallSource == "" || !report.Tools[1].Installable || report.Tools[2].Installable {
		t.Errorf("report = %+v", report.Tools)
	}
}
