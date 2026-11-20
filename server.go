package main

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	// Allow any origin: the page is served locally for the demo.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// writeTimeout stops one stuck browser from freezing the simulation:
// Emit holds the hub lock while writing, and devices wait on Emit.
const writeTimeout = 2 * time.Second

// Hub keeps the list of connected browsers and sends every event to all
// of them as JSON. It also remembers recent events, so a browser that
// connects late (or reloads) sees what already happened.
// It implements http.Handler, so it can be mounted at /ws.
type Hub struct {
	// mu guards everything below, and also makes sure only one goroutine
	// writes to a connection at a time (gorilla/websocket requires that).
	mu          sync.Mutex
	clients     map[*websocket.Conn]bool
	history     []Event // recent events, oldest first (no metrics)
	maxHistory  int
	lastMetrics *Event // metrics arrive twice a second: keep only the latest

	onRemove func() // test hook: called after a client is removed (may be nil)
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*websocket.Conn]bool), maxHistory: 1000}
}

// ServeHTTP upgrades the request to a websocket and registers the client.
// The client gets a "connected" notice, then the recent history, then
// the latest metrics. This all happens under the lock, so no live event
// can be missed or arrive in between.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("websocket upgrade failed:", err)
		return
	}

	h.mu.Lock()
	h.clients[conn] = true
	ok := h.write(conn, Event{Type: EventNotice, Text: "Connected to Hollow Signal"})
	for _, e := range h.history {
		ok = ok && h.write(conn, e) // stop writing after the first failure
	}
	if ok && h.lastMetrics != nil {
		ok = h.write(conn, *h.lastMetrics)
	}
	if !ok {
		h.removeLocked(conn)
	}
	h.mu.Unlock()
	log.Println("browser connected")

	// The browser never sends us anything, but reading is how we notice
	// it went away: ReadMessage returns an error when the tab closes.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				h.mu.Lock()
				h.removeLocked(conn)
				h.mu.Unlock()
				return
			}
		}
	}()
}

// write sends one event with a deadline. Returns false if it failed.
// The caller must hold h.mu.
func (h *Hub) write(conn *websocket.Conn, e Event) bool {
	conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return conn.WriteJSON(e) == nil
}

// removeLocked closes and forgets a client. Safe to call twice: the
// reader goroutine and a failed write may both try. Caller holds h.mu.
func (h *Hub) removeLocked(conn *websocket.Conn) {
	if !h.clients[conn] {
		return
	}
	delete(h.clients, conn)
	conn.Close()
	log.Println("browser disconnected")
	if h.onRemove != nil {
		h.onRemove()
	}
}

func (h *Hub) clientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Emit records an event and sends it to every connected client. Its
// signature matches Emitter. Clients whose write fails are removed.
func (h *Hub) Emit(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if e.Type == EventMetrics {
		h.lastMetrics = &e
	} else {
		h.history = append(h.history, e)
		if len(h.history) > h.maxHistory {
			h.history = h.history[1:] // drop the oldest
		}
	}

	for conn := range h.clients { // deleting during range is allowed in Go
		if !h.write(conn, e) {
			h.removeLocked(conn)
		}
	}
}
