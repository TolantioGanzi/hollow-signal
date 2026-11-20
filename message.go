package main

import "time"

// Priority decides send order: lower numbers leave a device first.
type Priority int

const (
	Emergency Priority = 1
	CheckIn   Priority = 2
	General   Priority = 3
)

// MaxHops is how many times a message may be passed on before devices
// stop forwarding it. 8 covers the whole Hollow (levels 10 to 50 plus
// detours) while still stopping messages from circulating forever.
const MaxHops = 8

// DefaultTTL is how long a message lives if the sender sets no expiry.
const DefaultTTL = 30 * time.Minute

// Rate limit: each device accepts at most RateLimit messages per original
// sender in each RateWindow. Receipts and emergencies are exempt.
const (
	RateLimit  = 10
	RateWindow = time.Minute
)

// MaxQueue is how many messages a device holds. When full, it drops the
// least urgent message (oldest first among equals).
const MaxQueue = 50

// Clock returns the current time. Real runs use time.Now; tests pass a
// fake clock so they can jump forward without sleeping.
type Clock func() time.Time

// MessageType says what a message is for. The UI uses it for labels and
// colors; NewMessage uses it to pick the priority.
type MessageType string

const (
	TypeEmergency MessageType = "emergency" // blackout warnings, calls for help
	TypeSickness  MessageType = "sickness"  // lung sickness / bad water reports
	TypeCheckIn   MessageType = "checkin"   // "I'm okay" to family
	TypeGeneral   MessageType = "general"   // everything else
	TypeReceipt   MessageType = "receipt"   // delivery receipt (see AckFor)
)

// priorityFor maps a message type to its send priority.
func priorityFor(t MessageType) Priority {
	switch t {
	case TypeEmergency:
		return Emergency
	case TypeSickness, TypeCheckIn:
		return CheckIn
	default:
		return General
	}
}

type Message struct {
	ID        string
	SenderID  string
	Recipient string // a device ID, or "ALL" for a broadcast
	Type      MessageType
	Priority  Priority
	HopCount  int
	// Path lists the devices that accepted this message, in order, starting
	// with the sender. Like HopCount it changes at every hop, so it is NOT
	// signed: a relay could lie about it. Display only, never trusted.
	Path      []string
	Timestamp time.Time
	Body      string

	// AckFor is set only on delivery receipts: the ID of the message that
	// was delivered. Devices that see a receipt stop carrying that message.
	AckFor string

	// Expiry is when devices stop carrying the message (zero = never).
	// Covered by the signature, so relays can't extend it.
	Expiry time.Time

	// Signature is the sender's ed25519 signature over every field above
	// except HopCount (see signingBytes).
	Signature []byte
}

// expiredAt reports whether the message has expired at time now.
func (m Message) expiredAt(now time.Time) bool {
	return !m.Expiry.IsZero() && !now.Before(m.Expiry)
}

// NewMessage builds a message with its priority taken from its type.
func NewMessage(id, from, to string, typ MessageType, body string) Message {
	return Message{
		ID:        id,
		SenderID:  from,
		Recipient: to,
		Type:      typ,
		Priority:  priorityFor(typ),
		Timestamp: time.Now(),
		Body:      body,
	}
}
