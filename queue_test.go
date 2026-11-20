package main

import (
	"reflect"
	"testing"
)

// newCappedDevice starts a lone device whose queue holds at most max messages.
func newCappedDevice(c *collector, max int) *Device {
	d := NewDevice("A", c.emit)
	d.maxQueue = max // set before start(), so no race
	go d.start()
	return d
}

func TestFullQueueDropsOldestLeastUrgent(t *testing.T) {
	c := newCollector()
	d := newCappedDevice(c, 3)

	d.Inbox <- NewMessage("g1", "A", "Z", TypeGeneral, "chatter")
	d.Inbox <- NewMessage("c1", "A", "Z", TypeCheckIn, "I'm okay")
	d.Inbox <- NewMessage("e1", "A", "ALL", TypeEmergency, "BLACKOUT")
	d.Inbox <- NewMessage("g2", "A", "Z", TypeGeneral, "more chatter") // 4th: over the cap

	c.waitFor(t, EventDropped, "A", "g1")
	c.waitFor(t, EventForwarded, "A", "g2") // g2 handled: queue is settled

	got := queuedIDs(d)
	want := []string{"e1", "c1", "g2"} // Sync keeps the queue sorted by priority
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("queue = %v, want %v", got, want)
	}
}

func TestEmergencyIsNeverDroppedForChatter(t *testing.T) {
	c := newCollector()
	d := newCappedDevice(c, 2)

	d.Inbox <- NewMessage("e1", "A", "ALL", TypeEmergency, "BLACKOUT")
	d.Inbox <- NewMessage("e2", "A", "ALL", TypeEmergency, "BAD WATER")
	d.Inbox <- NewMessage("g1", "A", "Z", TypeGeneral, "chatter")

	// The new chatter itself is the least urgent, so it is the one dropped.
	c.waitFor(t, EventDropped, "A", "g1")
	if got, want := queuedIDs(d), []string{"e1", "e2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("queue = %v, want %v", got, want)
	}
}

func TestQueueDropsAreCounted(t *testing.T) {
	n, c := newTestNetwork("A")
	a := n.devices["A"]
	a.mu.Lock() // already running: change it under the lock enqueue uses
	a.maxQueue = 1
	a.mu.Unlock()

	mustDo(t, n.Send(testMsg("m1", "A", "Z")))
	mustDo(t, n.Send(testMsg("m2", "A", "Z")))
	c.waitFor(t, EventDropped, "A", "m1")

	if got := n.Metrics().QueueDropped; got != 1 {
		t.Fatalf("QueueDropped = %d, want 1", got)
	}
}
