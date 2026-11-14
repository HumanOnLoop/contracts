package kafka

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/HumanOnLoop/contracts/events"
)

func runStarted(t *testing.T, subject string) events.Envelope {
	t.Helper()
	env, err := events.New(events.RunStarted{IntentVersion: 1, Developer: "Developer, Orders team",
		Autonomy: events.AutonomyL3, Repos: []events.Repo{"orders-service"}},
		events.Meta{Source: "/hotl/test", Subject: subject, RunID: events.RunID(subject), IntentID: "INT-0042"})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestNewRecord(t *testing.T) {
	env := runStarted(t, "RUN-0042-1")
	rec, err := NewRecord(env)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Topic != events.TopicRun || string(rec.Key) != "RUN-0042-1" {
		t.Errorf("topic %s, key %s", rec.Topic, rec.Key)
	}
	if len(rec.Headers) != 1 || rec.Headers[0].Key != "content-type" || string(rec.Headers[0].Value) != events.ContentType {
		t.Errorf("headers = %v", rec.Headers)
	}
	if err := events.ValidateJSON(rec.Value); err != nil {
		t.Errorf("record value is not a valid event: %v", err)
	}
}

func TestNewRecordRejectsInvalidEvents(t *testing.T) {
	env := runStarted(t, "RUN-0042-1")
	env.Subject = ""
	if _, err := NewRecord(env); err == nil {
		t.Error("accepted an event without a subject")
	}
}

func TestDecode(t *testing.T) {
	env := runStarted(t, "RUN-0042-1")
	value, _ := json.Marshal(env)
	msg, err := decode(&kgo.Record{Topic: "hotl.run", Partition: 2, Offset: 41, Value: value})
	if err != nil {
		t.Fatal(err)
	}
	if msg.ID != env.ID || msg.Partition != 2 || msg.Offset != 41 || msg.Topic != "hotl.run" {
		t.Errorf("message = %+v", msg)
	}

	// A type this release does not know is still a message: a newer producer may add event types.
	newer := []byte(`{"specversion":"1.0","id":"e1","source":"/s","type":"RunTeleported","time":"2026-11-14T09:00:00Z",
		"subject":"RUN-1-1","data":{}}`)
	msg, err = decode(&kgo.Record{Value: newer})
	if err != nil {
		t.Fatalf("unknown type rejected: %v", err)
	}
	if _, err := msg.Decode(); !errors.Is(err, events.ErrUnknownType) {
		t.Errorf("Decode: want ErrUnknownType, got %v", err)
	}

	for _, bad := range []string{`not json`, `{"type":"RunStarted"}`, `{"id":"x"}`} {
		if _, err := decode(&kgo.Record{Value: []byte(bad)}); err == nil {
			t.Errorf("decoded %s", bad)
		}
	}
}

func TestNewConsumerNeedsGroupAndTopics(t *testing.T) {
	if _, err := NewConsumer([]string{"localhost:1"}, "", []string{events.TopicRun}); err == nil {
		t.Error("accepted an empty group")
	}
	if _, err := NewConsumer([]string{"localhost:1"}, "g", nil); err == nil {
		t.Error("accepted no topics")
	}
}
