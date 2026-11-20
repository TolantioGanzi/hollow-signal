package main

// EventType says what happened. The web UI and the tests both read these.
type EventType string

const (
	EventNotice      EventType = "notice"      // free text, e.g. a scenario caption
	EventDirectorate EventType = "directorate" // Text is "ONLINE" or "SEVERED" (status banner)
	EventDelivered EventType = "delivered" // message reached its recipient
	EventBroadcast EventType = "broadcast" // device received an "ALL" message
	EventForwarded EventType = "forwarded" // device queued a message to pass on
	EventCleared   EventType = "cleared"   // relay dropped a message because it was delivered
	EventAcked     EventType = "acked"     // sender got the delivery receipt
	EventRejected  EventType = "rejected"  // message failed verification (forged)
	EventExpired   EventType = "expired"   // message too old: dropped, not forwarded
	EventMetrics   EventType = "metrics"   // Metrics holds the current counters

	EventRateLimited EventType = "rate_limited" // sender over its limit: dropped
	EventDropped     EventType = "dropped"      // queue full: least urgent message dropped

	// Topology changes. For links, DeviceID and PeerID are the two ends.
	EventLinkCut       EventType = "link_cut"
	EventLinkRestored  EventType = "link_restored"
	EventDeviceKilled  EventType = "device_killed"
	EventDeviceRevived EventType = "device_revived"
)

// Event is one thing that happened in the simulation.
// The json tags are the field names the browser will see.
type Event struct {
	Type      EventType `json:"type"`
	DeviceID  string    `json:"deviceId"`
	PeerID    string    `json:"peerId,omitempty"` // other end of a link, for link events
	MessageID string    `json:"messageId"`
	Path      []string         `json:"path,omitempty"`    // devices the message passed through, for message events
	Metrics   *MetricsSnapshot `json:"metrics,omitempty"` // only on metrics events
	Text      string    `json:"text"`
}

// Emitter is a function that receives events. Devices call it; whoever
// created the device decides what to do with them (send to UI, record in a test).
type Emitter func(Event)
