package agents

import (
	"fmt"
	"strings"

	"github.com/HumanOnLoop/contracts/events"
)

// TestReport is the tester's report on a task (ADR 0007): which tests cover each acceptance criterion of the
// intent, and the verified run. The verified run is not the tester's claim: the agent runtime runs gofmt -l,
// go vet ./... and go test ./... itself on a clean checkout of the commit the tester pushed, and Passed holds only
// when every check passed, every criterion is covered by a test that ran and passed, and the tester's own verdict
// agrees with the run.
type TestReport struct {
	Repo   events.Repo   `json:"repo"`
	Branch events.Branch `json:"branch"`
	// Commit is the commit the verified run checked: the run branch's head after the tester's push.
	Commit   events.Commit    `json:"commit"`
	Criteria []CriterionTests `json:"criteria"`
	// Checks are the verified run's checks, in the order they ran: gofmt, vet, test.
	Checks []TestCheck `json:"checks"`
	// Claimed is the tester's own verdict: pass or fail.
	Claimed string `json:"claimed"`
	// Passed is the verdict that counts.
	Passed bool `json:"passed"`
	// Problems says why Passed is false.
	Problems []string `json:"problems,omitempty"`
}

// CriterionTests maps one acceptance criterion of the intent package to the tests that cover it.
type CriterionTests struct {
	// Criterion numbers the acceptance criteria in the order the intent package lists them, from 1.
	Criterion int    `json:"criterion"`
	Text      string `json:"text,omitempty"`
	// Tests are Go test names, such as TestVersion or TestVersion/POST, optionally prefixed with their package.
	Tests []string `json:"tests"`
}

// TestCheck is one check of the verified run.
type TestCheck struct {
	// Name is gofmt, vet or test.
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	// Output is the end of what the check printed, for a failed check.
	Output string `json:"output,omitempty"`
}

// Severity of a review finding.
const (
	// FindingBlocking: the change cannot be approved until the finding is fixed.
	FindingBlocking = "blocking"
	// FindingAdvisory: worth fixing, but it does not hold the change back.
	FindingAdvisory = "advisory"
)

// RuleGeneral is the rule of a finding that no rule in scope covers.
const RuleGeneral = "general"

// Finding is one problem the reviewer found in a change (ADR 0007).
type Finding struct {
	// Rule is the id of the rule the change breaks, or "general".
	Rule string `json:"rule"`
	// RuleVersion is the version of that rule; zero for general findings.
	RuleVersion int `json:"ruleVersion,omitempty"`
	// File is relative to the repo root; Line counts from 1, zero for the file as a whole.
	File string `json:"file"`
	Line int    `json:"line,omitempty"`
	// Severity is blocking or advisory. A finding on a rule takes the rule's severity: hard is blocking, soft is
	// advisory.
	Severity string `json:"severity"`
	// Issue says what is wrong; Fix says concretely what to change.
	Issue string `json:"issue"`
	Fix   string `json:"fix"`
}

// String renders the finding on one line, the way it is handed to the implementer.
func (f Finding) String() string {
	where := f.File
	if f.Line > 0 {
		where = fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	return fmt.Sprintf("[%s] %s at %s: %s Fix: %s", f.Severity, f.Rule, where, strings.TrimSpace(f.Issue), strings.TrimSpace(f.Fix))
}

// Blocking reports whether any finding is blocking.
func Blocking(findings []Finding) bool {
	for _, f := range findings {
		if f.Severity == FindingBlocking {
			return true
		}
	}
	return false
}
