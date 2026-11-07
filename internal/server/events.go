package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Event is one message on the WebSocket: the name the desktop passes to
// runtime.EventsEmit and its payload (null for events without one).
type Event struct {
	Name string `json:"name"`
	Data any    `json:"data"`
}

const (
	// clientBuffer is how many events a slow client may lag behind
	// before it is dropped; it reconnects and reloads the state.
	clientBuffer = 256
	writeTimeout = 10 * time.Second
	pingInterval = 30 * time.Second
	pongTimeout  = pingInterval + 10*time.Second
)

// Hub fans core events out to the connected WebSockets. It is the
// server's core.EventSink.
type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[chan []byte]struct{})}
}

// Publish sends an event to every client. It never blocks the core: a
// client whose buffer is full is disconnected instead.
func (h *Hub) Publish(name string, data ...interface{}) {
	event := Event{Name: name}
	if len(data) > 0 {
		event.Data = data[0]
	}
	message, err := json.Marshal(event)
	if err != nil {
		log.Printf("ytgrabber: encoding event %s: %v", name, err)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.clients {
		select {
		case client <- message:
		default:
			delete(h.clients, client)
			close(client)
		}
	}
}

func (h *Hub) subscribe() chan []byte {
	client := make(chan []byte, clientBuffer)
	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()
	return client
}

func (h *Hub) unsubscribe(client chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[client]; ok {
		delete(h.clients, client)
		close(client)
	}
}

// upgrader keeps gorilla's default origin check: the page must come from
// this server, so another site can't open a socket with the user's cookie.
var upgrader = websocket.Upgrader{}

// serveWS streams events to one client. Clients only listen; anything
// they send is read and discarded so pings and close frames are handled.
func (h *Hub) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade has answered already
	}
	client := h.subscribe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn.SetReadLimit(4096)
		conn.SetReadDeadline(time.Now().Add(pongTimeout))
		conn.SetPongHandler(func(string) error {
			return conn.SetReadDeadline(time.Now().Add(pongTimeout))
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ping := time.NewTicker(pingInterval)
	defer func() {
		ping.Stop()
		h.unsubscribe(client)
		conn.Close()
	}()
	for {
		select {
		case message, ok := <-client:
			if !ok {
				return // dropped as too slow
			}
			conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ping.C:
			conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}
