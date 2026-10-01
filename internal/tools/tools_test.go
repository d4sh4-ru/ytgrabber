package tools

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"testing"

	"ytgrabber/internal/platform"
)

func TestLocateToolOverride(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, platform.ExecutableName("my-yt-dlp"))
	os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755)

	status := locate(YtDlp, tool)
	if !status.Found || !status.Custom || status.Path != tool {
		t.Errorf("override not used: %+v", status)
	}

	missing := locate(YtDlp, filepath.Join(dir, "missing"))
	if missing.Found || !missing.Custom {
		t.Errorf("missing override reported as found: %+v", missing)
	}
}

func TestResolveToolsFindsFfprobeNextToCustomFfmpeg(t *testing.T) {
	dir := t.TempDir()
	ffmpeg := filepath.Join(dir, platform.ExecutableName(Ffmpeg))
	ffprobe := filepath.Join(dir, platform.ExecutableName(Ffprobe))
	for _, path := range []string{ffmpeg, ffprobe} {
		os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755)
	}

	tools := Resolve(filepath.Join(dir, "none"), ffmpeg)
	if tools.Ffprobe.Path != ffprobe {
		t.Errorf("ffprobe = %+v", tools.Ffprobe)
	}
	if tools.Ready() {
		t.Error("ready without yt-dlp")
	}
	if auto := locate("definitely-not-installed-tool", ""); auto.Found {
		t.Errorf("found nonexistent tool: %+v", auto)
	}
}

func TestToolEnvExtendsPath(t *testing.T) {
	dir := t.TempDir()
	tools := Set{YtDlp: Status{Found: true, Path: filepath.Join(dir, "yt-dlp")}}

	var path string
	env := tools.Env()
	for _, entry := range env {
		if key, value, ok := strings.Cut(entry, "="); ok && strings.EqualFold(key, "PATH") {
			path = value // the last PATH entry wins, as in exec.Cmd
		}
	}

	dirs := filepath.SplitList(path)
	if !slices.Contains(dirs, dir) {
		t.Errorf("tool dir %s missing from PATH %q", dir, path)
	}
	if goruntime.GOOS == "darwin" && !slices.Contains(dirs, "/opt/homebrew/bin") {
		t.Errorf("Homebrew missing from PATH %q", path)
	}
	if !slices.Contains(env, "PYTHONIOENCODING=utf-8") {
		t.Error("UTF-8 output not forced")
	}
}

func TestParseVersion(t *testing.T) {
	tests := map[string][2]string{
		YtDlp:   {"2026.08.19\n", "2026.08.19"},
		Ffmpeg:  {"ffmpeg version 8.0 Copyright (c) 2000-2025\nbuilt with", "8.0"},
		Ffprobe: {"ffprobe version n7.1-3 Copyright", "n7.1-3"},
		Deno:    {"deno 2.5.0 (stable, release, aarch64-apple-darwin)\nv8 13", "2.5.0"},
	}
	for name, tt := range tests {
		if got := parseVersion(name, tt[0]); got != tt[1] {
			t.Errorf("parseVersion(%s) = %q, want %q", name, got, tt[1])
		}
	}
}
