package agents

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/HumanOnLoop/contracts/documents"
	"github.com/HumanOnLoop/contracts/events"
)

func TestValidateTasks(t *testing.T) {
	impl, tester := events.AgentRoleImplementer, events.AgentRoleTester
	for _, tc := range []struct {
		name  string
		tasks []TaskSpec
		err   string
	}{
		{"valid", []TaskSpec{{ID: "T-1", Role: impl}, {ID: "T-2", Role: tester, DependsOn: []events.TaskID{"T-1"}}}, ""},
		{"empty", nil, "no tasks"},
		{"no id", []TaskSpec{{Role: impl}}, "task 1 has no id"},
		{"duplicate", []TaskSpec{{ID: "T-1", Role: impl}, {ID: "T-1", Role: impl}}, "used twice"},
		{"role", []TaskSpec{{ID: "T-1", Role: events.AgentRoleReviewer}}, `role "reviewer"`},
		{"unknown dependency", []TaskSpec{{ID: "T-1", Role: impl, DependsOn: []events.TaskID{"T-9"}}}, "unknown task T-9"},
		{"cycle", []TaskSpec{{ID: "T-1", Role: impl, DependsOn: []events.TaskID{"T-2"}}, {ID: "T-2", Role: impl, DependsOn: []events.TaskID{"T-1"}}}, "T-1 is part of a dependency cycle"},
	} {
		err := ValidateTasks(tc.tasks)
		if tc.err == "" && err != nil || tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.err)
		}
	}
}

func TestValidCommit(t *testing.T) {
	if !ValidCommit(events.Commit(strings.Repeat("a", 40))) || !ValidCommit(events.Commit(strings.Repeat("0", 64))) {
		t.Error("full ids refused")
	}
	for _, c := range []string{"", "abc123", strings.Repeat("A", 40), strings.Repeat("a", 41)} {
		if ValidCommit(events.Commit(c)) {
			t.Errorf("%q accepted", c)
		}
	}
}

// The JSON names are recorded in workflow histories; this pins the ones the agent runtime added.
func TestWireNames(t *testing.T) {
	b, _ := json.Marshal(TaskRequest{Limit: Budget{Tokens: 1, Seconds: 2}})
	if !strings.Contains(string(b), `"limit":{"tokens":1,"seconds":2}`) {
		t.Errorf("TaskRequest %s", b)
	}
	b, _ = json.Marshal(TaskResult{Outcome: OutcomeDone, Summary: "s", FilesChanged: []string{"main.go"}})
	if string(b) != `{"outcome":"done","summary":"s","filesChanged":["main.go"]}` {
		t.Errorf("TaskResult %s", b)
	}
	if u := (Usage{Tokens: 1, Seconds: 2, QueueSeconds: 3}).Add(Usage{Tokens: 1, Seconds: 1, QueueSeconds: 1}); u != (Usage{2, 3, 4}) {
		t.Errorf("Add %+v", u)
	}
	b, _ = json.Marshal(Usage{Seconds: 5, QueueSeconds: 7})
	if string(b) != `{"seconds":5,"queueSeconds":7}` {
		t.Errorf("Usage %s", b)
	}
	b, _ = json.Marshal(PlanRequest{})
	if strings.Contains(string(b), "limit") {
		t.Errorf("an empty limit is sent: %s", b)
	}
}

func briefRequest() BriefRequest {
	return BriefRequest{
		Run: RunRef{RunID: "RUN-0042-1", IntentID: "INT-0042", IntentVersion: 2}, ID: "RUN-0042-1-T-2-1",
		Category: events.BriefCategoryImplementation, To: "Developer, Orders team", RaisedBy: events.AgentRoleImplementer,
		Urgency: events.UrgencyNormal, Deadline: time.Date(2026, 11, 17, 9, 30, 0, 0, time.UTC), Default: "pause",
		Blocks: []events.TaskID{"T-2"}, Question: "Which port?", Context: "The README names two.",
		Options: []BriefOption{
			{ID: "A", Name: "8080", Text: "Use 8080.", Impact: "none.", Recommended: true},
			{ID: "B", Name: "9090", Text: "Use 9090.", Impact: "the docs change."},
		},
		Recommendation: "A.",
	}
}

func TestRenderBriefAndInline(t *testing.T) {
	md, err := RenderBrief(briefRequest())
	if err != nil {
		t.Fatal(err)
	}
	rb, body, err := documents.ParseReviewBrief(md)
	if err != nil {
		t.Fatal(err)
	}
	if rb.ID != "RUN-0042-1-T-2-1" || rb.Intent != "INT-0042 v2" || rb.Deadline != "2026-11-17T09:30" || len(rb.Blocks) != 1 {
		t.Errorf("front matter %+v", rb)
	}
	if !strings.Contains(string(body), "### A: 8080 (recommended)") {
		t.Errorf("body:\n%s", body)
	}
	ref := Inline(md)
	got, err := DecodeInline(ref)
	if err != nil || string(got) != string(md) {
		t.Fatalf("round trip: %v", err)
	}
	ref.Sha256 = strings.Repeat("0", 64)
	if _, err := DecodeInline(ref); err == nil {
		t.Error("a wrong digest decoded")
	}
	if _, err := DecodeInline(events.DocumentRef{URI: "https://example.com/x"}); err == nil {
		t.Error("a non-inline reference decoded")
	}
	req := briefRequest()
	req.Default = "replan"
	if _, err := RenderBrief(req); err == nil {
		t.Error("an invalid default was rendered")
	}
}

func TestFindingString(t *testing.T) {
	f := Finding{Rule: "GO-ERR-1", RuleVersion: 1, File: "server.go", Line: 12, Severity: FindingBlocking,
		Issue: "loadMotd returns the read error as is.", Fix: "Return fmt.Errorf(\"read motd %s: %w\", path, err)."}
	if got := f.String(); got != `[blocking] GO-ERR-1 at server.go:12: loadMotd returns the read error as is. Fix: Return fmt.Errorf("read motd %s: %w", path, err).` {
		t.Errorf("String() = %s", got)
	}
	f.Line = 0
	if !strings.Contains(f.String(), " at server.go: ") {
		t.Errorf("no line: %s", f)
	}
	if Blocking([]Finding{{Severity: FindingAdvisory}}) || !Blocking([]Finding{{Severity: FindingAdvisory}, f}) {
		t.Error("Blocking")
	}
}

func TestReviewTypesRoundTrip(t *testing.T) {
	in := CheckRequest{Task: TaskSpec{ID: "T-1"}, TestReport: &TestReport{Repo: "playground", Commit: events.Commit(strings.Repeat("a", 40)),
		Criteria: []CriterionTests{{Criterion: 1, Tests: []string{"TestVersion"}}}, Checks: []TestCheck{{Name: "test", Passed: true}},
		Claimed: "pass", Passed: true}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out CheckRequest
	if err := json.Unmarshal(b, &out); err != nil || out.TestReport == nil || !out.TestReport.Passed || out.TestReport.Criteria[0].Tests[0] != "TestVersion" {
		t.Errorf("round trip %s: %+v, %v", b, out, err)
	}
	// Requests and results recorded before these fields existed still decode.
	var old ReviewResult
	if err := json.Unmarshal([]byte(`{"approved":true,"comments":["ok"]}`), &old); err != nil || !old.Approved || old.Findings != nil {
		t.Errorf("old review result: %+v, %v", old, err)
	}
}
