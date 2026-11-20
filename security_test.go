package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

// newTestTrust trusts devices A and B; only A may send emergencies.
func newTestTrust() *Trust {
	tr := NewTrust()
	tr.Register("A", demoKey("A").Public().(ed25519.PublicKey))
	tr.Register("B", demoKey("B").Public().(ed25519.PublicKey))
	tr.AuthorizeEmergency("A")
	return tr
}

func signedMsg(from, to string, typ MessageType, body string) Message {
	m := NewMessage("m1", from, to, typ, body)
	Sign(&m, demoKey(from))
	return m
}

func TestVerifyAcceptsSignedMessage(t *testing.T) {
	if err := newTestTrust().Verify(signedMsg("B", "A", TypeCheckIn, "ok")); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyAllowsHopCountChange(t *testing.T) {
	m := signedMsg("B", "A", TypeCheckIn, "ok")
	m.HopCount = 5                   // relays change these,
	m.Path = []string{"B", "X", "Y"} // so they are not signed
	if err := newTestTrust().Verify(m); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsTamperedMessage(t *testing.T) {
	tr := newTestTrust()
	tamper := []func(*Message){
		func(m *Message) { m.Body = "Level 38 is safe, return" },
		func(m *Message) { m.Recipient = "ALL" },
		func(m *Message) { m.Priority = Emergency },
		func(m *Message) { m.Type = TypeGeneral },
		func(m *Message) { m.Timestamp = m.Timestamp.Add(1) },
		func(m *Message) { m.Expiry = m.Timestamp },
		func(m *Message) { m.AckFor = "other" },
		func(m *Message) { m.Signature = nil },
	}
	for i, change := range tamper {
		m := signedMsg("B", "A", TypeCheckIn, "ok")
		change(&m)
		if err := tr.Verify(m); err == nil {
			t.Errorf("tamper case %d: expected rejection", i)
		}
	}
}

func TestVerifyRejectsUnknownSender(t *testing.T) {
	m := NewMessage("m1", "Directorate", "A", TypeGeneral, "return home")
	_, attackerKey, _ := ed25519.GenerateKey(rand.Reader)
	Sign(&m, attackerKey) // validly signed, but by a key nobody trusts
	if err := newTestTrust().Verify(m); err == nil {
		t.Fatal("expected rejection of unknown sender")
	}
}

func TestVerifyRejectsWrongKeyForKnownSender(t *testing.T) {
	m := NewMessage("m1", "B", "A", TypeGeneral, "hi")
	_, attackerKey, _ := ed25519.GenerateKey(rand.Reader)
	Sign(&m, attackerKey) // claims to be B
	if err := newTestTrust().Verify(m); err == nil {
		t.Fatal("expected rejection of message signed with someone else's key")
	}
}

func TestOnlyAuthorizedSendersMayUseEmergency(t *testing.T) {
	tr := newTestTrust()
	if err := tr.Verify(signedMsg("A", "ALL", TypeEmergency, "BLACKOUT")); err != nil {
		t.Fatalf("authorized emergency rejected: %v", err)
	}
	if err := tr.Verify(signedMsg("B", "ALL", TypeEmergency, "BLACKOUT")); err == nil {
		t.Fatal("unauthorized emergency accepted")
	}
}

func TestDisabledTrustAcceptsAnything(t *testing.T) {
	tr := newTestTrust()
	tr.SetEnabled(false)
	forged := NewMessage("m1", "B", "A", TypeGeneral, "unsigned")
	if err := tr.Verify(forged); err != nil {
		t.Fatalf("disabled trust rejected: %v", err)
	}
}

// The key guarantee: a forged message carrying a real message's ID is
// rejected BEFORE being marked seen, so the real one still gets through.
func TestForgedMessageWithRealIDDoesNotBlockRealOne(t *testing.T) {
	n, c := newTestNetwork("A", "B", "C")
	mustDo(t, n.Link("A", "B"))
	mustDo(t, n.Link("B", "C"))

	forged := NewMessage("m1", "A", "C", TypeGeneral, "Level 38 is safe, return")
	mustDo(t, n.Inject("B", forged)) // unsigned, straight into B's radio
	c.waitFor(t, EventRejected, "B", "m1")

	real := NewMessage("m1", "A", "C", TypeGeneral, "Stay put, air is bad")
	mustDo(t, n.Send(real))
	c.waitFor(t, EventDelivered, "C", "m1")
}

func TestForgedMessageIsNotForwarded(t *testing.T) {
	n, c := newTestNetwork("A", "B", "C")
	mustDo(t, n.Link("A", "B"))
	mustDo(t, n.Link("B", "C"))

	mustDo(t, n.Inject("B", NewMessage("fake", "A", "C", TypeGeneral, "return")))
	// Marker sent afterwards along the same path: once it arrives, the
	// forged message would have arrived too if B had forwarded it.
	mustDo(t, n.Send(testMsg("marker", "A", "C")))

	events := c.waitFor(t, EventDelivered, "C", "marker")
	for _, e := range events {
		if e.MessageID == "fake" && e.Type != EventRejected {
			t.Fatalf("forged message got past B: %+v", e)
		}
	}
}

func TestWithoutVerificationForgedMessageGetsThrough(t *testing.T) {
	n, c := newTestNetwork("A", "B", "C")
	mustDo(t, n.Link("A", "B"))
	mustDo(t, n.Link("B", "C"))
	n.SetVerification(false)

	mustDo(t, n.Inject("B", NewMessage("fake", "A", "C", TypeGeneral, "return")))
	c.waitFor(t, EventDelivered, "C", "fake")
}
