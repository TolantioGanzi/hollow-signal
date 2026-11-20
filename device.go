package main

import (
	"crypto/ed25519"
	"fmt"
	"sort"
	"sync"
	"time"
)

type Device struct {
	ID        string
	Inbox     chan Message
	Neighbors []*Device
	Seen      map[string]bool
	Queue     []Message
	mu        sync.Mutex
	emit      Emitter

	key   ed25519.PrivateKey // signs this device's receipts
	trust *Trust             // nil = accept everything (unit tests without security)
	now   Clock              // current time, for expiry
	stats *Stats             // shared counters; nil outside a Network

	// rates counts messages per original sender for the rate limit.
	// Only the start() goroutine touches it, so it needs no lock.
	rates map[string]*rateWindow

	maxQueue int // queue cap; read under mu
}

// rateWindow counts one sender's messages since start.
type rateWindow struct {
	start time.Time
	count int
}

// NewDevice creates a device with an empty inbox, seen set and queue.
// emit receives this device's events; nil means "ignore events".
func NewDevice(id string, emit Emitter) *Device {
	if emit == nil {
		emit = func(Event) {}
	}
	return &Device{
		ID:    id,
		Inbox: make(chan Message, 100),
		Seen:  make(map[string]bool),
		emit:  emit,
		key:   demoKey(id),
		now:   time.Now,
		rates: make(map[string]*rateWindow),

		maxQueue: MaxQueue,
	}
}

func (d *Device) Connect(other *Device) {
	d.mu.Lock()
	d.Neighbors = append(d.Neighbors, other)
	d.mu.Unlock()

	other.mu.Lock()
	other.Neighbors = append(other.Neighbors, d)
	other.mu.Unlock()

	d.Sync()
	other.Sync()
}

// Disconnect removes the link between two devices (both directions).
// Messages already queued stay queued until a link comes back.
func (d *Device) Disconnect(other *Device) {
	d.removeNeighbor(other)
	other.removeNeighbor(d)
}

func (d *Device) removeNeighbor(other *Device) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, n := range d.Neighbors {
		if n == other {
			d.Neighbors = append(d.Neighbors[:i], d.Neighbors[i+1:]...)
			return
		}
	}
}

// Sync sends every queued message to every neighbor, most urgent first.
// It works in two phases so the lock is never held while sending:
//  1. under the lock: sort the queue and copy what to send and to whom
//  2. without the lock: write to the neighbor inboxes (this can block)
func (d *Device) Sync() {
	now := d.now()
	d.mu.Lock()
	// Drop expired messages first: they are never sent again.
	var expired []Message
	kept := d.Queue[:0] // reuse the same array; safe because we hold the lock
	for _, msg := range d.Queue {
		if msg.expiredAt(now) {
			expired = append(expired, msg)
		} else {
			kept = append(kept, msg)
		}
	}
	d.Queue = kept

	sort.SliceStable(d.Queue, func(i, j int) bool {
		return d.Queue[i].Priority < d.Queue[j].Priority
	})

	var outgoing []Message
	for _, msg := range d.Queue {
		if msg.HopCount >= MaxHops {
			continue
		}
		msg.HopCount++ // msg is a copy, so the queued message is unchanged
		outgoing = append(outgoing, msg)
	}
	// Copy the slice: Disconnect may change d.Neighbors after we unlock.
	neighbors := append([]*Device(nil), d.Neighbors...)
	d.mu.Unlock()

	for _, msg := range expired {
		d.emitExpired(msg)
	}

	for _, msg := range outgoing {
		for _, neighbor := range neighbors {
			neighbor.Inbox <- msg
		}
	}
}

// start is the device's main loop: it handles inbox messages one at a time.
func (d *Device) start() {
	for msg := range d.Inbox {
		// Verify BEFORE marking seen: otherwise a forged message with a
		// real ID would make us drop the real one when it arrives.
		if d.trust != nil {
			if err := d.trust.Verify(msg); err != nil {
				d.stats.addRejected()
				d.emit(Event{Type: EventRejected, DeviceID: d.ID, MessageID: msg.ID,
					Text: fmt.Sprintf("[Device %s] FORGED MESSAGE REJECTED: %v", d.ID, err)})
				continue // not stored, not forwarded
			}
		}
		// Replay protection: an old message, even with a valid signature,
		// is dropped.
		if msg.expiredAt(d.now()) {
			d.emitExpired(msg)
			continue
		}

		d.mu.Lock()
		if d.Seen[msg.ID] {
			d.mu.Unlock()
			d.stats.addDuplicate()
			continue
		}
		d.Seen[msg.ID] = true
		d.mu.Unlock()

		// After the seen check, so a message heard from many neighbors
		// counts once.
		if d.overRateLimit(msg) {
			d.stats.addRateLimited()
			d.emit(Event{Type: EventRateLimited, DeviceID: d.ID, MessageID: msg.ID, PeerID: msg.SenderID,
				Text: fmt.Sprintf("[Device %s] %s is sending too fast: %s DROPPED", d.ID, msg.SenderID, msg.ID)})
			continue
		}

		msg.Path = withHop(msg.Path, d.ID)

		if msg.AckFor != "" {
			d.handleReceipt(msg)
		} else {
			d.handleMessage(msg)
		}
	}
}

// overRateLimit counts msg against its sender and reports whether the
// sender has gone over RateLimit in the current window. Receipts and
// emergencies are never limited. Called only from start().
func (d *Device) overRateLimit(msg Message) bool {
	if msg.AckFor != "" || msg.Priority == Emergency {
		return false
	}
	now := d.now()
	w := d.rates[msg.SenderID]
	if w == nil || now.Sub(w.start) >= RateWindow {
		w = &rateWindow{start: now} // first message, or the window is over
		d.rates[msg.SenderID] = w
	}
	w.count++
	return w.count > RateLimit
}

// withHop returns a NEW slice: path followed by id.
// We can't just append(path, id): the sender gives every neighbor a copy
// of the same message, and those copies share one backing array. If two
// neighbors appended in place they could overwrite each other's entry.
func withHop(path []string, id string) []string {
	out := make([]string, len(path), len(path)+1)
	copy(out, path)
	return append(out, id)
}

// handleMessage delivers, rebroadcasts or forwards a normal message.
func (d *Device) handleMessage(msg Message) {
	isFinalDestination := msg.Recipient == d.ID

	var dropped *Message // set if the queue was full
	d.mu.Lock()
	if isFinalDestination {
		// Send a receipt back so relays stop carrying the message.
		receipt := Message{
			ID:        "ack:" + msg.ID,
			SenderID:  d.ID,
			Recipient: msg.SenderID,
			Type:      TypeReceipt,
			Priority:  msg.Priority, // as urgent as the message it confirms
			Timestamp: d.now(),
			Body:      "delivered",
			AckFor:    msg.ID,
			Expiry:    msg.Expiry, // not needed once the original has expired
		}
		Sign(&receipt, d.key) // signed, so a forged receipt can't cancel a message
		d.Seen[receipt.ID] = true // ignore our own receipt if it echoes back
		dropped = d.enqueue(receipt)
	} else {
		dropped = d.enqueue(msg)
	}
	d.mu.Unlock()
	d.reportDropped(dropped)

	// Report what happened as an event, then pass messages on.
	// Count before emitting, so anyone who sees the event sees the count.
	if isFinalDestination || msg.Recipient == "ALL" {
		d.stats.addDelivered(d.now().Sub(msg.Timestamp))
	}

	// msg.Path is never changed after this point, so events can share it.
	if isFinalDestination {
		d.emit(Event{Type: EventDelivered, DeviceID: d.ID, MessageID: msg.ID, Path: msg.Path,
			Text: fmt.Sprintf("[Device %s] FINAL DESTINATION reached: %s", d.ID, msg.Body)})
	} else if msg.Recipient == "ALL" {
		d.emit(Event{Type: EventBroadcast, DeviceID: d.ID, MessageID: msg.ID, Path: msg.Path,
			Text: fmt.Sprintf("[Device %s] BROADCAST received: %s", d.ID, msg.Body)})
	} else {
		d.emit(Event{Type: EventForwarded, DeviceID: d.ID, MessageID: msg.ID, Path: msg.Path,
			Text: fmt.Sprintf("[Device %s] Not for me. Forwarding...", d.ID)})
	}
	d.Sync()
}

// handleReceipt stops carrying a delivered message and passes the receipt
// on toward the original sender.
func (d *Device) handleReceipt(receipt Message) {
	isOriginalSender := receipt.Recipient == d.ID

	var dropped *Message // set if the queue was full
	d.mu.Lock()
	d.Seen[receipt.AckFor] = true // late copies of the original get dropped
	d.removeFromQueue(receipt.AckFor)
	if !isOriginalSender {
		dropped = d.enqueue(receipt)
	}
	d.mu.Unlock()
	d.reportDropped(dropped)

	if isOriginalSender {
		d.emit(Event{Type: EventAcked, DeviceID: d.ID, MessageID: receipt.AckFor,
			Text: fmt.Sprintf("[Device %s] Message %s confirmed DELIVERED", d.ID, receipt.AckFor)})
		return
	}
	d.emit(Event{Type: EventCleared, DeviceID: d.ID, MessageID: receipt.AckFor,
		Text: fmt.Sprintf("[Device %s] %s was delivered, no longer carrying it", d.ID, receipt.AckFor)})
	d.Sync()
}

// enqueue adds msg to the queue. If the queue is then over maxQueue, it
// removes the least urgent message (the oldest among equals, which is the
// first one in queue order) and returns it. That can be msg itself.
// The caller must hold d.mu.
func (d *Device) enqueue(msg Message) *Message {
	d.Queue = append(d.Queue, msg)
	if len(d.Queue) <= d.maxQueue {
		return nil
	}
	victim := 0
	for i, m := range d.Queue {
		if m.Priority > d.Queue[victim].Priority { // strictly greater: keeps the oldest
			victim = i
		}
	}
	dropped := d.Queue[victim]
	d.Queue = append(d.Queue[:victim], d.Queue[victim+1:]...)
	return &dropped
}

// reportDropped emits an event for a message enqueue pushed out (nil = none).
// Call it after unlocking.
func (d *Device) reportDropped(m *Message) {
	if m == nil {
		return
	}
	d.stats.addQueueDropped()
	d.emit(Event{Type: EventDropped, DeviceID: d.ID, MessageID: m.ID,
		Text: fmt.Sprintf("[Device %s] Queue full: dropped %s (least urgent)", d.ID, m.ID)})
}

func (d *Device) emitExpired(msg Message) {
	d.stats.addExpired()
	d.emit(Event{Type: EventExpired, DeviceID: d.ID, MessageID: msg.ID,
		Text: fmt.Sprintf("[Device %s] %s EXPIRED, dropped", d.ID, msg.ID)})
}

// removeFromQueue deletes the message with the given ID from the queue.
// The caller must hold d.mu.
func (d *Device) removeFromQueue(id string) {
	kept := d.Queue[:0] // reuse the same array; safe because we hold the lock
	for _, m := range d.Queue {
		if m.ID != id {
			kept = append(kept, m)
		}
	}
	d.Queue = kept
}