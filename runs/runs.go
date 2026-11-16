// Package runs holds the signals other services send to a run's workflow (Temporal workflow id = run id): their
// names and payloads. The run workflow lives in the orchestrator; the review service signals it with decisions.
//
// The names and JSON field names are recorded in workflow histories, so they change only compatibly, as the
// activity types in package agents do: new optional fields, never a rename or a removal.
package runs

import "github.com/HumanOnLoop/contracts/events"

// Signal names.
const (
	// SignalDecision delivers a DecisionSignal.
	SignalDecision = "DecisionRecorded"
	// SignalAbort delivers an AbortSignal.
	SignalAbort = "Abort"
)

// DecisionSignal delivers a recorded decision (DecisionRecorded) to the run.
//
// Before v0.6.0 the orchestrator defined it, with a top-level taskId it still reads in histories of runs that were
// signalled then. Senders set Decision.TaskID instead.
type DecisionSignal struct {
	// BriefID is the id the run knows the brief by: the BriefRaised subject. For a budget brief the review service
	// raised itself it is the review service's id, and the run matches the decision by Decision.TaskID.
	BriefID events.BriefID `json:"briefId"`
	// Decision is the DecisionRecorded event's payload.
	Decision events.DecisionRecorded `json:"decision"`
	// EventID is the DecisionRecorded event's id, when there is one.
	EventID string `json:"eventId,omitempty"`
}

// AbortSignal abandons a run. Changes already merged stay in place.
type AbortSignal struct {
	By     events.Person `json:"by"`
	Reason string        `json:"reason"`
}

// QueryScope is the run workflow's query for its scope (since v0.12.0, ADR 0018). The agent runtime asks it before
// it mints a GitHub token for one of the run's Jobs, and refuses a repo outside a confirmed scope.
const QueryScope = "scope"

// ScopeStatus answers QueryScope.
type ScopeStatus struct {
	// Version is the scope version proposed last; 0 before the first proposal.
	Version events.ScopeVersion `json:"version"`
	// Confirmed is set once a scope is confirmed; from then on Repos is the confirmed scope.
	Confirmed bool `json:"confirmed"`
	// Repos are the repos the run may change: the confirmed scope, or the last proposal's repos before one is
	// confirmed.
	Repos []events.Repo `json:"repos"`
	// Legacy is set for a run that started before scoping: it has no scope, and its repos are the intent's.
	Legacy bool `json:"legacy,omitempty"`
}

// Allows reports whether a Job may get a token for repos: every one is in a confirmed scope, or the run is legacy.
func (s ScopeStatus) Allows(repos []events.Repo) bool {
	if s.Legacy {
		return true
	}
	if !s.Confirmed {
		return false
	}
	for _, r := range repos {
		found := false
		for _, o := range s.Repos {
			if o == r {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
