package main

import (
	"crypto/ed25519"
	"fmt"
	"sort"
	"sync"
	"time"
)

// linkKey identifies a link between two devices. The IDs are stored in
// sorted order so that A-B and B-A are the same key.
type linkKey struct{ a, b string }

func makeLinkKey(a, b string) linkKey {
	if a > b {
		a, b = b, a
	}
	return linkKey{a, b}
}

// Network owns every device in the simulation. The scenario, the web
// server and tests all go through it instead of touching devices directly.
//
// It also remembers the topology: which pairs of devices are in range of
// each other (links), which links are cut, and which devices are dead.
// A link is active only if it is not cut and both ends are alive.
type Network struct {
	mu      sync.RWMutex // RLock for lookups, Lock for any change
	devices map[string]*Device
	links   map[linkKey]bool // true = up, false = cut
	dead    map[string]bool
	trust   *Trust // shared by every device
	now     Clock  // shared by every device
	stats   *Stats // shared by every device
	emit    Emitter
}

// NewNetwork creates an empty network that uses the real clock. All
// devices it creates will send their events to emit.
func NewNetwork(emit Emitter) *Network {
	return NewNetworkWithClock(emit, time.Now)
}

// NewNetworkWithClock is NewNetwork with a custom clock (tests use a fake one).
func NewNetworkWithClock(emit Emitter, now Clock) *Network {
	if emit == nil {
		emit = func(Event) {}
	}
	return &Network{
		now:     now,
		stats:   &Stats{},
		devices: make(map[string]*Device),
		links:   make(map[linkKey]bool),
		dead:    make(map[string]bool),
		trust:   NewTrust(),
		emit:    emit,
	}
}

// AddDevice creates a device with the given ID and starts its goroutine.
func (n *Network) AddDevice(id string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if _, exists := n.devices[id]; exists {
		return fmt.Errorf("device %q already exists", id)
	}
	d := NewDevice(id, n.emit)
	d.trust = n.trust // set before start(), so no race
	d.now = n.now
	d.stats = n.stats
	n.trust.Register(id, d.key.Public().(ed25519.PublicKey))
	n.devices[id] = d
	go d.start()
	return nil
}

// AuthorizeEmergency allows a device (e.g. the Level-10 warden) to send
// emergency messages. Everyone else's emergencies are rejected.
func (n *Network) AuthorizeEmergency(id string) {
	n.trust.AuthorizeEmergency(id)
}

// SetVerification turns signature checking on or off for every device.
// Off is only for the demo, to show what a forged message does without it.
func (n *Network) SetVerification(on bool) {
	n.trust.SetEnabled(on)
	state := "ON"
	if !on {
		state = "OFF"
	}
	n.Notice("Signature verification " + state)
}

// lookup finds a device by ID. The caller must already hold n.mu
// (RWMutex is not re-entrant, so we can't lock again here).
func (n *Network) lookup(id string) (*Device, error) {
	d, ok := n.devices[id]
	if !ok {
		return nil, fmt.Errorf("unknown device %q", id)
	}
	return d, nil
}

// lookupPair finds both ends of a link. The caller must hold n.mu.
func (n *Network) lookupPair(a, b string) (*Device, *Device, error) {
	da, err := n.lookup(a)
	if err != nil {
		return nil, nil, err
	}
	db, err := n.lookup(b)
	if err != nil {
		return nil, nil, err
	}
	return da, db, nil
}

// Link records that two devices are in range and connects them.
func (n *Network) Link(a, b string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	da, db, err := n.lookupPair(a, b)
	if err != nil {
		return err
	}
	key := makeLinkKey(a, b)
	if _, exists := n.links[key]; exists {
		return fmt.Errorf("%s and %s are already linked", a, b)
	}
	n.links[key] = true
	if !n.dead[a] && !n.dead[b] {
		da.Connect(db)
	}
	return nil
}

// CutLink takes an existing link down. Queued messages wait for another path.
func (n *Network) CutLink(a, b string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	da, db, err := n.lookupPair(a, b)
	if err != nil {
		return err
	}
	key := makeLinkKey(a, b)
	if _, exists := n.links[key]; !exists {
		return fmt.Errorf("%s and %s were never linked", a, b)
	}
	n.links[key] = false
	da.Disconnect(db)
	n.emit(Event{Type: EventLinkCut, DeviceID: a, PeerID: b,
		Text: fmt.Sprintf("Link %s <-> %s CUT", a, b)})
	return nil
}

// RestoreLink brings a cut link back up. Connecting triggers a Sync, so
// waiting messages move on right away.
func (n *Network) RestoreLink(a, b string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	da, db, err := n.lookupPair(a, b)
	if err != nil {
		return err
	}
	key := makeLinkKey(a, b)
	if _, exists := n.links[key]; !exists {
		return fmt.Errorf("%s and %s were never linked", a, b)
	}
	if n.links[key] {
		return nil // already up
	}
	n.links[key] = true
	// Emit first, so the event comes before any delivery it causes.
	n.emit(Event{Type: EventLinkRestored, DeviceID: a, PeerID: b,
		Text: fmt.Sprintf("Link %s <-> %s RESTORED", a, b)})
	if !n.dead[a] && !n.dead[b] {
		da.Connect(db)
	}
	return nil
}

// otherEnd returns the other device of a link, or "" if id isn't part of it.
func (k linkKey) otherEnd(id string) string {
	switch id {
	case k.a:
		return k.b
	case k.b:
		return k.a
	}
	return ""
}

// KillDevice takes a device offline: all its links go down, but it keeps
// its queue (like a phone that is switched off).
func (n *Network) KillDevice(id string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	d, err := n.lookup(id)
	if err != nil {
		return err
	}
	if n.dead[id] {
		return nil // already dead
	}
	n.dead[id] = true
	for key, up := range n.links {
		other := key.otherEnd(id)
		if up && other != "" {
			d.Disconnect(n.devices[other]) // no-op if they weren't connected
		}
	}
	n.emit(Event{Type: EventDeviceKilled, DeviceID: id,
		Text: fmt.Sprintf("Device %s went OFFLINE", id)})
	return nil
}

// ReviveDevice brings a dead device back and reconnects its active links.
func (n *Network) ReviveDevice(id string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	d, err := n.lookup(id)
	if err != nil {
		return err
	}
	if !n.dead[id] {
		return nil // already alive
	}
	delete(n.dead, id)
	// Emit first, so the event comes before any delivery it causes.
	n.emit(Event{Type: EventDeviceRevived, DeviceID: id,
		Text: fmt.Sprintf("Device %s is back ONLINE", id)})
	for key, up := range n.links {
		other := key.otherEnd(id)
		if up && other != "" && !n.dead[other] {
			d.Connect(n.devices[other])
		}
	}
	return nil
}

// Send puts a message into its sender's inbox, as if the user typed it
// on that device. The device then stores and forwards it as usual.
func (n *Network) Send(msg Message) error {
	n.mu.RLock()
	defer n.mu.RUnlock()

	d, err := n.lookup(msg.SenderID)
	if err != nil {
		return err
	}
	if n.dead[msg.SenderID] {
		return fmt.Errorf("device %q is offline", msg.SenderID)
	}
	// msg is a copy, so these changes don't affect the caller's value.
	// The send time comes from the network clock, so delays are measured
	// on the same clock as deliveries.
	msg.Timestamp = n.now()
	if msg.Expiry.IsZero() {
		msg.Expiry = n.now().Add(DefaultTTL)
	}
	Sign(&msg, d.key) // after setting Expiry, so it is covered
	d.Inbox <- msg
	return nil
}

// Inject is the Directorate attack: it puts a message straight into a
// device's inbox, as if received over the air, WITHOUT signing it.
// The message can claim any sender.
func (n *Network) Inject(deviceID string, msg Message) error {
	n.mu.RLock()
	defer n.mu.RUnlock()

	d, err := n.lookup(deviceID)
	if err != nil {
		return err
	}
	d.Inbox <- msg
	return nil
}

// SetDirectorate reports the state of the Directorate's central network
// for the UI banner. The simulation doesn't model that network itself:
// the point is that the mesh keeps working while it is down.
func (n *Network) SetDirectorate(online bool) {
	state := "ONLINE"
	if !online {
		state = "SEVERED"
	}
	n.emit(Event{Type: EventDirectorate, Text: state})
}

// DeviceInfo, LinkInfo and Topology describe the network for the UI map.
type DeviceInfo struct {
	ID    string `json:"id"`
	Alive bool   `json:"alive"`
}

type LinkInfo struct {
	A  string `json:"a"`
	B  string `json:"b"`
	Up bool   `json:"up"` // false = cut
}

type Topology struct {
	Devices []DeviceInfo `json:"devices"`
	Links   []LinkInfo   `json:"links"`
}

// Topology returns every device and link, sorted so the order is stable.
func (n *Network) Topology() Topology {
	n.mu.RLock()
	defer n.mu.RUnlock()

	var top Topology
	for id := range n.devices {
		top.Devices = append(top.Devices, DeviceInfo{ID: id, Alive: !n.dead[id]})
	}
	for key, up := range n.links {
		top.Links = append(top.Links, LinkInfo{A: key.a, B: key.b, Up: up})
	}
	sort.Slice(top.Devices, func(i, j int) bool { return top.Devices[i].ID < top.Devices[j].ID })
	sort.Slice(top.Links, func(i, j int) bool {
		if top.Links[i].A != top.Links[j].A {
			return top.Links[i].A < top.Links[j].A
		}
		return top.Links[i].B < top.Links[j].B
	})
	return top
}

// Metrics returns the current counters.
func (n *Network) Metrics() MetricsSnapshot {
	return n.stats.Snapshot()
}

// EmitMetrics sends the current counters to the UI as a metrics event.
// main calls it on a ticker; tests call it directly.
func (n *Network) EmitMetrics() {
	snap := n.Metrics()
	n.emit(Event{Type: EventMetrics, Metrics: &snap})
}

// Notice emits a free-text event that isn't tied to a device,
// e.g. "DIRECTORATE NETWORK SEVERED".
func (n *Network) Notice(text string) {
	n.emit(Event{Type: EventNotice, Text: text})
}
