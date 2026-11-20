package main

import (
	"sync"
	"testing"
	"time"
)

// fakeClock is a Clock that only moves when the test calls Advance.
type fakeClock struct {
	mu sync.Mutex // devices read the time from their own goroutines
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// newClockNetwork is newTestNetwork with a fake clock.
func newClockNetwork(ids ...string) (*Network, *collector, *fakeClock) {
	clock := newFakeClock()
	c := newCollector()
	n := NewNetworkWithClock(c.emit, clock.Now)
	for _, id := range ids {
		if err := n.AddDevice(id); err != nil {
			panic(err)
		}
	}
	return n, c, clock
}

func TestSendSetsDefaultExpiry(t *testing.T) {
	n, c, clock := newClockNetwork("A")
	mustDo(t, n.Send(testMsg("m1", "A", "C"))) // no links: A holds it
	c.waitFor(t, EventForwarded, "A", "m1")

	a := n.devices["A"]
	a.mu.Lock()
	got := a.Queue[0].Expiry
	a.mu.Unlock()
	if want := clock.Now().Add(DefaultTTL); !got.Equal(want) {
		t.Fatalf("Expiry = %v, want %v", got, want)
	}
}

func TestExpiredMessageIsPurgedAndNotForwarded(t *testing.T) {
	n, c, clock := newClockNetwork("A", "B")
	mustDo(t, n.Send(testMsg("m1", "A", "C"))) // no links yet: A holds it
	c.waitFor(t, EventForwarded, "A", "m1")

	clock.Advance(DefaultTTL + time.Minute)
	mustDo(t, n.Link("A", "B")) // triggers Sync on A

	c.waitFor(t, EventExpired, "A", "m1")
	for _, id := range queuedIDs(n.devices["A"]) {
		if id == "m1" {
			t.Fatal("A still holds the expired message")
		}
	}

	// Marker sent after the purge: once B has it, B would have had m1 too.
	mustDo(t, n.Send(testMsg("marker", "A", "B")))
	events := c.waitFor(t, EventDelivered, "B", "marker")
	for _, e := range events {
		if e.DeviceID == "B" && e.MessageID == "m1" {
			t.Fatalf("expired message reached B: %+v", e)
		}
	}
}

// Replay protection: a genuine, correctly signed message captured earlier
// is useless once it has expired.
func TestReplayedExpiredMessageIsDropped(t *testing.T) {
	n, c, clock := newClockNetwork("A", "B")

	captured := NewMessage("old", "A", "B", TypeGeneral, "Level 38 is safe, return")
	captured.Expiry = clock.Now().Add(time.Minute)
	Sign(&captured, demoKey("A")) // valid signature from a trusted device

	clock.Advance(2 * time.Minute)
	mustDo(t, n.Inject("B", captured))

	events := c.waitFor(t, EventExpired, "B", "old")
	if hasEvent(events, EventDelivered, "B") {
		t.Fatal("expired replay was delivered")
	}
}

func TestReceiptExpiresWithItsMessage(t *testing.T) {
	n, c, clock := newClockNetwork("A", "B")
	mustDo(t, n.Link("A", "B"))
	mustDo(t, n.Send(testMsg("m1", "A", "B")))
	c.waitFor(t, EventAcked, "A", "m1")

	clock.Advance(DefaultTTL + time.Minute)
	mustDo(t, n.CutLink("A", "B"))
	mustDo(t, n.RestoreLink("A", "B")) // Sync on both: B purges its receipt

	c.waitFor(t, EventExpired, "B", "ack:m1")
	if ids := queuedIDs(n.devices["B"]); len(ids) != 0 {
		t.Fatalf("B still holds %v", ids)
	}
}
