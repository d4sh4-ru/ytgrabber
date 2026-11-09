// Command ytgrabber-server runs the app as a self-hosted server: the same
// core as the desktop app, reached from a browser or a phone over a VPN.
//
// Configuration comes from flags or environment variables:
//
//	-listen    YTG_LISTEN      address to listen on (127.0.0.1:8080)
//	-data      YTG_DATA_DIR    database, accounts, tools, token (./data)
//	-media     YTG_MEDIA_DIR   downloaded files (./media)
//	-web       YTG_WEB_DIR     built frontend; empty serves the API only
//	           YTG_TOKEN       access token; YTG_TOKEN_FILE reads it from
//	                           a file (Docker secrets). Without either, a
//	                           token is generated once and kept in the data dir.
//	-ytdlp-update   YTG_YTDLP_UPDATE   how often to update yt-dlp (24h, 0 = never)
//	-ytdlp-channel  YTG_YTDLP_CHANNEL  stable or nightly yt-dlp builds (stable)
//
// "ytgrabber-server healthcheck" asks the running server for
// /api/v1/health and exits non-zero if it doesn't answer: the Docker
// image has no curl.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"ytgrabber/internal/core"
	"ytgrabber/internal/installer"
	"ytgrabber/internal/platform"
	"ytgrabber/internal/server"
)

const shutdownTimeout = 15 * time.Second

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		log.Fatalf("ytgrabber-server: %v", err)
	}
}

func run() error {
	listen := flag.String("listen", env("YTG_LISTEN", "127.0.0.1:8080"), "address to listen on")
	dataDir := flag.String("data", env("YTG_DATA_DIR", "data"), "data directory")
	mediaDir := flag.String("media", env("YTG_MEDIA_DIR", "media"), "downloads directory")
	webDir := flag.String("web", env("YTG_WEB_DIR", ""), "built frontend directory")
	ytDlpUpdate := flag.String("ytdlp-update", env("YTG_YTDLP_UPDATE", "24h"), "how often to update yt-dlp; 0 turns it off")
	ytDlpChannel := flag.String("ytdlp-channel", env("YTG_YTDLP_CHANNEL", "stable"), "yt-dlp builds: stable or nightly")
	flag.Parse()

	updateInterval, err := time.ParseDuration(*ytDlpUpdate)
	if err != nil || updateInterval < 0 {
		return fmt.Errorf("YTG_YTDLP_UPDATE: %q is not a duration like 24h, 12h or 0", *ytDlpUpdate)
	}
	if updateInterval > 0 && updateInterval < time.Hour {
		return fmt.Errorf("YTG_YTDLP_UPDATE: %s is too often, the minimum is 1h", updateInterval)
	}
	channel, err := installer.ParseChannel(*ytDlpChannel)
	if err != nil {
		return fmt.Errorf("YTG_YTDLP_CHANNEL: %w", err)
	}

	data := platform.AbsOrSelf(*dataDir)
	media := platform.AbsOrSelf(*mediaDir)
	for _, dir := range []string{data, media} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	token, generated, err := loadToken(data)
	if err != nil {
		return err
	}

	app := core.New(platform.Paths{
		DataDir:             data,
		DefaultDownloadsDir: media,
		LegacyDownloadsDir:  media,
	}, core.WithoutSelfUpdate(), core.WithYtDlpChannel(channel))

	srv, err := server.New(app, server.Config{Token: token, WebDir: *webDir})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// quit is how the core asks to exit (after "delete all data").
	app.Start(srv.Events, stop)
	defer app.Stop()
	if info := app.GetAppInfo(); info.StartupError != "" {
		return errors.New(info.StartupError)
	}

	if generated {
		log.Printf("ytgrabber-server: access token (kept in %s): %s", tokenPath(data), token)
	}
	if !isLoopback(*listen) {
		log.Printf("ytgrabber-server: listening on %s — make sure it is reachable only through your VPN", *listen)
	}

	go func() {
		server.EnsureTools(ctx, app)
		if updateInterval > 0 {
			server.AutoUpdateYtDlp(ctx, app, updateInterval)
		}
	}()

	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errs := make(chan error, 1)
	go func() {
		log.Printf("ytgrabber-server %s listening on http://%s", core.Version, *listen)
		errs <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	log.Printf("ytgrabber-server: shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	// Open WebSockets and media streams would hold Shutdown until the
	// timeout; downloads are stopped by app.Stop (deferred above) and
	// marked interrupted on the next start.
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		httpServer.Close()
	}
	return nil
}

// healthcheck probes the server on the port YTG_LISTEN names.
func healthcheck() int {
	_, port, err := net.SplitHostPort(env("YTG_LISTEN", "127.0.0.1:8080"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", port) + "/api/v1/health")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, resp.Status)
		return 1
	}
	return 0
}

func env(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func tokenPath(dataDir string) string {
	return filepath.Join(dataDir, "token")
}

// loadToken returns the access token from YTG_TOKEN, YTG_TOKEN_FILE or
// the data dir, generating and saving one on the first start.
func loadToken(dataDir string) (token string, generated bool, err error) {
	if token := strings.TrimSpace(os.Getenv("YTG_TOKEN")); token != "" {
		return token, false, nil
	}
	path := os.Getenv("YTG_TOKEN_FILE")
	fromEnv := path != ""
	if !fromEnv {
		path = tokenPath(dataDir)
	}

	content, err := os.ReadFile(path)
	switch {
	case err == nil:
		if token := strings.TrimSpace(string(content)); token != "" {
			return token, false, nil
		}
		if fromEnv {
			return "", false, fmt.Errorf("token file %s is empty", path)
		}
	case fromEnv || !errors.Is(err, os.ErrNotExist):
		return "", false, fmt.Errorf("reading token: %w", err)
	}

	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return "", false, err
	}
	token = base64.RawURLEncoding.EncodeToString(random)
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", false, fmt.Errorf("saving token: %w", err)
	}
	return token, true, nil
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
