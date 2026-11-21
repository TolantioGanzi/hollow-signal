package main

import (
	"fmt"
	"sync"
	"time"
)

// Expect is an event a step waits for before the scenario moves on.
type Expect struct {
	Type      EventType
	DeviceID  string
	MessageID string
}

// Step is one beat of a scripted scenario.
type Step struct {
	Caption string               // shown in the live feed as a notice
	Do      func(*Network) error // what happens in this beat
	WaitFor []Expect             // events that must happen before moving on
	Pause   time.Duration        // extra time for viewers (skipped in tests)
}

// Runner plays scenarios. It watches every network event (pass
// r.Observe as the network's emitter), so a step can wait for real
// outcomes instead of hoping a sleep was long enough.
type Runner struct {
	Timeout time.Duration // how long to wait for each expected event

	mu      sync.Mutex
	history []Event
	// changed is closed when a new event arrives, then replaced with a
	// fresh channel. Closing wakes EVERY waiter (a send would wake only
	// one), so several goroutines can wait at the same time.
	changed chan struct{}
}

func NewRunner() *Runner {
	return &Runner{Timeout: 5 * time.Second, changed: make(chan struct{})}
}

// Observe records an event. It never blocks, so devices are never held up.
func (r *Runner) Observe(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.history = append(r.history, e)
	close(r.changed)
	r.changed = make(chan struct{})
}

// Events returns a copy of every event observed so far, in order.
func (r *Runner) Events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.history...)
}

// happened reports whether w has been seen, and returns the channel that
// will be closed on the next event. Both come from under the same lock,
// so no event can slip in between the check and the wait.
func (r *Runner) happened(w Expect) (bool, chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.history {
		if e.Type == w.Type && e.DeviceID == w.DeviceID && e.MessageID == w.MessageID {
			return true, nil
		}
	}
	return false, r.changed
}

func (r *Runner) waitFor(w Expect) error {
	timeout := time.After(r.Timeout)
	for {
		found, changed := r.happened(w)
		if found {
			return nil
		}
		select {
		case <-changed: // something new arrived: check again
		case <-timeout:
			return fmt.Errorf("timed out waiting for %s on %q for %q", w.Type, w.DeviceID, w.MessageID)
		}
	}
}

// Run plays the steps in order. pause is time.Sleep live and a no-op in tests.
func (r *Runner) Run(n *Network, steps []Step, pause func(time.Duration)) error {
	for i, s := range steps {
		if s.Caption != "" {
			n.Notice(s.Caption)
		}
		if err := s.Do(n); err != nil {
			return fmt.Errorf("step %d (%s): %w", i+1, s.Caption, err)
		}
		for _, w := range s.WaitFor {
			if err := r.waitFor(w); err != nil {
				return fmt.Errorf("step %d (%s): %w", i+1, s.Caption, err)
			}
		}
		pause(s.Pause)
	}
	return nil
}

// ---- The video scenario ----

// Device IDs. The number is the level, used by the UI to place nodes.
const (
	Pumps   = "L50-Pumps"
	Shelter = "L50-Shelter"
	Maya    = "L40-Maya"
	Market  = "L40-Market"
	Clinic  = "L30-Clinic"
	Relay   = "L30-Relay"
	Shaft   = "L20-Shaft"
	School  = "L20-School"
	Dad     = "L12-Dad"
	Warden  = "L10-Warden"
)

// Message IDs, fixed so the test and the UI can refer to them.
const (
	MsgMorning  = "checkin-morning"
	MsgCheckIn  = "checkin-maya"
	MsgChatter1 = "chatter-1"
	MsgChatter2 = "chatter-2"
	MsgBlackout = "alert-blackout"
	MsgForged   = "forged-38"
)

// DemoDevices lists every device, top of the Hollow to the bottom.
var DemoDevices = []string{Warden, Dad, Shaft, School, Relay, Clinic, Maya, Market, Shelter, Pumps}

// DemoLinks are the pairs of devices in radio range of each other.
var DemoLinks = [][2]string{
	{Warden, Dad}, {Warden, School},
	{Dad, Shaft},
	{Shaft, School}, {Shaft, Relay},
	{School, Clinic},
	{Relay, Clinic}, {Relay, Maya},
	{Clinic, Market},
	{Maya, Market}, {Maya, Shelter},
	{Market, Pumps},
	{Shelter, Pumps},
}

// forgedOrder is the Directorate's fake message. It claims to come from
// the Warden but carries no valid signature.
func forgedOrder() Message {
	return NewMessage(MsgForged, Warden, "ALL", TypeEmergency,
		"DIRECTORATE ORDER: Level 38 is safe. Return to your homes.")
}

// SetupDemo builds the demo devices and links. main calls it at startup,
// so the map and the buttons work before the scenario runs.
func SetupDemo(n *Network) error {
	for _, id := range DemoDevices {
		if err := n.AddDevice(id); err != nil {
			return err
		}
	}
	for _, l := range DemoLinks {
		if err := n.Link(l[0], l[1]); err != nil {
			return err
		}
	}
	n.AuthorizeEmergency(Warden) // only the Warden may send emergencies
	return nil
}

// VideoScenario is the exact script of the demo video.
// It expects a network already built by SetupDemo.
func VideoScenario() []Step {
	return []Step{
		{
			Caption: "The Hollow, 2 billion people underground. The Directorate network is online.",
			Do: func(n *Network) error {
				n.SetDirectorate(true)
				return nil
			},
			Pause: 4 * time.Second,
		},
		{
			Caption: "A regular morning: Maya tells her dad she is heading to the market.",
			Do: func(n *Network) error {
				return n.Send(NewMessage(MsgMorning, Maya, Dad, TypeCheckIn, "Morning, Dad. Heading to the market."))
			},
			WaitFor: []Expect{
				{EventDelivered, Dad, MsgMorning},
				{EventAcked, Maya, MsgMorning},
			},
			Pause: 4 * time.Second,
		},
		{
			Caption: "Blackout. The Directorate network is SEVERED. A relay on Level 30 loses power.",
			Do: func(n *Network) error {
				n.SetDirectorate(false)
				return n.KillDevice(Relay)
			},
			WaitFor: []Expect{{EventDeviceKilled, Relay, ""}},
			Pause:   4 * time.Second,
		},
		{
			Caption: "Maya on Level 40 sends \"I'm okay\" to her dad on Level 12.",
			Do: func(n *Network) error {
				return n.Send(NewMessage(MsgCheckIn, Maya, Dad, TypeCheckIn, "Dad, I'm okay. Staying at the market."))
			},
			WaitFor: []Expect{
				{EventDelivered, Dad, MsgCheckIn},
				{EventAcked, Maya, MsgCheckIn},
			},
			Pause: 6 * time.Second,
		},
		{
			Caption: "The pumps on Level 50 are cut off. Market chatter piles up for them.",
			Do: func(n *Network) error {
				if err := n.CutLink(Market, Pumps); err != nil {
					return err
				}
				if err := n.CutLink(Shelter, Pumps); err != nil {
					return err
				}
				if err := n.Send(NewMessage(MsgChatter1, Market, Pumps, TypeGeneral, "Water ration moved to 0600.")); err != nil {
					return err
				}
				return n.Send(NewMessage(MsgChatter2, Market, Pumps, TypeGeneral, "Market closes early today."))
			},
			WaitFor: []Expect{
				{EventForwarded, Market, MsgChatter1},
				{EventForwarded, Market, MsgChatter2},
			},
			Pause: 3 * time.Second,
		},
		{
			Caption: "The Warden broadcasts an EMERGENCY blackout warning.",
			Do: func(n *Network) error {
				return n.Send(NewMessage(MsgBlackout, Warden, "ALL", TypeEmergency, "BLACKOUT on Levels 40-50 in 5 minutes. Move up now."))
			},
			WaitFor: []Expect{{EventBroadcast, Market, MsgBlackout}},
			Pause:   3 * time.Second,
		},
		{
			Caption: "A runner reaches the pumps. The emergency jumps ahead of the chatter.",
			Do: func(n *Network) error {
				return n.RestoreLink(Market, Pumps)
			},
			WaitFor: []Expect{
				{EventBroadcast, Pumps, MsgBlackout},
				{EventDelivered, Pumps, MsgChatter1},
				{EventDelivered, Pumps, MsgChatter2},
			},
			Pause: 6 * time.Second,
		},
		{
			Caption: "DIRECTORATE ATTACK: a forged order \"Level 38 is safe, return\" hits Maya's device.",
			Do: func(n *Network) error {
				return n.Inject(Maya, forgedOrder())
			},
			WaitFor: []Expect{{EventRejected, Maya, MsgForged}},
			Pause:   6 * time.Second,
		},
		{
			Caption: "Without signatures, the same forged order would spread to everyone.",
			Do: func(n *Network) error {
				n.SetVerification(false)
				return n.Inject(Maya, forgedOrder())
			},
			WaitFor: []Expect{{EventBroadcast, Dad, MsgForged}},
			Pause:   6 * time.Second,
		},
		{
			Caption: "The network is the people. No one can switch it off.",
			Do: func(n *Network) error {
				n.SetVerification(true)
				return nil
			},
		},
	}
}

// Phase is a named part of the video scenario that the UI can run on its
// own button. It covers VideoScenario()[From:To].
type Phase struct {
	Name     string
	From, To int
}

// Phases split the video scenario into the four parts of the demo, in
// order. Together they cover every step exactly once.
var Phases = []Phase{
	{"Regular day", 0, 2},
	{"Blackout", 2, 4},
	{"Emergency first", 4, 7},
	{"Directorate attack", 7, 10},
}
