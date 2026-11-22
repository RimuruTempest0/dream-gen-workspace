package core

import (
	"testing"
	"time"
)

// Opening the image-service settings checks every image service that has no
// recent free check, so their health is not left "unknown" after a restart.
func TestOpeningTheSettingsChecksOnlyStaleImageServices(t *testing.T) {
	s := testService(t)
	a := &fakeGateway{models: []string{"test-image"}}
	b := &fakeGateway{modelsErr: 401}
	ca := gatewayConnection(t, s, "A", newFakeGateway(t, a).URL+"/v1")
	cb := gatewayConnection(t, s, "B", newFakeGateway(t, b).URL+"/v1")

	health, e := s.CheckStaleGateways()
	if e != nil {
		t.Fatal(e)
	}
	states := map[string]GatewayHealth{}
	for _, h := range health {
		states[h.ConnectionID] = h
	}
	if states[ca.ID].State != "healthy" || states[ca.ID].CheckedAt == "" {
		t.Fatalf("A after the first open: %+v", states[ca.ID])
	}
	if states[cb.ID].State != "down" {
		t.Fatalf("B after the first open: %+v", states[cb.ID])
	}

	// Opening the page again right away reads nothing new.
	if _, e := s.CheckStaleGateways(); e != nil {
		t.Fatal(e)
	}
	if a.modelHits.Load() != 1 || b.modelHits.Load() != 1 {
		t.Fatal("a fresh check must not be repeated", a.modelHits.Load(), b.modelHits.Load())
	}

	// Once the last check is older than the regular interval, it runs again.
	old := probeInterval
	probeInterval = time.Millisecond
	t.Cleanup(func() { probeInterval = old })
	time.Sleep(5 * time.Millisecond)
	if _, e := s.CheckStaleGateways(); e != nil {
		t.Fatal(e)
	}
	if a.modelHits.Load() != 2 || b.modelHits.Load() != 2 {
		t.Fatal("a stale check must run again", a.modelHits.Load(), b.modelHits.Load())
	}
	if a.imageHits.Load() != 0 || b.imageHits.Load() != 0 {
		t.Fatal("the check on open must never generate")
	}
}

// While a free check is running its image service reads "checking", not "unknown".
func TestHealthSaysCheckingWhileTheFreeCheckRuns(t *testing.T) {
	s := testService(t)
	a := &fakeGateway{models: []string{"test-image"}}
	ca := gatewayConnection(t, s, "A", newFakeGateway(t, a).URL+"/v1")
	s.mu.Lock()
	s.probing[ca.ID] = true
	s.mu.Unlock()
	health, e := s.GatewayHealth()
	if e != nil {
		t.Fatal(e)
	}
	if len(health) != 1 || !health[0].Checking || health[0].State != "unknown" {
		t.Fatalf("health while checking: %+v", health)
	}
}
