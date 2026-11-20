package main

import (
	"reflect"
	"testing"
	"time"
)

// collector gathers events from devices through a channel, so tests can
// wait for a specific event instead of sleeping.
type collector struct {
	ch      chan Event
	history []Event // every event read so far (only touched by the test goroutine)
}

func newCollector() *collector {
	return &collector{ch: make(chan Event, 1000)}
}

// emit is passed to devices as their Emitter.
func (c *collector) emit(e Event) {
	c.ch <- e
}

// waitFor returns once an event with the given type, device and message
// has happened, whether it was already read earlier or arrives now.
// It fails the test if that takes longer than 2 seconds.
// It returns all events up to and including the match, in order.
func (c *collector) waitFor(t *testing.T, typ EventType, deviceID, msgID string) []Event {
	t.Helper()
	matches := func(e Event) bool {
		return e.Type == typ && e.DeviceID == deviceID && e.MessageID == msgID
	}

	// Already happened? Events can arrive in any order between devices.
	for i, e := range c.history {
		if matches(e) {
			return c.history[:i+1]
		}
	}

	timeout := time.After(2 * time.Second)
	for {
		select {
		case e := <-c.ch:
			c.history = append(c.history, e)
			if matches(e) {
				return c.history
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %s on %s for %s; got %v", typ, deviceID, msgID, c.history)
			return nil
		}
	}
}

// newStartedDevice creates a device and starts its loop.
func newStartedDevice(id string, c *collector) *Device {
	d := NewDevice(id, c.emit)
	go d.start()
	return d
}

func testMsg(id, from, to string) Message {
	return Message{ID: id, SenderID: from, Recipient: to, Priority: General, Timestamp: time.Now(), Body: "hi"}
}

func TestDirectNeighborDelivery(t *testing.T) {
	c := newCollector()
	a := newStartedDevice("A", c)
	b := newStartedDevice("B", c)
	a.Connect(b)

	a.Inbox <- testMsg("m1", "A", "B")

	c.waitFor(t, EventDelivered, "B", "m1")
}

func TestTwoHopForwarding(t *testing.T) {
	c := newCollector()
	a := newStartedDevice("A", c)
	b := newStartedDevice("B", c)
	d := newStartedDevice("C", c)
	a.Connect(b)
	b.Connect(d)

	a.Inbox <- testMsg("m1", "A", "C")

	c.waitFor(t, EventForwarded, "B", "m1")
	c.waitFor(t, EventDelivered, "C", "m1")
}

func TestStoreAndForwardAfterConnect(t *testing.T) {
	c := newCollector()
	a := newStartedDevice("A", c)
	b := newStartedDevice("B", c)

	// No link yet: A has to hold the message.
	a.Inbox <- testMsg("m1", "A", "B")
	c.waitFor(t, EventForwarded, "A", "m1") // A has queued it

	a.Connect(b)

	c.waitFor(t, EventDelivered, "B", "m1")
}

func TestDuplicateIsDropped(t *testing.T) {
	c := newCollector()
	b := newStartedDevice("B", c)

	b.Inbox <- testMsg("m1", "A", "B")
	b.Inbox <- testMsg("m1", "A", "B") // same ID again
	b.Inbox <- testMsg("m2", "A", "B")

	// The inbox is processed in order, so once m2 is delivered the
	// duplicate m1 has already been handled.
	events := c.waitFor(t, EventDelivered, "B", "m2")
	count := 0
	for _, e := range events {
		if e.Type == EventDelivered && e.MessageID == "m1" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("m1 delivered %d times, want 1", count)
	}
}

// newChain starts devices with the given IDs and links each to the next,
// e.g. A-B-C-D-E.
func newChain(c *collector, ids ...string) []*Device {
	devices := make([]*Device, len(ids))
	for i, id := range ids {
		devices[i] = newStartedDevice(id, c)
		if i > 0 {
			devices[i-1].Connect(devices[i])
		}
	}
	return devices
}

func TestMessageCrossesFiveDeviceChain(t *testing.T) {
	c := newCollector()
	chain := newChain(c, "A", "B", "C", "D", "E")

	chain[0].Inbox <- testMsg("m1", "A", "E") // needs 4 hops

	c.waitFor(t, EventDelivered, "E", "m1")
}

func TestBroadcastCrossesFiveDeviceChain(t *testing.T) {
	c := newCollector()
	chain := newChain(c, "A", "B", "C", "D", "E")

	chain[0].Inbox <- testMsg("alert", "A", "ALL")

	c.waitFor(t, EventBroadcast, "E", "alert")
}

// Sync must not hold the device lock while writing to neighbor inboxes,
// otherwise a full neighbor inbox freezes the device.
func TestSyncReleasesLockWhileSending(t *testing.T) {
	a := NewDevice("A", nil)
	b := NewDevice("B", nil)
	b.Inbox = make(chan Message) // unbuffered: every send blocks until we receive
	a.Neighbors = []*Device{b}
	a.Queue = []Message{testMsg("m1", "A", "X"), testMsg("m2", "A", "X")}

	go a.Sync()
	<-b.Inbox // first send done: Sync is now blocked sending m2

	locked := make(chan struct{})
	go func() {
		a.mu.Lock()
		a.mu.Unlock()
		close(locked)
	}()
	select {
	case <-locked:
		// good: the lock was free while Sync was sending
	case <-time.After(2 * time.Second):
		t.Fatal("Sync held the device lock while sending to a neighbor")
	}
	<-b.Inbox // let Sync finish
}

// queuedIDs returns the IDs of the messages a device is still holding.
func queuedIDs(d *Device) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var ids []string
	for _, m := range d.Queue {
		ids = append(ids, m.ID)
	}
	return ids
}

func TestDeliveredMessageIsClearedFromQueues(t *testing.T) {
	c := newCollector()
	chain := newChain(c, "A", "B", "C")

	chain[0].Inbox <- testMsg("m1", "A", "C")

	c.waitFor(t, EventDelivered, "C", "m1")
	c.waitFor(t, EventCleared, "B", "m1") // relay got the receipt
	c.waitFor(t, EventAcked, "A", "m1")   // sender knows it arrived

	for _, d := range chain {
		for _, id := range queuedIDs(d) {
			if id == "m1" {
				t.Fatalf("%s still holds m1 after delivery", d.ID)
			}
		}
	}
}

func TestLateCopyIsDroppedAfterReceipt(t *testing.T) {
	c := newCollector()
	b := newStartedDevice("B", c)

	// B hears the receipt first, then a late copy of the original.
	b.Inbox <- Message{ID: "ack:m1", SenderID: "C", Recipient: "A", AckFor: "m1", Priority: General}
	b.Inbox <- testMsg("m1", "A", "C")
	b.Inbox <- testMsg("m2", "A", "B") // marker: processed after the late copy

	events := c.waitFor(t, EventDelivered, "B", "m2")
	if hasEvent(events, EventForwarded, "B") {
		t.Fatal("B forwarded a message that was already delivered")
	}
}

func TestSyncSendsEmergencyFirst(t *testing.T) {
	a := NewDevice("A", nil)
	b := NewDevice("B", nil) // not started: we read its inbox directly
	a.Neighbors = []*Device{b}
	a.Queue = []Message{
		NewMessage("chat", "A", "X", TypeGeneral, "hey"),
		NewMessage("checkin", "A", "X", TypeCheckIn, "I'm okay"),
		NewMessage("alert", "A", "ALL", TypeEmergency, "BLACKOUT"),
	}

	a.Sync()

	for _, want := range []string{"alert", "checkin", "chat"} {
		if got := (<-b.Inbox).ID; got != want {
			t.Fatalf("got %s, want %s (most urgent first)", got, want)
		}
	}
}

// A device holding a check-in and an emergency while cut off must pass
// on the emergency first once a link appears, even though it was sent later.
func TestEmergencyOvertakesCheckInAfterReconnect(t *testing.T) {
	c := newCollector()
	a := newStartedDevice("A", c)
	b := newStartedDevice("B", c)

	a.Inbox <- NewMessage("checkin", "A", "Z", TypeCheckIn, "I'm okay")
	a.Inbox <- NewMessage("alert", "A", "ALL", TypeEmergency, "BLACKOUT")
	c.waitFor(t, EventBroadcast, "A", "alert") // A is holding both

	a.Connect(b)

	events := c.waitFor(t, EventForwarded, "B", "checkin")
	if !hasEvent(events, EventBroadcast, "B") {
		t.Fatal("check-in reached B before the emergency")
	}
}

// lastEvent returns the final event in a waitFor result (the match).
func lastEvent(events []Event) Event {
	return events[len(events)-1]
}

func TestDeliveredMessageCarriesItsPath(t *testing.T) {
	c := newCollector()
	chain := newChain(c, "A", "B", "C", "D")

	chain[0].Inbox <- testMsg("m1", "A", "D")

	got := lastEvent(c.waitFor(t, EventDelivered, "D", "m1")).Path
	if want := []string{"A", "B", "C", "D"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("path = %v, want %v", got, want)
	}
}

// A-B-C, then C fans out to D and E. If C's neighbors appended to a shared
// slice, D and E would overwrite each other's last entry.
func TestPathsDoNotShareMemoryAcrossBranches(t *testing.T) {
	c := newCollector()
	chain := newChain(c, "A", "B", "C")
	d := newStartedDevice("D", c)
	e := newStartedDevice("E", c)
	chain[2].Connect(d)
	chain[2].Connect(e)

	chain[0].Inbox <- testMsg("alert", "A", "ALL")

	gotD := lastEvent(c.waitFor(t, EventBroadcast, "D", "alert")).Path
	gotE := lastEvent(c.waitFor(t, EventBroadcast, "E", "alert")).Path
	if want := []string{"A", "B", "C", "D"}; !reflect.DeepEqual(gotD, want) {
		t.Fatalf("D path = %v, want %v", gotD, want)
	}
	if want := []string{"A", "B", "C", "E"}; !reflect.DeepEqual(gotE, want) {
		t.Fatalf("E path = %v, want %v", gotE, want)
	}
}

func TestBroadcastReachesEveryone(t *testing.T) {
	c := newCollector()
	a := newStartedDevice("A", c)
	b := newStartedDevice("B", c)
	d := newStartedDevice("C", c)
	a.Connect(b)
	b.Connect(d)

	a.Inbox <- testMsg("alert", "A", "ALL")

	// Order between B and C is fixed (C only hears it through B).
	c.waitFor(t, EventBroadcast, "B", "alert")
	c.waitFor(t, EventBroadcast, "C", "alert")
}
