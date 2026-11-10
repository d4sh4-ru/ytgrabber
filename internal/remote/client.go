package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	apiPrefix   = "/api/v1/"
	mediaPrefix = "/media/"
	sessionPath = "/api/v1/session"
	eventsPath  = "/api/v1/events"

	// sessionCookie is the server's cookie with the device token
	// (internal/server/auth.go): sign-in hands the token over in it.
	sessionCookie = "ytg_session"

	checkTimeout = 10 * time.Second
)

// StatusEvent is the event that tells the window whether the server is
// reachable; the server's own events are forwarded as EventPrefix+name.
const (
	StatusEvent = "remote-status"
	EventPrefix = "remote:"
)

// Status is the connection to the server as the window shows it.
type Status struct {
	// Connected: the event stream is open, so the server is reachable
	// and the device is signed in.
	Connected bool `json:"connected"`
	// Error says why the server can't be reached; empty while connected
	// or while nobody is signed in.
	Error string `json:"error,omitempty"`
	// Warning is something to know that doesn't stop the work, such as
	// the token not being saved for the next start.
	Warning string `json:"warning,omitempty"`
}

// Client is the desktop's link to a server: a proxy for the window's
// API calls and media requests, and the event stream.
type Client struct {
	secrets Secrets
	publish func(name string, data any)
	http    *http.Client

	mu       sync.Mutex
	server   *url.URL // nil: the local mode
	token    string
	status   Status
	restart  chan struct{}
	stopping context.CancelFunc // ends the open event stream
}

// New makes a client in the local mode; publish delivers events to the
// window.
func New(secrets Secrets, publish func(name string, data any)) *Client {
	return &Client{
		secrets: secrets,
		publish: publish,
		// No overall timeout: a video streams for as long as it plays.
		http: &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: checkTimeout}).DialContext,
			TLSHandshakeTimeout:   checkTimeout,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		}},
		restart: make(chan struct{}, 1),
	}
}

// Use switches to server (nil: the local mode) and the token stored for it.
func (c *Client) Use(server *url.URL) {
	token, warning := "", ""
	if server != nil {
		var err error
		if token, err = c.secrets.Get(server.String()); err != nil {
			warning = err.Error() + ". Войдите заново."
		}
	}
	c.mu.Lock()
	c.server, c.token = server, token
	c.status = Status{Warning: warning}
	c.mu.Unlock()
	c.Reconnect()
}

// Server is the server's address, "" in the local mode.
func (c *Client) Server() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.server == nil {
		return ""
	}
	return c.server.String()
}

// Status is the connection as it was last reported.
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// Reconnect drops the event stream and opens it again at once, instead
// of waiting out the pause after a failure.
func (c *Client) Reconnect() {
	c.mu.Lock()
	if c.stopping != nil {
		c.stopping()
	}
	c.mu.Unlock()
	select {
	case c.restart <- struct{}{}:
	default:
	}
}

func (c *Client) current() (*url.URL, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.server, c.token
}

func (c *Client) setStatus(status Status) {
	c.mu.Lock()
	// A warning stays until the server or the token changes.
	if status.Warning == "" {
		status.Warning = c.status.Warning
	}
	changed := status != c.status
	c.status = status
	c.mu.Unlock()
	if changed {
		c.publish(StatusEvent, status)
	}
}

// Check makes sure raw is a ytgrabber server and returns its address.
// Without a scheme https is tried first, then http.
func (c *Client) Check(ctx context.Context, raw string) (*url.URL, error) {
	server, err := ParseServer(raw)
	if err != nil {
		return nil, err
	}
	err = c.check(ctx, server)
	if err != nil && !strings.Contains(raw, "://") {
		plain := *server
		plain.Scheme = "http"
		if c.check(ctx, &plain) == nil {
			return &plain, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return server, nil
}

func (c *Client) check(ctx context.Context, server *url.URL) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.String()+sessionPath, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return unreachable(server, err)
	}
	defer resp.Body.Close()
	var session struct {
		Authenticated *bool `json:"authenticated"`
	}
	if resp.StatusCode != http.StatusOK ||
		json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&session) != nil ||
		session.Authenticated == nil {
		return fmt.Errorf("по адресу %s отвечает не сервер YT Grabber (%s)", server.Host, resp.Status)
	}
	return nil
}

// unreachable explains a failed request in the user's terms: the usual
// reason is the VPN being off.
func unreachable(server *url.URL, err error) error {
	var dnsErr *net.DNSError
	switch {
	case errors.As(err, &dnsErr):
		return fmt.Errorf("сервер %s не найден. Проверьте адрес и что VPN включён", server.Host)
	case errors.Is(err, context.DeadlineExceeded), isTimeout(err):
		return fmt.Errorf("сервер %s не отвечает. Проверьте, что VPN включён и сервер запущен", server.Host)
	default:
		return fmt.Errorf("нет связи с сервером %s: %v. Проверьте, что VPN включён и сервер запущен", server.Host, err)
	}
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// ServeHTTP forwards the window's API calls and media requests to the
// server with the device's token. Sign-in and sign-out pass through
// too: the token from a sign-in goes to the credential store instead of
// a cookie, and a sign-out forgets it.
func (c *Client) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	server, token := c.current()
	path := r.URL.Path
	if server == nil || path == eventsPath ||
		!(strings.HasPrefix(path, apiPrefix) || strings.HasPrefix(path, mediaPrefix)) {
		writeError(w, http.StatusNotFound, "не найдено")
		return
	}

	target := *server
	target.Path = server.Path + path
	target.RawQuery = r.URL.RawQuery
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.ContentLength = r.ContentLength
	for _, name := range forwardedRequestHeaders {
		if value := r.Header.Values(name); len(value) > 0 {
			req.Header[name] = value
		}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if r.Context().Err() != nil {
			return // the window gave up (a seek, a closed player)
		}
		err = unreachable(server, err)
		c.setStatus(Status{Error: err.Error()})
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()

	if path == sessionPath {
		c.afterSession(server, r.Method, resp)
	}
	for name, values := range resp.Header {
		if !droppedResponseHeaders[http.CanonicalHeaderKey(name)] {
			w.Header()[name] = values
		}
	}
	w.WriteHeader(resp.StatusCode)
	copyFlushing(w, resp.Body)
}

// afterSession keeps the token a sign-in returned, or forgets it on a
// sign-out.
func (c *Client) afterSession(server *url.URL, method string, resp *http.Response) {
	switch {
	case method == http.MethodPost && resp.StatusCode == http.StatusOK:
		for _, cookie := range resp.Cookies() {
			if cookie.Name == sessionCookie && cookie.Value != "" {
				c.setToken(server, cookie.Value)
			}
		}
	case method == http.MethodDelete && resp.StatusCode < 300:
		c.setToken(server, "")
	}
}

func (c *Client) setToken(server *url.URL, token string) {
	var err error
	if token == "" {
		err = c.secrets.Delete(server.String())
	} else {
		err = c.secrets.Set(server.String(), token)
	}
	warning := ""
	if err != nil {
		log.Printf("ytgrabber: %v", err)
		if token != "" {
			// Signed in all the same, until the app quits.
			warning = err.Error() + ". После перезапуска придётся войти снова."
		}
	}
	c.mu.Lock()
	if c.server == nil || c.server.String() != server.String() {
		c.mu.Unlock()
		return // switched away meanwhile
	}
	c.token = token
	c.status = Status{Warning: warning}
	c.mu.Unlock()
	c.publish(StatusEvent, Status{Warning: warning})
	c.Reconnect()
}

// SignOut revokes this device on the server, as far as it's reachable,
// and forgets its token either way.
func (c *Client) SignOut(ctx context.Context) {
	server, token := c.current()
	if server == nil {
		return
	}
	if token != "" {
		ctx, cancel := context.WithTimeout(ctx, checkTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, server.String()+sessionPath, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+token)
			if resp, err := c.http.Do(req); err == nil {
				resp.Body.Close()
			} else {
				log.Printf("ytgrabber: signing out of %s: %v", server.Host, err)
			}
		}
	}
	if err := c.secrets.Delete(server.String()); err != nil {
		log.Printf("ytgrabber: %v", err)
	}
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
}

// Download saves the library file at mediaPath (a /media/ URL) to dest.
func (c *Client) Download(ctx context.Context, mediaPath string, dest string) error {
	server, token := c.current()
	if server == nil {
		return errors.New("приложение не подключено к серверу")
	}
	if !strings.HasPrefix(mediaPath, mediaPrefix) {
		return fmt.Errorf("не файл библиотеки: %s", mediaPath)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.String()+mediaPath, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return unreachable(server, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("сервер не отдал файл: %s", resp.Status)
	}

	tmp := dest + ".part"
	file, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("не удалось создать файл: %w", err)
	}
	_, err = io.Copy(file, resp.Body)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, dest)
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("файл не скачан: %w", err)
	}
	return nil
}

// forwardedRequestHeaders are what the server needs from the window's
// requests; its cookies and anything else stay behind.
var forwardedRequestHeaders = []string{
	"Accept", "Content-Type", "Range", "If-Range", "If-None-Match", "If-Modified-Since",
}

// droppedResponseHeaders: the token cookie is the client's to keep, and
// the rest describe the hop to the server, not the one to the window.
var droppedResponseHeaders = map[string]bool{
	"Set-Cookie":        true,
	"Connection":        true,
	"Keep-Alive":        true,
	"Transfer-Encoding": true,
	"Upgrade":           true,
}

// copyFlushing streams the body to the window as it arrives.
func copyFlushing(w http.ResponseWriter, body io.Reader) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32<<10)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
