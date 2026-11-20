package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// demoKey returns a fixed private key for a device ID, derived from a
// seed. This gives the demo a "hardcoded" key registry that is the same on
// every run. A real deployment would generate keys on each phone and
// share only the public keys.
func demoKey(id string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("hollow-signal-demo:" + id)) // 32 bytes = ed25519 seed size
	return ed25519.NewKeyFromSeed(seed[:])
}

// signingBytes turns the signed fields into bytes in a fixed order.
// Every string is length-prefixed so fields can't be shifted into each
// other (e.g. ID "ab"+Sender "c" must differ from ID "a"+Sender "bc").
// HopCount is NOT included: relays change it on every hop.
func signingBytes(m Message) []byte {
	var buf bytes.Buffer
	writeString := func(s string) {
		binary.Write(&buf, binary.BigEndian, uint32(len(s)))
		buf.WriteString(s)
	}
	writeInt := func(v int64) {
		binary.Write(&buf, binary.BigEndian, v)
	}

	writeString(m.ID)
	writeString(m.SenderID)
	writeString(m.Recipient)
	writeString(string(m.Type))
	writeInt(int64(m.Priority))
	writeString(m.Body)
	writeString(m.AckFor)
	// Seconds + nanoseconds instead of UnixNano, which is undefined for
	// the zero time (used when there is no expiry).
	writeInt(m.Timestamp.Unix())
	writeInt(int64(m.Timestamp.Nanosecond()))
	writeInt(m.Expiry.Unix())
	writeInt(int64(m.Expiry.Nanosecond()))
	return buf.Bytes()
}

// Sign sets m.Signature using the sender's private key.
func Sign(m *Message, key ed25519.PrivateKey) {
	m.Signature = ed25519.Sign(key, signingBytes(*m))
}

// Trust is the shared list of who is trusted: one public key per device
// ID, plus which devices may send emergencies. Verification can be
// switched off to show in the demo what happens without it.
type Trust struct {
	mu        sync.RWMutex // devices read keys while new ones are registered
	keys      map[string]ed25519.PublicKey
	emergency map[string]bool
	enabled   atomic.Bool // atomic: flipped from the UI while devices verify
}

func NewTrust() *Trust {
	t := &Trust{
		keys:      make(map[string]ed25519.PublicKey),
		emergency: make(map[string]bool),
	}
	t.enabled.Store(true)
	return t
}

// Register trusts pub as the key of device id.
func (t *Trust) Register(id string, pub ed25519.PublicKey) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.keys[id] = pub
}

// AuthorizeEmergency allows device id to send emergency messages.
func (t *Trust) AuthorizeEmergency(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.emergency[id] = true
}

func (t *Trust) SetEnabled(on bool) {
	t.enabled.Store(on)
}

// Verify returns nil if the message may be accepted, or an error saying
// why not. The error text is shown in the UI.
func (t *Trust) Verify(m Message) error {
	if !t.enabled.Load() {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	pub, known := t.keys[m.SenderID]
	if !known {
		return fmt.Errorf("UNKNOWN SENDER %q", m.SenderID)
	}
	if !ed25519.Verify(pub, signingBytes(m), m.Signature) {
		return errors.New("INVALID SIGNATURE")
	}
	// Receipts copy the priority of the message they confirm, so they
	// are exempt; every other emergency needs an authorized sender.
	isEmergency := m.Type == TypeEmergency || m.Priority == Emergency
	if isEmergency && m.AckFor == "" && !t.emergency[m.SenderID] {
		return fmt.Errorf("%s IS NOT AUTHORIZED TO SEND EMERGENCIES", m.SenderID)
	}
	return nil
}
