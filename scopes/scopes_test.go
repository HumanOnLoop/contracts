package scopes

import (
	"strings"
	"testing"

	"github.com/HumanOnLoop/contracts/documents"
	"github.com/HumanOnLoop/contracts/events"
)

const contractsRule = `---
id: BND-CONTRACTS-1
title: A contracts change with three or more consumers needs an architect
scope:
  repos: ["*"]
severity: hard
owner: architects
version: 1
kind: boundary
evaluator: condition
boundary:
  repos: [contracts]
  dependents:
    of: [contracts]
    atLeast: 3
---

## Rule

Body.
`

func rule(t *testing.T, md string) documents.Rule {
	t.Helper()
	r, _, err := documents.ParseRule([]byte(md))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func scope(repos ...string) events.Scope {
	s := Normalize(events.Scope{Summary: "s"})
	for _, r := range repos {
		s.Repos = append(s.Repos, events.ScopeRepo{Repo: events.Repo(r), Reason: "r", Confidence: 0.9, Evidence: []events.ScopeEvidence{}})
	}
	return s
}

// HOTL's profiles: these services depend on contracts.
func deps(r events.Repo) []events.Repo {
	switch r {
	case "intake", "orchestrator", "agent-runtime", "cicd-adapter", "review-service", "portal":
		return []events.Repo{"contracts"}
	}
	return nil
}

func TestContractsBoundary(t *testing.T) {
	rules := []documents.Rule{rule(t, contractsRule), rule(t, strings.Replace(contractsRule, "kind: boundary\n", "", 1))}
	for _, c := range []struct {
		repos []string
		want  bool
	}{
		{[]string{"contracts", "intake", "orchestrator", "agent-runtime"}, true},
		{[]string{"contracts", "intake", "orchestrator", "playground"}, false},         // two consumers
		{[]string{"intake", "orchestrator", "agent-runtime", "review-service"}, false}, // no contracts
		{[]string{"contracts", "intake", "orchestrator", "agent-runtime", "portal"}, true},
	} {
		got := Boundaries(scope(c.repos...), rules, deps)
		if (len(got) == 1) != c.want || len(got) > 1 {
			t.Errorf("%v: crossed %v, want crossed=%v", c.repos, got, c.want)
			continue
		}
		if c.want {
			if got[0].Rule != "BND-CONTRACTS-1" || !strings.Contains(got[0].Reason, "depend on contracts") || len(got[0].Repos) < 4 {
				t.Errorf("%v: %+v", c.repos, got[0])
			}
		}
	}
}

func TestBoundaryIgnoresInactiveAndEmptyRules(t *testing.T) {
	shadow := rule(t, strings.Replace(contractsRule, "evaluator: condition\n", "evaluator: condition\nstatus: shadow\n", 1))
	empty := rule(t, strings.Replace(contractsRule, `boundary:
  repos: [contracts]
  dependents:
    of: [contracts]
    atLeast: 3
`, "boundary: {}\n", 1))
	s := scope("contracts", "intake", "orchestrator", "agent-runtime")
	if got := Boundaries(s, []documents.Rule{shadow, empty}, deps); len(got) != 0 {
		t.Errorf("crossed %v", got)
	}
}

func TestNewRepoBoundary(t *testing.T) {
	r := rule(t, strings.Replace(contractsRule, `  repos: [contracts]
  dependents:
    of: [contracts]
    atLeast: 3
`, "  newRepo: true\n", 1))
	s := scope("playground")
	if got := Boundaries(s, []documents.Rule{r}, deps); len(got) != 0 {
		t.Errorf("no new repo, crossed %v", got)
	}
	s.NewRepos = append(s.NewRepos, events.ScopeNewRepo{Name: "sandbox-x", Purpose: "p", Technology: "go", Why: "w",
		DraftProfile: events.DraftProfile{Languages: []string{"go"}}})
	if got := Boundaries(s, []documents.Rule{r}, deps); len(got) != 1 {
		t.Errorf("new repo, crossed %v", got)
	}
}

func TestNext(t *testing.T) {
	s := scope("playground")
	if next, why := Next(s, 0); next != events.ScopeProposedNextConfirm || why != nil {
		t.Errorf("high confidence: %s %v", next, why)
	}
	s.Repos[0].Confidence = 0.5
	if next, why := Next(s, 0); next != events.ScopeProposedNextDeveloper || len(why) != 1 {
		t.Errorf("low confidence: %s %v", next, why)
	}
	if next, _ := Next(scope(), 0); next != events.ScopeProposedNextDeveloper {
		t.Errorf("no repo: %s", next)
	}
	s.Boundaries = append(s.Boundaries, events.ScopeBoundary{Rule: "BND-1", Reason: "r"})
	if next, why := Next(s, 0); next != events.ScopeProposedNextArchitect || len(why) != 1 {
		t.Errorf("boundary: %s %v", next, why)
	}
}

func TestValidRepoName(t *testing.T) {
	for name, want := range map[string]bool{"sandbox-a1b2": true, "playground": true, "Sandbox": false, "-x": false,
		"a--b": false, "a_b": false, "../x": false, "": false, strings.Repeat("a", 65): false} {
		if ValidRepoName(name) != want {
			t.Errorf("ValidRepoName(%q) != %v", name, want)
		}
	}
}

func TestHints(t *testing.T) {
	ip := documents.IntentPackage{RepoHints: []string{"a", "b"}, Repos: []string{"b", "c"}}
	if got := strings.Join(ip.Hints(), ","); got != "a,b,c" {
		t.Errorf("Hints = %s", got)
	}
	if got := (documents.IntentPackage{}).Hints(); got == nil || len(got) != 0 {
		t.Errorf("no hints = %#v", got)
	}
}
