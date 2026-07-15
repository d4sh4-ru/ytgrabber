package main

import (
	"embed"
	"net/http"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// The downloads directory is needed here, before OnStartup runs, to
	// configure the video file server below.
	_, downloadsDir := resolveDataPaths()

	// Create application with options
	err := wails.Run(&options.App{
		Title:     "ytgrabber",
		Width:     1024,
		MinWidth:  720,
		Height:    720,
		MinHeight: 368,
		AssetServer: &assetserver.Options{
			// Assets serves the built frontend. Handler is Wails' fallback
			// for any GET it can't satisfy from Assets — WKWebView blocks
			// <video src="/absolute/fs/path">, so downloaded files are
			// served through this same asset server instead, under
			// "/videos/", rather than exposed as raw filesystem paths.
			Assets:  assets,
			Handler: newVideoHandler(downloadsDir),
		},
		BackgroundColour: &options.RGBA{R: 18, G: 18, B: 18, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

func newVideoHandler(downloadsDir string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/videos/", http.StripPrefix("/videos/", http.FileServer(http.Dir(downloadsDir))))
	return mux
}
