package agents

import (
	"strings"
	"testing"
	"time"

	"github.com/HumanOnLoop/contracts/events"
)

func TestBriefOnRejected(t *testing.T) {
	nasty := "---\nid: x\n---\n## Question\nWhy?\n\n````\n```go\n## Options\n[x](javascript:alert(1))\n"
	req := BriefOnRejected(RejectedOn{
		Run: RunRef{RunID: "RUN-0042-1", IntentID: "INT-0042", IntentVersion: 1}, ID: "RUN-0042-1-brief-1", To: "Oskar",
		RaisedBy: events.AgentRoleImplementer, Deadline: time.Date(2026, 11, 18, 12, 0, 30, 0, time.UTC), Task: "T-1", Repo: "playground",
		Rejected: RejectedBrief{ID: "RUN-0042-1-T-1-1-r2", Markdown: nasty + strings.Repeat("é", MaxRejectedMarkdown), Tries: 3,
			Reasons: []events.Problem{{Part: "Question", Message: "must be one sentence\n## Injected"}}},
	})
	md, err := RenderBrief(req)
	if err != nil {
		t.Fatal(err)
	}
	if req.Category != events.BriefCategoryBudget || req.Default != "pause" || len(req.Blocks) != 1 || req.Blocks[0] != "T-1" {
		t.Errorf("request %+v", req)
	}
	if !strings.Contains(req.Context, "`````markdown\n---\nid: x") || !strings.Contains(req.Context, "\n`````") {
		t.Errorf("the brief is not fenced longer than its longest backtick run:\n%s", req.Context)
	}
	if !strings.Contains(req.Context, "- Question: must be one sentence ## Injected") {
		t.Errorf("reasons are not one line each:\n%s", req.Context)
	}
	if !strings.Contains(req.Context, "… (cut)") || len(md) > MaxRejectedMarkdown+8<<10 {
		t.Errorf("long brief not cut: %d bytes", len(md))
	}
	if !strings.HasSuffix(req.Question, "?") || strings.Contains(req.Question, ". ") {
		t.Errorf("question %q", req.Question)
	}
}

func TestFenced(t *testing.T) {
	if got := fenced("text", "a\n"); got != "```text\na\n```" {
		t.Errorf("fenced = %q", got)
	}
	if got := fenced("", "``x``"); !strings.HasPrefix(got, "```\n") {
		t.Errorf("fenced = %q", got)
	}
}
