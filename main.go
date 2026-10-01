package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"ytgrabber/internal/app"
	"ytgrabber/internal/platform"
)

// main stays in the module root: go:embed can't reach files above the
// package directory, and Wails builds the root package.

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	if len(os.Args) > 1 && os.Args[1] == app.CleanupHelperArg {
		app.RunCleanupHelper(os.Args[2:])
		return
	}

	paths := platform.ResolvePaths()
	backend := app.New(paths)
	onStartup, onShutdown := app.Hooks(backend)

	err := wails.Run(&options.App{
		Title:     "YT Grabber",
		Width:     1024,
		MinWidth:  720,
		Height:    720,
		MinHeight: 368,
		AssetServer: &assetserver.Options{
			// Assets serves the built frontend. Handler is Wails' fallback
			// for any request it can't satisfy from Assets — WebViews block
			// <video src="file://…">, so library files are served through
			// the same asset server under /media/ (see app.MediaHandler).
			Assets:  assets,
			Handler: app.MediaHandler(backend),
		},
		BackgroundColour: &options.RGBA{R: 18, G: 18, B: 18, A: 1},
		OnStartup:        onStartup,
		OnShutdown:       onShutdown,
		Bind: []interface{}{
			backend,
		},
		Mac: &mac.Options{
			About: &mac.AboutInfo{
				Title:   "YT Grabber",
				Message: "Версия " + app.Version,
				Icon:    appIcon,
			},
		},
		Windows: &windows.Options{
			Theme: windows.Dark,
			// Inside the data dir rather than WebView2's default
			// %AppData%\ytgrabber.exe, so "delete all data" has one place to wipe.
			WebviewUserDataPath: paths.WebviewDataDir(),
		},
		Linux: &linux.Options{
			Icon:        appIcon,
			ProgramName: "ytgrabber",
			// Hardware acceleration in WebKitGTK breaks rendering on a number
			// of GPU/driver combinations; only use it where it's known to work.
			WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand,
		},
	})

	if err != nil {
		log.Printf("ytgrabber: %v", err)
	}
}
