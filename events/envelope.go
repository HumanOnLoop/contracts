// Package events holds the HOTL event envelope and one Go type per event in the catalogue, generated from the
// schemas in this directory (events/<topic>/<Event>.schema.json).
//
// Build an event with New, which fills in the envelope and validates it:
//
//	env, err := events.New(events.RunStarted{IntentVersion: 1, Developer: "Developer, Orders team",
//		Autonomy: events.AutonomyL3, Repos: []events.Repo{"orders-service"}},
//		events.Meta{Source: "/hotl/orchestrator", Subject: "RUN-0042-1", RunID: "RUN-0042-1", IntentID: "INT-0042"})
//
// Read one with Decode, or As when the type is known:
//
//	p, err := env.Decode()
//	switch p := p.(type) {
//	case *events.RunStarted: ...
//	}
//
// Producers validate; consumers are tolerant readers. Decoding ignores unknown properties and accepts enum values
// it does not know, so a consumer keeps working when a producer on a newer release adds to a schema.
package events

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SpecVersion is the CloudEvents version of the envelope.
const SpecVersion = "1.0"

// ContentType is the CloudEvents structured-mode content type HOTL events are published with.
const ContentType = "application/cloudevents+json; charset=UTF-8"

// Envelope is a HOTL event: a CloudEvents 1.0 event in structured JSON mode with the HOTL extension attributes
// runid, intentid and traceid. Its schema is events/envelope.schema.json.
type Envelope struct {
	SpecVersion     string    `json:"specversion"`
	ID              string    `json:"id"`
	Source          string    `json:"source"`
	Type            string    `json:"type"`
	Time            time.Time `json:"time"`
	Subject         string    `json:"subject"`
	DataContentType string    `json:"datacontenttype,omitempty"`
	DataSchema      string    `json:"dataschema,omitempty"`
	// RunID is the run the event belongs to, when it belongs to one.
	RunID RunID `json:"runid,omitempty"`
	// IntentID is the intent the event belongs to, when it belongs to one.
	IntentID IntentID `json:"intentid,omitempty"`
	// TraceID is the OpenTelemetry trace id of the run: 32 lowercase hex digits.
	TraceID string `json:"traceid,omitempty"`
	// Data is the payload, as JSON.
	Data json.RawMessage `json:"data"`
}

// Payload is the data of an event: one of the generated event types, such as RunStarted.
type Payload interface {
	// EventType returns the event's name in the catalogue, which is also the envelope's type.
	EventType() string
	// Topic returns the Kafka topic the event is published on.
	Topic() string
}

// Meta is what New needs besides the payload.
type Meta struct {
	// Source names the producing component, for example /hotl/orchestrator. Required.
	Source string
	// Subject is the id of the aggregate the event is about: the run, intent, brief or rule id, or the repo for
	// hotl.knowledge events. It is also the Kafka key, so all events about one thing stay in order. Required.
	Subject string
	// RunID, IntentID and TraceID correlate the event with its run, intent and trace; set them when they apply.
	RunID    RunID
	IntentID IntentID
	TraceID  string
	// ID is the event id consumers dedupe on. Leave it empty for a random UUID; code in a Temporal workflow must
	// set a deterministic one, so a replay never emits a duplicate.
	ID string
	// Time is when the fact happened; zero means now.
	Time time.Time
}

type spec struct {
	topic  string
	schema string
	new    func() Payload
}

// ErrUnknownType reports an event type that is not in this release's catalogue.
var ErrUnknownType = errors.New("unknown event type")

// New builds the envelope for p and validates it against the envelope schema and p's schema.
func New(p Payload, m Meta) (Envelope, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return Envelope{}, fmt.Errorf("%s: %w", p.EventType(), err)
	}
	id := m.ID
	if id == "" {
		id = newUUID()
	}
	t := m.Time
	if t.IsZero() {
		t = time.Now()
	}
	env := Envelope{
		SpecVersion:     SpecVersion,
		ID:              id,
		Source:          m.Source,
		Type:            p.EventType(),
		Time:            t.UTC(),
		Subject:         m.Subject,
		DataContentType: "application/json",
		RunID:           m.RunID,
		IntentID:        m.IntentID,
		TraceID:         m.TraceID,
		Data:            data,
	}
	if err := Validate(env); err != nil {
		return Envelope{}, err
	}
	return env, nil
}

// Topic returns the topic events of this envelope's type are published on.
func (e Envelope) Topic() (string, error) {
	s, ok := catalog[e.Type]
	if !ok {
		return "", fmt.Errorf("%w %q", ErrUnknownType, e.Type)
	}
	return s.topic, nil
}

// Decode returns the payload as a pointer to its generated type, for example *RunStarted. It does not validate:
// unknown properties are ignored, so events from producers on a newer release still decode. It returns an error
// wrapping ErrUnknownType for a type this release does not know.
func (e Envelope) Decode() (Payload, error) {
	s, ok := catalog[e.Type]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownType, e.Type)
	}
	p := s.new()
	if err := json.Unmarshal(e.Data, p); err != nil {
		return nil, fmt.Errorf("decode %s data: %w", e.Type, err)
	}
	return p, nil
}

// As decodes the payload into T, which must match the envelope's type.
func As[T Payload](e Envelope) (T, error) {
	var p T
	if e.Type != p.EventType() {
		return p, fmt.Errorf("event %s is a %s, not a %s", e.ID, e.Type, p.EventType())
	}
	if err := json.Unmarshal(e.Data, &p); err != nil {
		return p, fmt.Errorf("decode %s data: %w", e.Type, err)
	}
	return p, nil
}

// Types lists every event type in this release's catalogue.
func Types() []string {
	out := make([]string, 0, len(catalog))
	for t := range catalog {
		out = append(out, t)
	}
	return out
}

// TopicOf returns the topic an event type is published on.
func TopicOf(eventType string) (string, bool) {
	s, ok := catalog[eventType]
	return s.topic, ok
}

// newUUID returns a random (version 4) UUID.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // never fails; see crypto/rand.Read
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
