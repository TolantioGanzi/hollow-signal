package main

import (
	"strings"
	"testing"
	"time"
)

// noPause replaces time.Sleep so scenarios run instantly in tests.
func noPause(time.Duration) {}

// indexOf returns the position of the first matching event, or -1.
func indexOf(events []Event, typ EventType, deviceID, msgID string) int {
	for i, e := range events {
		if e.Type == typ && e.DeviceID == deviceID && e.MessageID == msgID {
			return i
		}
	}
	return -1
}

// Runs the exact scenario used in the demo video, end to end.
func TestVideoScenario(t *testing.T) {
	r := NewRunner()
	n := NewNetwork(r.Observe)
	mustDo(t, SetupDemo(n))

	if err := r.Run(n, VideoScenario(), noPause); err != nil {
		t.Fatal(err)
	}
	events := r.Events()

	must := func(typ EventType, deviceID, msgID string) int {
		t.Helper()
		i := indexOf(events, typ, deviceID, msgID)
		if i < 0 {
			t.Fatalf("missing %s on %q for %q", typ, deviceID, msgID)
		}
		return i
	}

	// Beat 1: the Directorate network goes down.
	severed := false
	for _, e := range events {
		if e.Type == EventDirectorate && e.Text == "SEVERED" {
			severed = true
		}
	}
	if !severed {
		t.Fatal("no DIRECTORATE SEVERED event")
	}

	// Beat 2: Maya's check-in reaches Dad and she gets the receipt.
	path := events[must(EventDelivered, Dad, MsgCheckIn)].Path
	if len(path) < 2 || path[0] != Maya || path[len(path)-1] != Dad {
		t.Fatalf("check-in path = %v, want Maya ... Dad", path)
	}
	for _, id := range path {
		if id == Relay {
			t.Fatalf("check-in went through the dead relay: %v", path)
		}
	}
	must(EventAcked, Maya, MsgCheckIn)

	// Beat 3: the emergency reaches the cut-off pumps before the chatter.
	alert := must(EventBroadcast, Pumps, MsgBlackout)
	chat := must(EventDelivered, Pumps, MsgChatter1)
	if alert > chat {
		t.Fatal("chatter reached the pumps before the emergency")
	}

	// Beat 4: the forged message is rejected for its signature...
	rej := must(EventRejected, Maya, MsgForged)
	if !strings.Contains(events[rej].Text, "INVALID SIGNATURE") {
		t.Fatalf("rejection text = %q", events[rej].Text)
	}
	// ...and with verification off, the same message spreads to Dad.
	must(EventBroadcast, Dad, MsgForged)
}

func TestRunnerFailsWhenExpectedEventNeverHappens(t *testing.T) {
	r := NewRunner()
	r.Timeout = 50 * time.Millisecond
	n := NewNetwork(r.Observe)

	steps := []Step{{
		Caption: "waiting for something that never happens",
		Do:      func(n *Network) error { return nil },
		WaitFor: []Expect{{EventDelivered, "nobody", "nothing"}},
	}}
	if err := r.Run(n, steps, noPause); err == nil {
		t.Fatal("expected a timeout error")
	}
}
