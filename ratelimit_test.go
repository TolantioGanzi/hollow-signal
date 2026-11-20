package main

import (
	"fmt"
	"testing"
)

func TestSenderOverLimitIsDropped(t *testing.T) {
	n, c, clock := newClockNetwork("A", "B") // not linked

	for i := 1; i <= RateLimit; i++ {
		id := fmt.Sprintf("m%d", i)
		mustDo(t, n.Send(testMsg(id, "A", "Z")))
		c.waitFor(t, EventForwarded, "A", id)
	}

	// One more inside the same window is dropped by A itself.
	mustDo(t, n.Send(testMsg("flood", "A", "Z")))
	c.waitFor(t, EventRateLimited, "A", "flood")
	if got := n.Metrics().RateLimited; got != 1 {
		t.Fatalf("RateLimited = %d, want 1", got)
	}

	// Other senders are not affected.
	other := NewMessage("fromB", "B", "Z", TypeGeneral, "hi")
	Sign(&other, demoKey("B"))
	mustDo(t, n.Inject("A", other))
	c.waitFor(t, EventForwarded, "A", "fromB")

	// A new window starts after RateWindow.
	clock.Advance(RateWindow)
	mustDo(t, n.Send(testMsg("later", "A", "Z")))
	c.waitFor(t, EventForwarded, "A", "later")
}

// The same message arriving many times (from many neighbors) counts once.
func TestDuplicatesDoNotCountTowardLimit(t *testing.T) {
	n, c := newTestNetwork("A", "B") // not linked

	m := NewMessage("m1", "A", "B", TypeGeneral, "hi")
	Sign(&m, demoKey("A"))
	for i := 0; i < RateLimit+5; i++ {
		mustDo(t, n.Inject("B", m))
	}
	next := NewMessage("m2", "A", "B", TypeGeneral, "second")
	Sign(&next, demoKey("A"))
	mustDo(t, n.Inject("B", next))

	c.waitFor(t, EventDelivered, "B", "m2")
}

func TestEmergenciesAreNotRateLimited(t *testing.T) {
	n, c := newTestNetwork("A")
	n.AuthorizeEmergency("A")

	last := ""
	for i := 0; i <= RateLimit; i++ { // one more than the limit
		last = fmt.Sprintf("alert%d", i)
		mustDo(t, n.Send(NewMessage(last, "A", "ALL", TypeEmergency, "BLACKOUT")))
	}
	events := c.waitFor(t, EventBroadcast, "A", last)
	if hasEvent(events, EventRateLimited, "A") {
		t.Fatal("an emergency was rate limited")
	}
}
