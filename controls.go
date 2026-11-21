package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Controller exposes the operator buttons as HTTP endpoints under /api/.
// Parameters come from the query string, e.g.
//
//	POST /api/link/cut?a=L40-Market&b=L50-Pumps
//
// Success is 204 No Content; a bad parameter or a network error is 400
// with the reason as text.
type Controller struct {
	n      *Network
	runner *Runner
	pause  func(time.Duration) // passed to the scenario: time.Sleep live, no-op in tests
	// mu guards done and running. Phases run once per server, in order;
	// the full scenario runs only if no phase has run yet.
	mu      sync.Mutex
	done    int          // how many phases have finished (or are running)
	running bool         // a phase or the full scenario is playing right now
	nextID  atomic.Int64 // for unique IDs of messages sent from buttons
	mux     *http.ServeMux
}

func NewController(n *Network, runner *Runner, pause func(time.Duration)) *Controller {
	c := &Controller{n: n, runner: runner, pause: pause, mux: http.NewServeMux()}

	// "METHOD /path" patterns: the mux answers 405 for any other method.
	c.mux.HandleFunc("GET /api/topology", c.topology)
	c.mux.HandleFunc("POST /api/start", c.start)
	c.mux.HandleFunc("POST /api/phase", c.phase)
	c.mux.HandleFunc("GET /api/status", c.status)
	c.mux.HandleFunc("POST /api/directorate", c.directorate)
	c.mux.HandleFunc("POST /api/verification", c.verification)
	c.mux.HandleFunc("POST /api/link/cut", c.linkAction(n.CutLink))
	c.mux.HandleFunc("POST /api/link/restore", c.linkAction(n.RestoreLink))
	c.mux.HandleFunc("POST /api/device/kill", c.deviceAction(n.KillDevice))
	c.mux.HandleFunc("POST /api/device/revive", c.deviceAction(n.ReviveDevice))
	c.mux.HandleFunc("POST /api/send/checkin", c.sendCheckIn)
	c.mux.HandleFunc("POST /api/send/emergency", c.sendEmergency)
	c.mux.HandleFunc("POST /api/attack", c.attack)
	return c
}

func (c *Controller) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mux.ServeHTTP(w, r)
}

// reply writes 204 on success, or 400 with the error text.
func reply(w http.ResponseWriter, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// param returns a query parameter, or def if it is missing.
func param(r *http.Request, name, def string) string {
	if v := r.URL.Query().Get(name); v != "" {
		return v
	}
	return def
}

// boolParam parses a required true/false query parameter.
func boolParam(r *http.Request, name string) (bool, error) {
	v, err := strconv.ParseBool(r.URL.Query().Get(name))
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return v, nil
}

// newID returns a fresh message ID like "manual-1".
func (c *Controller) newID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, c.nextID.Add(1))
}

func (c *Controller) topology(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(c.n.Topology())
}

// start runs the whole video scenario in the background and answers at once.
// It is refused once any phase has run.
func (c *Controller) start(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	if c.running || c.done > 0 {
		c.mu.Unlock()
		http.Error(w, "scenario already started: restart the server to run it again", http.StatusConflict)
		return
	}
	c.running, c.done = true, len(Phases)
	c.mu.Unlock()
	c.play(VideoScenario())
	w.WriteHeader(http.StatusAccepted)
}

// phase runs one part of the scenario (?n=1..4) in the background.
// Parts must run in order, one at a time.
func (c *Controller) phase(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.URL.Query().Get("n"))
	if err != nil || n < 1 || n > len(Phases) {
		http.Error(w, fmt.Sprintf("n must be 1 to %d", len(Phases)), http.StatusBadRequest)
		return
	}
	c.mu.Lock()
	var refuse string
	switch {
	case c.running:
		refuse = "wait for the current part to finish"
	case n <= c.done:
		refuse = "that part already ran: restart the server to run it again"
	case n != c.done+1:
		refuse = fmt.Sprintf("run part %d (%s) first", c.done+1, Phases[c.done].Name)
	}
	if refuse != "" {
		c.mu.Unlock()
		http.Error(w, refuse, http.StatusConflict)
		return
	}
	c.running, c.done = true, n
	c.mu.Unlock()
	p := Phases[n-1]
	c.play(VideoScenario()[p.From:p.To])
	w.WriteHeader(http.StatusAccepted)
}

// play runs steps in the background, then clears the running flag.
func (c *Controller) play(steps []Step) {
	go func() {
		if err := c.runner.Run(c.n, steps, c.pause); err != nil {
			c.n.Notice("Scenario error: " + err.Error())
		}
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
	}()
}

// Status is what GET /api/status returns, so the UI can enable the right buttons.
type Status struct {
	Done    int  `json:"done"` // parts finished or running
	Running bool `json:"running"`
}

func (c *Controller) status(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	s := Status{Done: c.done, Running: c.running}
	c.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

func (c *Controller) directorate(w http.ResponseWriter, r *http.Request) {
	online, err := boolParam(r, "online")
	if err == nil {
		c.n.SetDirectorate(online)
	}
	reply(w, err)
}

func (c *Controller) verification(w http.ResponseWriter, r *http.Request) {
	on, err := boolParam(r, "on")
	if err == nil {
		c.n.SetVerification(on)
	}
	reply(w, err)
}

// linkAction makes a handler for CutLink or RestoreLink (?a=&b=).
func (c *Controller) linkAction(action func(a, b string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reply(w, action(param(r, "a", ""), param(r, "b", "")))
	}
}

// deviceAction makes a handler for KillDevice or ReviveDevice (?id=).
func (c *Controller) deviceAction(action func(id string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reply(w, action(param(r, "id", "")))
	}
}

func (c *Controller) sendCheckIn(w http.ResponseWriter, r *http.Request) {
	msg := NewMessage(c.newID("manual"),
		param(r, "from", Maya), param(r, "to", Dad), TypeCheckIn,
		param(r, "body", "I'm okay."))
	reply(w, c.n.Send(msg))
}

func (c *Controller) sendEmergency(w http.ResponseWriter, r *http.Request) {
	msg := NewMessage(c.newID("manual"),
		param(r, "from", Warden), "ALL", TypeEmergency,
		param(r, "body", "EMERGENCY: move to the upper levels."))
	reply(w, c.n.Send(msg))
}

// attack injects the Directorate's forged order into a device (default Maya).
func (c *Controller) attack(w http.ResponseWriter, r *http.Request) {
	forged := forgedOrder()
	forged.ID = c.newID("forged") // new ID each press, so repeats aren't deduplicated
	reply(w, c.n.Inject(param(r, "target", Maya), forged))
}
