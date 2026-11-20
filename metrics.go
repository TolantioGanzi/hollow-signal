package main

import (
	"sync/atomic"
	"time"
)

// Stats are counters shared by every device in a network. They use
// atomics because many device goroutines update them at the same time.
// A nil *Stats is allowed and ignores everything (devices outside a Network).
type Stats struct {
	delivered  atomic.Int64 // direct deliveries + broadcast copies received
	duplicates atomic.Int64
	rejected   atomic.Int64
	expired    atomic.Int64
	limited    atomic.Int64 // dropped by the per-sender rate limit
	overflow   atomic.Int64 // dropped because a queue was full
	totalDelay atomic.Int64 // sum of delivery delays, in nanoseconds
}

// MetricsSnapshot is a copy of the counters at one moment, sent to the UI.
type MetricsSnapshot struct {
	Delivered         int64   `json:"delivered"`
	DuplicatesDropped int64   `json:"duplicatesDropped"`
	AttacksBlocked    int64   `json:"attacksBlocked"`
	Expired           int64   `json:"expired"`
	RateLimited       int64   `json:"rateLimited"`
	QueueDropped      int64   `json:"queueDropped"`
	AvgDelayMs        float64 `json:"avgDelayMs"`
}

func (s *Stats) addDelivered(delay time.Duration) {
	if s == nil {
		return
	}
	s.delivered.Add(1)
	s.totalDelay.Add(int64(delay))
}

func (s *Stats) addDuplicate() {
	if s != nil {
		s.duplicates.Add(1)
	}
}

func (s *Stats) addRejected() {
	if s != nil {
		s.rejected.Add(1)
	}
}

func (s *Stats) addExpired() {
	if s != nil {
		s.expired.Add(1)
	}
}

func (s *Stats) addRateLimited() {
	if s != nil {
		s.limited.Add(1)
	}
}

func (s *Stats) addQueueDropped() {
	if s != nil {
		s.overflow.Add(1)
	}
}

// Snapshot reads all counters. Each one is read separately, so the
// numbers can be a moment apart; that's fine for a dashboard.
func (s *Stats) Snapshot() MetricsSnapshot {
	if s == nil {
		return MetricsSnapshot{}
	}
	snap := MetricsSnapshot{
		Delivered:         s.delivered.Load(),
		DuplicatesDropped: s.duplicates.Load(),
		AttacksBlocked:    s.rejected.Load(),
		Expired:           s.expired.Load(),
		RateLimited:       s.limited.Load(),
		QueueDropped:      s.overflow.Load(),
	}
	if snap.Delivered > 0 {
		avg := time.Duration(s.totalDelay.Load() / snap.Delivered)
		snap.AvgDelayMs = float64(avg) / float64(time.Millisecond)
	}
	return snap
}
