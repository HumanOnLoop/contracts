package runs

import (
	"encoding/json"
	"testing"

	"github.com/HumanOnLoop/contracts/events"
)

// The JSON is recorded in workflow histories: it must not change.
func TestDecisionSignalJSON(t *testing.T) {
	opt, task := "A", events.TaskID("T-1")
	b, err := json.Marshal(DecisionSignal{BriefID: "RUN-1-T-1-1", EventID: "e1",
		Decision: events.DecisionRecorded{Option: &opt, TaskID: &task}})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["briefId"] != "RUN-1-T-1-1" || m["eventId"] != "e1" || m["decision"].(map[string]any)["taskId"] != "T-1" {
		t.Errorf("json %s", b)
	}
	b, _ = json.Marshal(AbortSignal{By: "Oskar", Reason: "r"})
	if string(b) != `{"by":"Oskar","reason":"r"}` {
		t.Errorf("abort json %s", b)
	}
}
