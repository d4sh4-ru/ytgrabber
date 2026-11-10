package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

const (
	retryMin = time.Second
	retryMax = 30 * time.Second

	// The server pings every 30 s; a stream silent for longer is dead
	// even if the socket doesn't know yet (a laptop waking up, the VPN
	// reconnecting).
	silenceTimeout = 45 * time.Second
	writeTimeout   = 10 * time.Second
)

var errSignedOut = errors.New("вход на этом устройстве больше не действует: войдите снова")

// Run keeps the server's event stream open while there is a server and a
// token, forwarding its events to the window, until ctx ends. A dropped
// stream is reopened with growing pauses; Reconnect skips the pause.
func (c *Client) Run(ctx context.Context) {
	delay := retryMin
	for {
		// A Reconnect from before this attempt is served by it.
		select {
		case <-c.restart:
		default:
		}
		server, token := c.current()
		var err error
		connected := false
		if server != nil && token != "" {
			streamCtx, stop := context.WithCancel(ctx)
			c.mu.Lock()
			c.stopping = stop
			c.mu.Unlock()
			connected, err = c.stream(streamCtx, server, token)
			stop()
		}
		if ctx.Err() != nil {
			return
		}
		if connected {
			delay = retryMin
		}

		wait := delay
		switch {
		case server == nil || token == "":
			c.setStatus(Status{})
			wait = time.Hour // nothing to connect to until Use or a sign-in
		case errors.Is(err, context.Canceled):
			wait = 0 // Reconnect: the server or the token changed
		default:
			c.setStatus(Status{Error: err.Error()})
			delay = min(delay*2, retryMax)
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-c.restart:
			delay = retryMin
		case <-timer.C:
		}
		timer.Stop()
	}
}

// stream reads the event stream until it breaks; connected tells whether
// it was open at all.
func (c *Client) stream(ctx context.Context, server *url.URL, token string) (connected bool, err error) {
	target := *server
	target.Scheme = map[string]string{"https": "wss", "http": "ws"}[server.Scheme]
	target.Path = server.Path + eventsPath

	dialer := websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: checkTimeout}
	header := http.Header{"Authorization": {"Bearer " + token}}
	conn, resp, err := dialer.DialContext(ctx, target.String(), header)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return false, errSignedOut
		}
		if resp != nil {
			return false, fmt.Errorf("сервер %s отказал в подключении: %s", server.Host, resp.Status)
		}
		return false, unreachable(server, err)
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
		conn.Close() // unblocks ReadMessage
	}()

	conn.SetReadLimit(16 << 20)
	conn.SetReadDeadline(time.Now().Add(silenceTimeout))
	conn.SetPingHandler(func(data string) error {
		conn.SetReadDeadline(time.Now().Add(silenceTimeout))
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(writeTimeout))
	})
	c.setStatus(Status{Connected: true})

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return true, ctx.Err()
			}
			if isTimeout(err) {
				return true, fmt.Errorf("сервер %s перестал отвечать. Проверьте, что VPN включён", server.Host)
			}
			return true, fmt.Errorf("связь с сервером %s прервалась", server.Host)
		}
		conn.SetReadDeadline(time.Now().Add(silenceTimeout))
		var event struct {
			Name string          `json:"name"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(message, &event) != nil || event.Name == "" {
			continue
		}
		c.publish(EventPrefix+event.Name, event.Data)
	}
}
