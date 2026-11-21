package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestController builds the demo network with a controller on top.
// The runner sees every event, so tests can wait on it.
func newTestController(t *testing.T) (*Controller, *Runner) {
	t.Helper()
	r := NewRunner()
	n := NewNetwork(r.Observe)
	mustDo(t, SetupDemo(n))
	return NewController(n, r, noPause), r
}

// call sends a request to the controller and returns the status code.
func call(c *Controller, method, url string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	c.ServeHTTP(rec, httptest.NewRequest(method, url, nil))
	return rec
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
	}
}

func getTopology(t *testing.T, c *Controller) Topology {
	t.Helper()
	rec := call(c, "GET", "/api/topology")
	wantStatus(t, rec, http.StatusOK)
	var top Topology
	if err := json.Unmarshal(rec.Body.Bytes(), &top); err != nil {
		t.Fatal(err)
	}
	return top
}

func TestTopologyListsDemoNetwork(t *testing.T) {
	c, _ := newTestController(t)
	top := getTopology(t, c)
	if len(top.Devices) != len(DemoDevices) || len(top.Links) != len(DemoLinks) {
		t.Fatalf("got %d devices, %d links", len(top.Devices), len(top.Links))
	}
	for _, l := range top.Links {
		if !l.Up {
			t.Fatalf("link %s-%s down at start", l.A, l.B)
		}
	}
}

func TestCutLinkEndpoint(t *testing.T) {
	c, r := newTestController(t)
	wantStatus(t, call(c, "POST", "/api/link/cut?a="+Market+"&b="+Pumps), http.StatusNoContent)

	if err := r.waitFor(Expect{EventLinkCut, Market, ""}); err != nil {
		t.Fatal(err)
	}
	for _, l := range getTopology(t, c).Links {
		if makeLinkKey(l.A, l.B) == makeLinkKey(Market, Pumps) && l.Up {
			t.Fatal("topology still shows the link up")
		}
	}
}

func TestKillDeviceEndpoint(t *testing.T) {
	c, _ := newTestController(t)
	wantStatus(t, call(c, "POST", "/api/device/kill?id="+Relay), http.StatusNoContent)
	for _, d := range getTopology(t, c).Devices {
		if d.ID == Relay && d.Alive {
			t.Fatal("relay still alive")
		}
	}
	wantStatus(t, call(c, "POST", "/api/device/kill?id=ghost"), http.StatusBadRequest)
}

func TestWrongMethodIsRejected(t *testing.T) {
	c, _ := newTestController(t)
	wantStatus(t, call(c, "GET", "/api/device/kill?id="+Relay), http.StatusMethodNotAllowed)
}

func TestBadBoolIsRejected(t *testing.T) {
	c, _ := newTestController(t)
	wantStatus(t, call(c, "POST", "/api/verification?on=maybe"), http.StatusBadRequest)
}

func TestSendCheckInEndpoint(t *testing.T) {
	c, r := newTestController(t)
	wantStatus(t, call(c, "POST", "/api/send/checkin"), http.StatusNoContent) // defaults: Maya -> Dad

	// IDs are generated, so wait for Maya's receipt by looking it up.
	if err := r.waitFor(Expect{EventAcked, Maya, "manual-1"}); err != nil {
		t.Fatal(err)
	}
}

func TestEmergencyEndpoint(t *testing.T) {
	c, r := newTestController(t)
	wantStatus(t, call(c, "POST", "/api/send/emergency"), http.StatusNoContent) // from the Warden
	if err := r.waitFor(Expect{EventBroadcast, Pumps, "manual-1"}); err != nil {
		t.Fatal(err)
	}
}

func TestAttackEndpointIsRejected(t *testing.T) {
	c, r := newTestController(t)
	wantStatus(t, call(c, "POST", "/api/attack"), http.StatusNoContent) // targets Maya
	if err := r.waitFor(Expect{EventRejected, Maya, "forged-1"}); err != nil {
		t.Fatal(err)
	}
}

func TestStartRunsScenarioOnce(t *testing.T) {
	c, r := newTestController(t)
	wantStatus(t, call(c, "POST", "/api/start"), http.StatusAccepted)
	wantStatus(t, call(c, "POST", "/api/start"), http.StatusConflict)

	// The scenario runs in the background: wait for its last beat.
	if err := r.waitFor(Expect{EventBroadcast, Dad, MsgForged}); err != nil {
		t.Fatal(err)
	}
}

// waitIdle polls /api/status until no part is running.
func waitIdle(t *testing.T, c *Controller) Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		rec := call(c, "GET", "/api/status")
		wantStatus(t, rec, http.StatusOK)
		var s Status
		if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		if !s.Running {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatal("part still running after 10s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPhasesCoverEveryStepInOrder(t *testing.T) {
	next := 0
	for _, p := range Phases {
		if p.From != next || p.To <= p.From {
			t.Fatalf("phase %q covers [%d:%d], want to start at %d", p.Name, p.From, p.To, next)
		}
		next = p.To
	}
	if next != len(VideoScenario()) {
		t.Fatalf("phases end at step %d, scenario has %d", next, len(VideoScenario()))
	}
}

func TestPhasesRunInOrder(t *testing.T) {
	c, r := newTestController(t)
	wantStatus(t, call(c, "POST", "/api/phase?n=2"), http.StatusConflict) // part 1 first
	wantStatus(t, call(c, "POST", "/api/phase?n=9"), http.StatusBadRequest)

	for n := 1; n <= len(Phases); n++ {
		wantStatus(t, call(c, "POST", fmt.Sprintf("/api/phase?n=%d", n)), http.StatusAccepted)
		if s := waitIdle(t, c); s.Done != n {
			t.Fatalf("after part %d, done = %d", n, s.Done)
		}
	}
	wantStatus(t, call(c, "POST", "/api/phase?n=1"), http.StatusConflict) // already ran
	if err := r.waitFor(Expect{EventBroadcast, Dad, MsgForged}); err != nil {
		t.Fatal(err)
	}
}

func TestFullScenarioRefusedAfterAPhase(t *testing.T) {
	c, _ := newTestController(t)
	wantStatus(t, call(c, "POST", "/api/phase?n=1"), http.StatusAccepted)
	wantStatus(t, call(c, "POST", "/api/start"), http.StatusConflict)
	waitIdle(t, c)
}

func TestPhaseRefusedDuringFullScenario(t *testing.T) {
	c, _ := newTestController(t)
	wantStatus(t, call(c, "POST", "/api/start"), http.StatusAccepted)
	wantStatus(t, call(c, "POST", "/api/phase?n=1"), http.StatusConflict)
	waitIdle(t, c)
}
