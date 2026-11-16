package agents

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/HumanOnLoop/contracts/events"
)

func TestCriteriaPerTask(t *testing.T) {
	c := func(n ...int) *[]int { s := append([]int{}, n...); return &s }
	adr := TaskSpec{ID: "T-1", Role: events.AgentRoleImplementer, Criteria: c()}
	portal := TaskSpec{ID: "T-2", Role: events.AgentRoleImplementer, Criteria: c(1), DependsOn: []events.TaskID{"T-1"}}
	scoper := TaskSpec{ID: "T-3", Role: events.AgentRoleImplementer, Criteria: c(2)}
	old := TaskSpec{ID: "T-1", Role: events.AgentRoleImplementer}
	if adr.Tested() || !portal.Tested() || !old.Tested() {
		t.Error("Tested")
	}
	if got := old.Covered(3); len(got) != 3 || got[2] != 3 {
		t.Errorf("old Covered %v", got)
	}
	if got := adr.Covered(3); len(got) != 0 {
		t.Errorf("adr Covered %v", got)
	}
	if err := ValidateTasks([]TaskSpec{adr, portal, scoper}); err != nil {
		t.Error(err)
	}
	if err := ValidateCoverage([]TaskSpec{adr, portal, scoper}, 2); err != nil {
		t.Error(err)
	}
	if err := ValidateCoverage([]TaskSpec{old}, 5); err != nil {
		t.Errorf("a plan from before covers all: %v", err)
	}
	if err := ValidateCoverage([]TaskSpec{old, {ID: "T-2", Role: events.AgentRoleImplementer, Criteria: c(9)}}, 2); err == nil {
		t.Error("a mixed plan was accepted")
	}
	if err := ValidateCoverage([]TaskSpec{adr, portal}, 2); err == nil || !strings.Contains(err.Error(), "no task covers criterion 2") {
		t.Errorf("uncovered: %v", err)
	}
	if err := ValidateCoverage([]TaskSpec{adr, portal, scoper}, 1); err == nil {
		t.Error("a criterion the intent lacks was accepted")
	}
	for _, bad := range []*[]int{c(0), c(1, 1)} {
		if err := ValidateTasks([]TaskSpec{{ID: "T-1", Role: events.AgentRoleImplementer, Criteria: bad}}); err == nil {
			t.Errorf("criteria %v accepted", *bad)
		}
	}
	b, _ := json.Marshal(adr)
	if !strings.Contains(string(b), `"criteria":[]`) {
		t.Errorf("an empty list must survive JSON: %s", b)
	}
	if b, _ := json.Marshal(old); strings.Contains(string(b), "criteria") {
		t.Errorf("nil criteria marshalled: %s", b)
	}
}

func TestTaskModelAndEffort(t *testing.T) {
	for _, eff := range []string{"", EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax} {
		if err := ValidateTasks([]TaskSpec{{ID: "T-1", Role: events.AgentRoleImplementer, Model: "gemini-4-hydrogen-medium", Effort: eff}}); err != nil {
			t.Errorf("effort %q: %v", eff, err)
		}
	}
	if err := ValidateTasks([]TaskSpec{{ID: "T-1", Role: events.AgentRoleImplementer, Effort: "turbo"}}); err == nil {
		t.Error("unknown effort was accepted")
	}
}
