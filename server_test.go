package main

import (
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// dialHub starts a test HTTP server for the hub and connects one client.
// It waits for the hub's "connected" notice, so after this returns the
// client is registered and will receive every emitted event.
func dialHub(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") // http://... -> ws://...
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	hello := readEvent(t, conn)
	if hello.Type != EventNotice {
		t.Fatalf("first event = %+v, want a notice", hello)
	}
	return conn
}

// readEvent reads one JSON event, failing the test after 2 seconds.
func readEvent(t *testing.T, conn *websocket.Conn) Event {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var e Event
	if err := conn.ReadJSON(&e); err != nil {
		t.Fatalf("reading event: %v", err)
	}
	return e
}

func notice(text string) Event { return Event{Type: EventNotice, Text: text} }

func TestNewClientGetsHistoryReplayed(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(hub)
	defer srv.Close()

	hub.Emit(notice("first"))
	hub.Emit(notice("second"))

	conn := dialHub(t, srv)
	for _, want := range []string{"first", "second"} {
		if got := readEvent(t, conn).Text; got != want {
			t.Fatalf("replayed %q, want %q", got, want)
		}
	}
}

func TestReplayKeepsOnlyRecentEventsAndLatestMetrics(t *testing.T) {
	hub := NewHub()
	hub.maxHistory = 2
	srv := httptest.NewServer(hub)
	defer srv.Close()

	hub.Emit(notice("old"))
	hub.Emit(Event{Type: EventMetrics, Metrics: &MetricsSnapshot{Delivered: 1}})
	hub.Emit(notice("a"))
	hub.Emit(notice("b"))
	hub.Emit(Event{Type: EventMetrics, Metrics: &MetricsSnapshot{Delivered: 2}})

	conn := dialHub(t, srv)
	// "old" fell out of the 2-event history; metrics are not history.
	for _, want := range []string{"a", "b"} {
		if got := readEvent(t, conn).Text; got != want {
			t.Fatalf("replayed %q, want %q", got, want)
		}
	}
	m := readEvent(t, conn)
	if m.Type != EventMetrics || m.Metrics.Delivered != 2 {
		t.Fatalf("got %+v, want only the latest metrics", m)
	}
}

func TestClosedClientIsRemoved(t *testing.T) {
	hub := NewHub()
	removed := make(chan struct{}, 1)
	hub.onRemove = func() { removed <- struct{}{} }
	srv := httptest.NewServer(hub)
	defer srv.Close()

	conn := dialHub(t, srv)
	conn.Close() // the browser tab closes

	select {
	case <-removed:
	case <-time.After(2 * time.Second):
		t.Fatal("closed client was not removed")
	}
	if got := hub.clientCount(); got != 0 {
		t.Fatalf("clientCount = %d, want 0", got)
	}
	hub.Emit(notice("still works")) // must not fail or block
}

func TestHubSendsEventsAsJSONToAllClients(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(hub)
	defer srv.Close()

	c1 := dialHub(t, srv)
	c2 := dialHub(t, srv)

	want := Event{Type: EventDelivered, DeviceID: "Level-10", MessageID: "m1", Text: "arrived"}
	hub.Emit(want)

	for _, conn := range []*websocket.Conn{c1, c2} {
		// DeepEqual, because Event contains a slice (Path) and can't use !=.
		if got := readEvent(t, conn); !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
}
