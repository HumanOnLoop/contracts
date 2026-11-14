package events

import (
	"encoding/json"
	"fmt"

	"github.com/HumanOnLoop/contracts/internal/schema"
)

// ValidationError lists every way an event fails its schema. Problems[i].Path is a JSON pointer into the
// envelope (for /data/... into the payload).
type ValidationError = schema.ValidationError

const envelopeSchema = "events/envelope.schema.json"

// Validate checks an envelope against the envelope schema and its data against the schema of its type. It returns
// a *ValidationError when either does not match, and an error wrapping ErrUnknownType for a type this release does
// not know.
func Validate(e Envelope) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return ValidateJSON(raw)
}

// ValidateJSON checks a JSON-encoded event, as published on Kafka, like Validate.
func ValidateJSON(event []byte) error {
	v := schema.Default()
	if err := v.ValidateJSON(envelopeSchema, event); err != nil {
		return err
	}
	var head struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(event, &head); err != nil {
		return err
	}
	s, ok := catalog[head.Type]
	if !ok {
		return fmt.Errorf("%w %q", ErrUnknownType, head.Type)
	}
	if err := v.ValidateJSON(s.schema, head.Data); err != nil {
		if ve, ok := err.(*schema.ValidationError); ok {
			// Report payload problems as paths into the event, like envelope problems.
			out := &schema.ValidationError{Schema: ve.Schema, Problems: make([]schema.Problem, len(ve.Problems))}
			for i, p := range ve.Problems {
				out.Problems[i] = schema.Problem{Path: "/data" + p.Path, Message: p.Message}
			}
			return out
		}
		return err
	}
	return nil
}
