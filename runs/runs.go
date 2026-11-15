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
