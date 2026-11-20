package main

import "testing"

// newTestNetwork builds a network whose events go to a collector.
func newTestNetwork(ids ...string) (*Network, *collector) {
	c := newCollector()
	n := NewNetwork(c.emit)
	for _, id := range ids {
		if err := n.AddDevice(id); err != nil {
			panic(err) // test setup bug
		}
	}
	return n, c
}

func TestNetworkSendAcrossLinks(t *testing.T) {
	n, c := newTestNetwork("A", "B", "C")
	if err := n.Link("A", "B"); err != nil {
		t.Fatal(err)
	}
	if err := n.Link("B", "C"); err != nil {
		t.Fatal(err)
	}

	if err := n.Send(testMsg("m1", "A", "C")); err != nil {
		t.Fatal(err)
	}

	c.waitFor(t, EventDelivered, "C", "m1")
}

func TestNetworkAddDuplicateDevice(t *testing.T) {
	n, _ := newTestNetwork("A")
	if err := n.AddDevice("A"); err == nil {
		t.Fatal("expected error for duplicate device ID")
	}
}

func TestNetworkLinkUnknownDevice(t *testing.T) {
	n, _ := newTestNetwork("A")
	if err := n.Link("A", "ghost"); err == nil {
		t.Fatal("expected error linking to unknown device")
	}
}

func TestNetworkSendFromUnknownSender(t *testing.T) {
	n, _ := newTestNetwork("A")
	if err := n.Send(testMsg("m1", "ghost", "A")); err == nil {
		t.Fatal("expected error sending from unknown device")
	}
}

// mustDo fails the test if err is not nil. Keeps setup lines short.
func mustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// hasEvent reports whether events contains one of the given type for the device.
func hasEvent(events []Event, typ EventType, deviceID string) bool {
	for _, e := range events {
		if e.Type == typ && e.DeviceID == deviceID {
			return true
		}
	}
	return false
}

func TestCutLinkHoldsMessageUntilRestore(t *testing.T) {
	n, c := newTestNetwork("A", "B", "C")
	mustDo(t, n.Link("A", "B"))
	mustDo(t, n.Link("B", "C"))
	mustDo(t, n.CutLink("B", "C"))

	mustDo(t, n.Send(testMsg("m1", "A", "C")))
	c.waitFor(t, EventForwarded, "B", "m1") // B is holding it

	mustDo(t, n.RestoreLink("B", "C"))

	// Events up to delivery must include the restore: it can't arrive earlier.
	events := c.waitFor(t, EventDelivered, "C", "m1")
	if !hasEvent(events, EventLinkRestored, "B") {
		t.Fatal("message was delivered before the link was restored")
	}
}

func TestKillDeviceHoldsMessageUntilRevive(t *testing.T) {
	n, c := newTestNetwork("A", "B", "C")
	mustDo(t, n.Link("A", "B"))
	mustDo(t, n.Link("B", "C"))
	mustDo(t, n.KillDevice("B")) // the only path from A to C

	mustDo(t, n.Send(testMsg("m1", "A", "C")))
	c.waitFor(t, EventForwarded, "A", "m1") // A is holding it

	mustDo(t, n.ReviveDevice("B"))

	events := c.waitFor(t, EventDelivered, "C", "m1")
	if !hasEvent(events, EventDeviceRevived, "B") {
		t.Fatal("message was delivered before the device was revived")
	}
}

func TestCutLinkEmitsEvent(t *testing.T) {
	n, c := newTestNetwork("A", "B")
	mustDo(t, n.Link("A", "B"))
	mustDo(t, n.CutLink("A", "B"))

	events := c.waitFor(t, EventLinkCut, "A", "")
	if got := events[len(events)-1].PeerID; got != "B" {
		t.Fatalf("PeerID = %q, want B", got)
	}
}

func TestCutUnknownLinkFails(t *testing.T) {
	n, _ := newTestNetwork("A", "B")
	if err := n.CutLink("A", "B"); err == nil {
		t.Fatal("expected error cutting a link that was never made")
	}
}

func TestLinkTwiceFails(t *testing.T) {
	n, _ := newTestNetwork("A", "B")
	mustDo(t, n.Link("A", "B"))
	if err := n.Link("B", "A"); err == nil {
		t.Fatal("expected error linking the same pair twice")
	}
}

func TestSendFromDeadDeviceFails(t *testing.T) {
	n, _ := newTestNetwork("A")
	mustDo(t, n.KillDevice("A"))
	if err := n.Send(testMsg("m1", "A", "B")); err == nil {
		t.Fatal("expected error sending from a dead device")
	}
}

func TestNetworkNotice(t *testing.T) {
	n, c := newTestNetwork()
	n.Notice("network down")

	events := c.waitFor(t, EventNotice, "", "")
	if got := events[len(events)-1].Text; got != "network down" {
		t.Fatalf("notice text = %q, want %q", got, "network down")
	}
}
