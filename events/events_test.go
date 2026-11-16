package events

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/HumanOnLoop/contracts"
	"github.com/HumanOnLoop/contracts/internal/schema"
)

func TestCatalogCoversEverySchema(t *testing.T) {
	if n := len(catalog); n != 46 {
		t.Errorf("catalogue has %d events: the design's 42 (ScopeProposed, ScopeConfirmed and RepoCreated since ADR 0018), RepoAttachRequested, RepoProfileApproved, ToolchainImageRequested and ToolchainImageApproved (ADR 0013)", n)
	}
	for name, s := range catalog {
		if _, err := fs.Stat(contracts.Schemas, s.schema); err != nil {
			t.Errorf("%s: schema %s: %v", name, s.schema, err)
		}
		p := s.new()
		if p.EventType() != name {
			t.Errorf("catalog[%q] builds a %s", name, p.EventType())
		}
		if !strings.HasPrefix(s.schema, "events/"+strings.TrimPrefix(s.topic, "hotl.")+"/") {
			t.Errorf("%s: topic %s does not match schema directory %s", name, s.topic, s.schema)
		}
	}
	if len(Topics) != 5 {
		t.Errorf("Topics = %v, the design has 5", Topics)
	}
}

func meta(subject string) Meta {
	return Meta{Source: "/hotl/test", Subject: subject, RunID: "RUN-0042-1", IntentID: "INT-0042",
		TraceID: "4bf92f3577b34da6a3ce929d0e0e4736"}
}

// TestExamples runs every example payload in testdata through its schema, the generated type and New, which
// re-encodes and validates it again. It proves the generated types produce valid events.
func TestExamples(t *testing.T) {
	files, err := filepath.Glob("testdata/*.json")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		seen[name] = true
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			s, ok := catalog[name]
			if !ok {
				t.Fatalf("no event %s", name)
			}
			if err := schema.Default().ValidateJSON(s.schema, raw); err != nil {
				t.Fatalf("example does not match its schema: %v", err)
			}
			p := s.new()
			if err := json.Unmarshal(raw, p); err != nil {
				t.Fatal(err)
			}
			env, err := New(p, meta("RUN-0042-1"))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			var a, b any
			_ = json.Unmarshal(raw, &a)
			_ = json.Unmarshal(env.Data, &b)
			if !jsonEqual(a, b) {
				t.Errorf("payload changed in a round trip through %T:\n got %s\nwant %s", p, env.Data, raw)
			}
			back, err := env.Decode()
			if err != nil {
				t.Fatal(err)
			}
			if back.EventType() != name {
				t.Errorf("Decode returned a %s", back.EventType())
			}
		})
	}
	var missing []string
	for name := range catalog {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no example in testdata for %v", missing)
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func TestNewFillsTheEnvelope(t *testing.T) {
	before := time.Now().Add(-time.Second)
	env, err := New(PlanApproved{PlanVersion: 2}, meta("RUN-0042-1"))
	if err != nil {
		t.Fatal(err)
	}
	if env.SpecVersion != "1.0" || env.Type != "PlanApproved" || env.DataContentType != "application/json" {
		t.Errorf("envelope = %+v", env)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(env.ID) {
		t.Errorf("id %q is not a v4 UUID", env.ID)
	}
	if env.Time.Before(before) || env.Time.Location() != time.UTC {
		t.Errorf("time = %v", env.Time)
	}
	if topic, _ := env.Topic(); topic != TopicRun {
		t.Errorf("topic = %s", topic)
	}

	fixed := meta("RUN-0042-1")
	fixed.ID, fixed.Time = "run-RUN-0042-1-plan-approved-2", time.Date(2026, 11, 14, 9, 0, 0, 0, time.FixedZone("CET", 3600))
	env, err = New(PlanApproved{PlanVersion: 2}, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if env.ID != fixed.ID || !env.Time.Equal(fixed.Time) {
		t.Errorf("id/time not kept: %+v", env)
	}
}

func TestNewRejectsInvalidEvents(t *testing.T) {
	cases := []struct {
		name string
		p    Payload
		m    Meta
		want string // path of the expected problem
	}{
		{"missing subject", PlanApproved{PlanVersion: 1}, Meta{Source: "/hotl/test"}, "/subject"},
		{"missing source", PlanApproved{PlanVersion: 1}, Meta{Subject: "RUN-1-1"}, "/source"},
		{"bad trace id", PlanApproved{PlanVersion: 1}, Meta{Source: "/s", Subject: "RUN-1-1", TraceID: "xyz"}, "/traceid"},
		{"payload below minimum", PlanApproved{PlanVersion: 0}, meta("RUN-1-1"), "/data/planVersion"},
		{"required array left nil", TaskChecked{TaskID: "T-1", Attempt: 1}, meta("RUN-1-1"), "/data/violations"},
		{"enum value not in schema", RunPaused{Reason: "lunch"}, meta("RUN-1-1"), "/data/reason"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.p, tc.m)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("want *ValidationError, got %v", err)
			}
			for _, p := range ve.Problems {
				if p.Path == tc.want {
					return
				}
			}
			t.Errorf("no problem at %s: %v", tc.want, ve)
		})
	}
}

func TestValidateJSONRejectsUnknownType(t *testing.T) {
	ev := `{"specversion":"1.0","id":"1","source":"/s","type":"RunExploded","time":"2026-11-14T09:00:00Z",
		"subject":"RUN-1-1","data":{}}`
	if err := ValidateJSON([]byte(ev)); !errors.Is(err, ErrUnknownType) {
		t.Fatalf("want ErrUnknownType, got %v", err)
	}
}

// A consumer on this release must still read events from a producer on a newer one, which may add properties
// and enum values.
func TestDecodeIsTolerant(t *testing.T) {
	env := Envelope{Type: "RunPaused", Data: json.RawMessage(`{"reason":"maintenance","pausedBy":"Dev","addedLater":42}`)}
	p, err := env.Decode()
	if err != nil {
		t.Fatal(err)
	}
	rp, ok := p.(*RunPaused)
	if !ok {
		t.Fatalf("Decode returned %T", p)
	}
	if rp.Reason != "maintenance" || rp.PausedBy == nil || *rp.PausedBy != "Dev" {
		t.Errorf("decoded %+v", rp)
	}

	// Unknown envelope attributes are ignored too.
	var e2 Envelope
	if err := json.Unmarshal([]byte(`{"type":"PlanApproved","newattr":"x","data":{"planVersion":3}}`), &e2); err != nil {
		t.Fatal(err)
	}
	pa, err := As[PlanApproved](e2)
	if err != nil || pa.PlanVersion != 3 {
		t.Errorf("As = %+v, %v", pa, err)
	}
}

func TestDecodeErrors(t *testing.T) {
	if _, err := (Envelope{Type: "Nope", Data: json.RawMessage(`{}`)}).Decode(); !errors.Is(err, ErrUnknownType) {
		t.Errorf("unknown type: %v", err)
	}
	if _, err := As[RunStarted](Envelope{Type: "RunPaused", Data: json.RawMessage(`{}`)}); err == nil {
		t.Error("As accepted a different type")
	}
	if _, err := (Envelope{Type: "RunPaused", Data: json.RawMessage(`{"reason":7}`)}).Decode(); err == nil {
		t.Error("Decode accepted a number for a string")
	}
}

// The hand-written Envelope must carry exactly the attributes of the envelope schema.
func TestEnvelopeMatchesSchema(t *testing.T) {
	raw, err := fs.ReadFile(contracts.Schemas, envelopeSchema)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	full := Envelope{SpecVersion: "1.0", ID: "i", Source: "s", Type: "t", Subject: "x", DataContentType: "d",
		DataSchema: "ds", RunID: "r", IntentID: "n", TraceID: "tr", Data: json.RawMessage(`{}`)}
	b, _ := json.Marshal(full)
	var fields map[string]any
	_ = json.Unmarshal(b, &fields)
	for k := range doc.Properties {
		if _, ok := fields[k]; !ok {
			t.Errorf("schema attribute %s has no Envelope field", k)
		}
	}
	for k := range fields {
		if _, ok := doc.Properties[k]; !ok {
			t.Errorf("Envelope field %s is not in the schema", k)
		}
	}
}
