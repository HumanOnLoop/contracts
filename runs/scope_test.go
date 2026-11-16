package runs

import (
	"testing"

	"github.com/HumanOnLoop/contracts/events"
)

func TestScopeAllows(t *testing.T) {
	s := ScopeStatus{Version: 1, Confirmed: true, Repos: []events.Repo{"playground"}}
	if !s.Allows([]events.Repo{"playground"}) {
		t.Error("in scope refused")
	}
	if s.Allows([]events.Repo{"playground", "fixture-python"}) {
		t.Error("out of scope allowed")
	}
	if (ScopeStatus{Version: 1, Repos: []events.Repo{"playground"}}).Allows([]events.Repo{"playground"}) {
		t.Error("unconfirmed scope allowed")
	}
	if !(ScopeStatus{Legacy: true}).Allows([]events.Repo{"anything"}) {
		t.Error("legacy run refused")
	}
}
