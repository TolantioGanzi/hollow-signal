package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
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
	n       *Network
	runner  *Runner
	pause   func(time.Duration) // passed to the scenario: time.Sleep live, no-op in tests
	started atomic.Bool         // the scenario can run only once per server
	nextID  atomic.Int64        // for unique IDs of messages sent from buttons
	mux     *http.ServeMux
}

func NewController(n *Network, runner *Runner, pause func(time.Duration)) *Controller {
	c := &Controller{n: n, runner: runner, pause: pause, mux: http.NewServeMux()}

	// "METHOD /path" patterns: the mux answers 405 for any other method.
	c.mux.HandleFunc("GET /api/topology", c.topology)
	c.mux.HandleFunc("POST /api/start", c.start)
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

// start runs the video scenario in the background and answers at once.
func (c *Controller) start(w http.ResponseWriter, r *http.Request) {
	// CompareAndSwap: only the first caller flips false -> true.
	if !c.started.CompareAndSwap(false, true) {
		http.Error(w, "scenario already started", http.StatusConflict)
		return
	}
	go func() {
		if err := c.runner.Run(c.n, VideoScenario(), c.pause); err != nil {
			c.n.Notice("Scenario error: " + err.Error())
		}
	}()
	w.WriteHeader(http.StatusAccepted)
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
