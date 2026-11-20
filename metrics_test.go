package main

import (
	"testing"
	"time"
)

func TestNilStatsIsSafe(t *testing.T) {
	var s *Stats // devices outside a Network have no stats
	s.addDelivered(time.Second)
	s.addDuplicate()
	s.addRejected()
	s.addExpired()
	if got := s.Snapshot(); got != (MetricsSnapshot{}) {
		t.Fatalf("nil stats snapshot = %+v, want zero", got)
	}
}

func TestAverageDelay(t *testing.T) {
	s := &Stats{}
	s.addDelivered(1 * time.Second)
	s.addDelivered(3 * time.Second)
	if got := s.Snapshot().AvgDelayMs; got != 2000 {
		t.Fatalf("AvgDelayMs = %v, want 2000", got)
	}
}

func TestDelayMeasuresTimeHeld(t *testing.T) {
	n, c, clock := newClockNetwork("A", "B")
	mustDo(t, n.Link("A", "B"))
	mustDo(t, n.CutLink("A", "B"))

	mustDo(t, n.Send(testMsg("m1", "A", "B")))
	c.waitFor(t, EventForwarded, "A", "m1") // held by A

	clock.Advance(2 * time.Minute)
	mustDo(t, n.RestoreLink("A", "B"))
	c.waitFor(t, EventDelivered, "B", "m1")

	got := n.Metrics()
	if got.Delivered != 1 {
		t.Fatalf("Delivered = %d, want 1", got.Delivered)
	}
	if got.AvgDelayMs != 120000 {
		t.Fatalf("AvgDelayMs = %v, want 120000 (2 minutes)", got.AvgDelayMs)
	}
}

func TestDuplicatesAndAttacksAreCounted(t *testing.T) {
	n, c := newTestNetwork("A", "B") // not linked: we inject into B directly

	m1 := NewMessage("m1", "A", "B", TypeGeneral, "hi")
	Sign(&m1, demoKey("A"))
	marker := NewMessage("marker", "A", "B", TypeGeneral, "last")
	Sign(&marker, demoKey("A"))

	mustDo(t, n.Inject("B", m1))
	mustDo(t, n.Inject("B", m1)) // duplicate
	mustDo(t, n.Inject("B", NewMessage("fake", "A", "B", TypeGeneral, "unsigned")))
	mustDo(t, n.Inject("B", marker))
	c.waitFor(t, EventDelivered, "B", "marker") // inbox is in order: all handled

	got := n.Metrics()
	want := MetricsSnapshot{Delivered: 2, DuplicatesDropped: 1, AttacksBlocked: 1}
	got.AvgDelayMs = 0 // real clock here, delay is not what this test checks
	if got != want {
		t.Fatalf("metrics = %+v, want %+v", got, want)
	}
}

func TestEmitMetricsSendsSnapshot(t *testing.T) {
	n, c := newTestNetwork()
	n.EmitMetrics()

	e := lastEvent(c.waitFor(t, EventMetrics, "", ""))
	if e.Metrics == nil {
		t.Fatal("metrics event has no Metrics")
	}
}
