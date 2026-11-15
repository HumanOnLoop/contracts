package agents

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/HumanOnLoop/contracts/events"
)

// RejectedBrief is a brief the review service rejected on the agent's last try, with why (BriefRejected), after
// the agent runtime had retried the attempt with the reasons (ADR 0010).
type RejectedBrief struct {
	// ID is the request id of the last brief raised: the BriefRaised and BriefRejected subject.
	ID events.BriefID `json:"id"`
	// Markdown is the last brief as raised, cut to MaxRejectedMarkdown bytes.
	Markdown string `json:"markdown"`
	// Reasons are the BriefRejected reasons for it.
	Reasons []events.Problem `json:"reasons"`
	// Tries is how many briefs were raised and rejected, the last included.
	Tries int `json:"tries"`
}

// MaxRejectedMarkdown is the most of a rejected brief a RejectedBrief carries.
const MaxRejectedMarkdown = 16 << 10

// Options of the brief BriefOnRejected writes.
const (
	// RejectedOptionAnswer runs the task again with the answer as a decision.
	RejectedOptionAnswer = "A"
	// RejectedOptionCancel cancels the task.
	RejectedOptionCancel = "B"
)

// RejectedOn is what BriefOnRejected needs.
type RejectedOn struct {
	Run RunRef
	// ID is the id of the brief to raise.
	ID events.BriefID
	// To is who answers: the run's developer.
	To       string
	RaisedBy events.AgentRole
	Deadline time.Time
	Task     events.TaskID
	Repo     events.Repo
	Rejected RejectedBrief
}

// BriefOnRejected writes the budget brief the orchestrator raises when an agent's brief was rejected on every try:
// answer the agent's question here (A), or cancel the task (B). The rejected brief and the reasons are quoted in
// fenced code blocks that nothing in them can close, so the brief passes the review service's checks whatever the
// agent wrote; the review service tests exactly that.
func BriefOnRejected(in RejectedOn) BriefRequest {
	r := in.Rejected
	reasons := make([]string, 0, len(r.Reasons))
	for i, p := range r.Reasons {
		if i == 50 {
			reasons = append(reasons, fmt.Sprintf("… and %d more", len(r.Reasons)-i))
			break
		}
		reasons = append(reasons, "- "+oneLine(p.Part)+": "+oneLine(p.Message))
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "(the review service gave no reason)")
	}
	md := r.Markdown
	if len(md) > MaxRejectedMarkdown {
		md = truncateUTF8(md, MaxRejectedMarkdown) + "\n… (cut)"
	}
	tries := max(r.Tries, 1)
	var ctx strings.Builder
	fmt.Fprintf(&ctx, "Task %s in %s is blocked on a decision its %s needs. The review service rejected each of the %d "+
		"briefs the %s raised for it, the last time even after being told why, so nobody was asked. The task and "+
		"every task that depends on it wait for this decision.\n\n", in.Task, in.Repo, in.RaisedBy, tries, in.RaisedBy)
	ctx.WriteString("What the review service said about the last brief:\n\n")
	ctx.WriteString(fenced("text", strings.Join(reasons, "\n")))
	fmt.Fprintf(&ctx, "\n\nThe last brief (%s) as it was raised. Its question is the decision the task needs:\n\n", oneLine(string(r.ID)))
	ctx.WriteString(fenced("markdown", md))
	return BriefRequest{
		Run: in.Run, ID: in.ID, Category: events.BriefCategoryBudget, To: in.To, RaisedBy: in.RaisedBy,
		Urgency: events.UrgencyNormal, Deadline: in.Deadline.UTC().Truncate(time.Minute), Default: "pause",
		Blocks:   []events.TaskID{in.Task},
		Question: fmt.Sprintf("The %s of task %s could not raise a brief that passes the review service's checks in %d tries, so will you answer its question here or cancel the task?", in.RaisedBy, in.Task, tries),
		Context:  ctx.String(),
		Options: []BriefOption{
			{ID: RejectedOptionAnswer, Name: "Answer the question here", Recommended: true,
				Text:   "Write your answer to the agent's question in this decision's text. The task gets another attempt, with your answer as a decision.",
				Impact: "More of the task's budget is spent; the next attempt follows your answer."},
			{ID: RejectedOptionCancel, Name: "Cancel the task",
				Text:   "Drop the task and the tasks that depend on it.",
				Impact: "The run delivers without them, or raises a scope brief when nothing is left to deliver."},
		},
		Recommendation: "A: the rejected brief still asks the decision the task needs, and the reasons say only how it was " +
			"written. No precedent applies: precedents are not searched yet.",
	}
}

// fenced puts text in a fenced code block longer than any run of backticks in it, so no line of it closes the block.
func fenced(info, text string) string {
	longest, run := 0, 0
	for _, c := range text {
		if c == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", max(3, longest+1))
	return fence + info + "\n" + strings.TrimRight(text, "\n") + "\n" + fence
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncateUTF8(s string, n int) string {
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
