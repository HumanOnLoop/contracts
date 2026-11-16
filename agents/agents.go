// Package agents holds the Temporal activity contract between the orchestrator and the services that do a run's
// work: the agent runtime (planner, implementer, tester, reviewer, integrator), the rule engine and the CI/CD
// adapter. The orchestrator's run workflow schedules activities by the names below, and whichever worker polls the
// activity's task queue implements them with these request and result types.
//
// Everything here crosses the Temporal boundary as JSON and is recorded in workflow histories, so the types change
// only compatibly, as the event schemas do: new optional fields, never a rename or a removal. Requests carry the
// attempt number and every other input that decides the result, so an implementation can be idempotent: called
// twice with the same request, it does the work once.
package agents

import (
	"time"

	"github.com/HumanOnLoop/contracts/documents"
	"github.com/HumanOnLoop/contracts/events"
)

// TaskQueue is the task queue the agent runtime polls for the agent activities it implements.
const TaskQueue = "hotl-agents"

// TaskQueueCICD is the task queue the CI/CD adapter polls for the CI/CD activities it implements.
const TaskQueueCICD = "hotl-cicd"

// Activity names. They are recorded in every run's history: never rename one.
const (
	ActivityPlan            = "Plan"
	ActivityImplement       = "Implement"
	ActivityTest            = "Test"
	ActivityReview          = "Review"
	ActivityCheckRules      = "CheckRules"
	ActivityPlanMerge       = "PlanMerge"
	ActivityFixMain         = "FixMain"
	ActivityUpdateBranch    = "UpdateBranch"
	ActivityReport          = "Report"
	ActivityPushBranch      = "PushBranch"
	ActivityMerge           = "Merge"
	ActivityPublishArtifact = "PublishArtifact"
	ActivityWatchMain       = "WatchMain"
	ActivityRevert          = "Revert"
	// ActivityOnboard asks the onboarder for a repo's profile (since v0.8.0, ADR 0013). An attachment's workflow
	// schedules it, not a run's.
	ActivityOnboard = "Onboard"
	// ActivitySmokeImage smoke-tests a toolchain image in a Job without credentials (since v0.9.0, ADR 0013).
	ActivitySmokeImage = "SmokeImage"
	// ActivityScope asks the scoper which repos a run touches (since v0.12.0, ADR 0018). The agent runtime
	// implements it.
	ActivityScope = "Scope"
	// ActivityCreateRepo creates a repo an architect approved for a run's scope (since v0.12.0, ADR 0018). The CI/CD
	// adapter implements it.
	ActivityCreateRepo = "CreateRepo"
)

// RunRef identifies the run an activity works for. Every request carries one.
type RunRef struct {
	RunID         events.RunID         `json:"runId"`
	IntentID      events.IntentID      `json:"intentId"`
	IntentVersion events.IntentVersion `json:"intentVersion"`
	// Package is the signed intent package this version of the run works against.
	Package events.DocumentRef `json:"package"`
	Title   string             `json:"title,omitempty"`
	// TraceID and SpanID place activity spans in the run's trace: TraceID is the trace the intent was signed in,
	// SpanID the span that started the run.
	TraceID string `json:"traceId,omitempty"`
	SpanID  string `json:"spanId,omitempty"`
	// Scenario is the stub scenario the intent's title names. The orchestrator sets it only while every agent and
	// CI/CD role is one of its stubs; real implementations never see one.
	Scenario string `json:"scenario,omitempty"`
}

// Usage is what an agent step consumed.
type Usage struct {
	Tokens  int `json:"tokens,omitempty"`
	Seconds int `json:"seconds,omitempty"`
	// QueueSeconds is how long the step waited before its work started (for the agent runtime: from when the
	// step was scheduled until its Job started). It is reported apart from Seconds and counts against no budget.
	QueueSeconds int `json:"queueSeconds,omitempty"`
}

// Add returns u plus v.
func (u Usage) Add(v Usage) Usage {
	return Usage{Tokens: u.Tokens + v.Tokens, Seconds: u.Seconds + v.Seconds, QueueSeconds: u.QueueSeconds + v.QueueSeconds}
}

// Budget limits tokens and agent time (seconds). Zero means "use the default".
type Budget struct {
	Tokens  int `json:"tokens,omitempty"`
	Seconds int `json:"seconds,omitempty"`
}

// TaskSpec is one task in a plan.
type TaskSpec struct {
	ID    events.TaskID `json:"id"`
	Title string        `json:"title"`
	// Role is the agent that does the task: implementer or tester.
	Role      events.AgentRole `json:"role"`
	Repo      events.Repo      `json:"repo,omitempty"`
	DependsOn []events.TaskID  `json:"dependsOn,omitempty"`
	// Budget overrides the run's default task budget, field by field.
	Budget Budget `json:"budget,omitzero"`
	// Criteria are the acceptance criteria, by number from 1, that the task covers: the tester verifies those, in
	// the task's repo (since v0.14.0, ADR 0022). An empty list covers none, such as a task that only writes an ADR,
	// and the task is not tested. Nil, in plans from before, covers every criterion.
	Criteria *[]int `json:"criteria,omitempty"`
	// Model and Effort optionally choose the model and reasoning effort for this task (since v0.15.0, ADR 0023).
	// When empty, the agent runtime uses its configured defaults.
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

// Reasoning effort levels for TaskSpec.Effort (ADR 0023).
const (
	EffortLow    = "low"
	EffortMedium = "medium"
	EffortHigh   = "high"
	EffortXHigh  = "xhigh"
	EffortMax    = "max"
)

// ValidEffort reports whether e is a known reasoning effort level.
func ValidEffort(e string) bool {
	switch e {
	case EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return true
	default:
		return false
	}
}

// Covered returns the criteria the task covers, out of n: its own, or all n when it names none.
func (t TaskSpec) Covered(n int) []int {
	if t.Criteria == nil {
		all := make([]int, n)
		for i := range all {
			all[i] = i + 1
		}
		return all
	}
	return *t.Criteria
}

// Tested reports whether the tester checks the task: it covers at least one criterion, or names none (plans from
// before v0.14.0).
func (t TaskSpec) Tested() bool { return t.Criteria == nil || len(*t.Criteria) > 0 }

// Plan is a versioned plan: the planner's document plus the tasks the workflow schedules.
type Plan struct {
	Version  events.PlanVersion `json:"version"`
	Document events.DocumentRef `json:"document"`
	Tasks    []TaskSpec         `json:"tasks"`
}

// TaskState is the state of a task, as the design's task state machine names it.
type TaskState string

const (
	TaskPending   TaskState = "Pending"
	TaskRunning   TaskState = "Running"
	TaskChecking  TaskState = "Checking"
	TaskBlocked   TaskState = "Blocked"
	TaskDone      TaskState = "Done"
	TaskFailed    TaskState = "Failed"
	TaskCancelled TaskState = "Cancelled"
)

// Terminal reports whether no further transition leaves s.
func (s TaskState) Terminal() bool { return s == TaskDone || s == TaskCancelled }

// TaskStatus is a task's progress, as the planner sees it on a re-plan.
type TaskStatus struct {
	ID     events.TaskID `json:"id"`
	State  TaskState     `json:"state"`
	Branch events.Branch `json:"branch,omitempty"`
	Commit events.Commit `json:"commit,omitempty"`
	Merged bool          `json:"merged,omitempty"`
}

// BriefRef is a review brief an agent raised (through the agent runtime) and the workflow now waits on.
type BriefRef struct {
	ID       events.BriefID       `json:"id"`
	Category events.BriefCategory `json:"category,omitempty"`
	// Summary is the brief's question, for queries and logs.
	Summary string `json:"summary,omitempty"`
}

// Decision is a recorded decision on a brief, handed to the agent that continues the work.
type Decision struct {
	BriefID   events.BriefID `json:"briefId,omitempty"`
	Option    string         `json:"option,omitempty"`
	Text      string         `json:"text,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	DecidedBy events.Person  `json:"decidedBy,omitempty"`
}

// PlanRequest asks for a plan, or a re-plan when Previous is set.
type PlanRequest struct {
	Run      RunRef          `json:"run"`
	Repos    []events.Repo   `json:"repos"`
	Autonomy events.Autonomy `json:"autonomy"`
	// Previous and Progress are set on a re-plan: the plan so far and where each of its tasks stands.
	Previous *Plan        `json:"previous,omitempty"`
	Progress []TaskStatus `json:"progress,omitempty"`
	// Feedback is why a re-plan was asked for: plan review comments, steering, a new intent version.
	Feedback []string `json:"feedback,omitempty"`
	// Limit is what is left of the run's budget when the step starts. Zero fields mean no limit was sent (runs
	// started before the field existed); the step's activity deadline still bounds its time.
	Limit Budget `json:"limit,omitzero"`
}

// PlanResult is a complete plan. On a re-plan, tasks keep their state when their id is in both plans and they are
// not in Redo; tasks of the previous plan that are missing are cancelled.
type PlanResult struct {
	Document events.DocumentRef `json:"document"`
	Tasks    []TaskSpec         `json:"tasks"`
	// Redo lists kept task ids whose done work no longer fits; they run again.
	Redo []events.TaskID `json:"redo,omitempty"`
	// Scope is set when the change is too large to carry on (most of the plan discarded, or merged work no longer
	// fits): a scope brief recommending to abandon the run.
	Scope *BriefRef `json:"scope,omitempty"`
	Usage Usage     `json:"usage,omitzero"`
	// OutOfScope is set when the planner found the intent needs a repo outside the run's confirmed scope (since
	// v0.12.0, ADR 0018). Tasks is then empty: the run raises a scope brief, and an approved change re-runs Scoping.
	OutOfScope *ScopeChange `json:"outOfScope,omitempty"`
}

// ScopeChange is an agent's request to widen the run's confirmed scope: the repos it needs and why.
type ScopeChange struct {
	Repos  []events.Repo `json:"repos"`
	Reason string        `json:"reason"`
}

// Outcome is how an agent's attempt ended.
type Outcome string

const (
	// OutcomeDone: the agent produced a result to check.
	OutcomeDone Outcome = "done"
	// OutcomeBlocked: the agent raised a brief and cannot go on without a decision.
	OutcomeBlocked Outcome = "blocked"
	// OutcomeFailed: the agent gave up on this attempt.
	OutcomeFailed Outcome = "failed"
)

// TaskRequest asks an implementer or tester for one attempt at a task.
type TaskRequest struct {
	Run     RunRef         `json:"run"`
	Task    TaskSpec       `json:"task"`
	Attempt events.Attempt `json:"attempt"`
	// Branch overrides the default run branch (e.g. when checking a FixMain branch before merging).
	Branch events.Branch `json:"branch,omitempty"`
	// Feedback holds what the checks said about the previous attempt.
	Feedback []string `json:"feedback,omitempty"`
	// Decisions holds the decisions on briefs this task raised, oldest first.
	Decisions []Decision `json:"decisions,omitempty"`
	// Steering holds the developer's steering notes for the run, oldest first.
	Steering []string `json:"steering,omitempty"`
	// Limit is what is left of the task's and the run's budget when the attempt starts, whichever is less. Zero
	// fields mean no limit was sent; the step's activity deadline still bounds its time.
	Limit Budget `json:"limit,omitzero"`
}

// TaskResult is an attempt's result.
type TaskResult struct {
	Outcome Outcome       `json:"outcome"`
	Branch  events.Branch `json:"branch,omitempty"`
	Commit  events.Commit `json:"commit,omitempty"`
	// Brief is set when Outcome is blocked.
	Brief *BriefRef `json:"brief,omitempty"`
	// Reason is set when Outcome is failed.
	Reason string `json:"reason,omitempty"`
	Usage  Usage  `json:"usage,omitzero"`
	// Summary says what the attempt changed, for checks and the feature result.
	Summary string `json:"summary,omitempty"`
	// FilesChanged lists the paths the attempt's commits changed, relative to the repo root.
	FilesChanged []string `json:"filesChanged,omitempty"`
	// TestReport is the tester's report, with the verified run, when the tester did the attempt (ADR 0007).
	TestReport *TestReport `json:"testReport,omitempty"`
	// Rejected is set when the agent needed a decision but the review service rejected every brief it raised for
	// it (ADR 0010). Outcome is then blocked and Brief is nil: the orchestrator raises a brief on it itself
	// (BriefOnRejected). An orchestrator from before v0.6.0 reads it as a failed attempt.
	Rejected *RejectedBrief `json:"rejected,omitempty"`
	// OutOfScope is set when the implementer found the task needs a repo outside the run's confirmed scope (since
	// v0.12.0, ADR 0018). Outcome is then blocked and Brief nil: the run raises the scope brief itself.
	OutOfScope *ScopeChange `json:"outOfScope,omitempty"`
}

// CheckRequest asks the rule engine or the reviewer agent to evaluate an attempt's result.
type CheckRequest struct {
	Run     RunRef         `json:"run"`
	Task    TaskSpec       `json:"task"`
	Attempt events.Attempt `json:"attempt"`
	Branch  events.Branch  `json:"branch,omitempty"`
	Commit  events.Commit  `json:"commit,omitempty"`
	// TestReport is the tester's report on this commit, when a tester ran (ADR 0007). The reviewer cannot approve
	// when its verified run failed.
	TestReport *TestReport `json:"testReport,omitempty"`
	// Limit is what is left of the task's and the run's budget, whichever is less; zero fields mean no limit was
	// sent.
	Limit Budget `json:"limit,omitzero"`
}

// RuleCheckResult is the rule engine's verdict. Hard violations fail the check; soft ones only warn.
type RuleCheckResult struct {
	Violations []events.TaskCheckedViolationsElem `json:"violations"`
	Report     *events.DocumentRef                `json:"report,omitempty"`
	Usage      Usage                              `json:"usage,omitzero"`
}

// ReviewResult is the reviewer agent's verdict: approved, or changes requested with the findings. Comments are the
// findings rendered one per line (Finding.String), for the implementer's next attempt.
type ReviewResult struct {
	Approved bool                `json:"approved"`
	Comments []string            `json:"comments,omitempty"`
	Findings []Finding           `json:"findings,omitempty"`
	Report   *events.DocumentRef `json:"report,omitempty"`
	Usage    Usage               `json:"usage,omitzero"`
	// Approvals are the approval rules the change meets, with the files each needs approved (since v0.13.0, ADR
	// 0021). HOTL's code finds them from the rules and the changed files, not the reviewer agent. Approved says
	// nothing about them: the run asks each rule's owner before it pushes the change.
	Approvals []Approval `json:"approvals,omitempty"`
}

// MaxApprovalContent bounds the text of one file an approval carries; longer files are cut and marked Truncated.
const MaxApprovalContent = 24 << 10

// Approval is an approval rule a change meets: the files in the rule's scope that the change touches, which the
// rule's owner must approve before the change is pushed (ADR 0021).
type Approval struct {
	Rule        string         `json:"rule"`
	RuleVersion int            `json:"ruleVersion"`
	Title       string         `json:"title"`
	Owner       string         `json:"owner"`
	Files       []ApprovalFile `json:"files"`
}

// ApprovalFile is one file to approve, as it is at the reviewed commit.
type ApprovalFile struct {
	Path string `json:"path"`
	// Sha256 is the hex SHA-256 of the file's whole content at the commit, empty when the change removes the file.
	// An approval holds for exactly this content: a later change to the file needs approving again.
	Sha256 string `json:"sha256,omitempty"`
	// Removed is set when the change deletes the file.
	Removed bool `json:"removed,omitempty"`
	// Content is the file's text, at most MaxApprovalContent bytes; Truncated says it was cut.
	Content   string `json:"content,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// PushRequest asks the CI/CD adapter to push a task's checked result to the run's feature branch.
type PushRequest struct {
	Run    RunRef        `json:"run"`
	TaskID events.TaskID `json:"taskId"`
	Repo   events.Repo   `json:"repo,omitempty"`
	Branch events.Branch `json:"branch,omitempty"`
	Commit events.Commit `json:"commit,omitempty"`
}

// PushResult is where the branch now points.
type PushResult struct {
	Branch events.Branch `json:"branch,omitempty"`
	Commit events.Commit `json:"commit,omitempty"`
	// NoChanges is set when the task's work adds nothing to the default branch: its commit is already on the
	// default branch, and its branch is missing or holds nothing more (since v0.7.0). Branch is then empty and
	// nothing is merged for the task; an orchestrator from before v0.7.0 ignores it.
	NoChanges bool `json:"noChanges,omitempty"`
}

// TaskBranch is a done task's pushed work, as input to the merge plan.
type TaskBranch struct {
	TaskID events.TaskID `json:"taskId"`
	Repo   events.Repo   `json:"repo"`
	Branch events.Branch `json:"branch"`
	Commit events.Commit `json:"commit"`
}

// MergePlanRequest asks the integrator to plan the merge sequence for the run's done, unmerged work.
type MergePlanRequest struct {
	Run      RunRef       `json:"run"`
	Branches []TaskBranch `json:"branches"`
}

// ArtifactSpec names what a provider's publish step releases.
type ArtifactSpec struct {
	// Kind is, for example, go-module, container-image or schema.
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// MergeStep merges one repo's feature branch. Providers merge first and may publish; consumers merge after every
// provider, with their dependencies bumped to what the providers published.
type MergeStep struct {
	Repo      events.Repo     `json:"repo"`
	Branch    events.Branch   `json:"branch"`
	Provider  bool            `json:"provider,omitempty"`
	Publish   *ArtifactSpec   `json:"publish,omitempty"`
	DependsOn []events.Repo   `json:"dependsOn,omitempty"`
	Tasks     []events.TaskID `json:"tasks,omitempty"`
}

// MergePlan is the integrator's merge sequence.
type MergePlan struct {
	Steps []MergeStep `json:"steps"`
	Usage Usage       `json:"usage,omitzero"`
}

// Artifact is something a publish step made available.
type Artifact struct {
	Kind    string        `json:"kind"`
	Name    string        `json:"name"`
	Version string        `json:"version"`
	Repo    events.Repo   `json:"repo"`
	Commit  events.Commit `json:"commit"`
}

// MergeRequest asks the CI/CD adapter to merge a step's branch into main. Bumps lists the artifacts published so
// far in the run, for a consumer to depend on.
type MergeRequest struct {
	Run   RunRef     `json:"run"`
	Step  MergeStep  `json:"step"`
	Bumps []Artifact `json:"bumps,omitempty"`
}

// MergeStatus reports why a Merge attempt succeeded or could not merge yet.
type MergeStatus string

const (
	MergeMerged       MergeStatus = "merged"
	MergeConflict     MergeStatus = "conflict"
	MergeBehind       MergeStatus = "behind"
	MergeChecksFailed MergeStatus = "checks-failed"
)

// MergeResult is main's new head, or why the branch could not be merged yet.
type MergeResult struct {
	Status      MergeStatus   `json:"status,omitempty"`
	Commit      events.Commit `json:"commit,omitempty"`
	PullRequest events.URL    `json:"pullRequest,omitempty"`
	Summary     string        `json:"summary,omitempty"`
}

// PublishRequest asks the CI/CD adapter to run a publish step for a merged provider.
type PublishRequest struct {
	Run      RunRef        `json:"run"`
	Repo     events.Repo   `json:"repo"`
	Commit   events.Commit `json:"commit"`
	Artifact ArtifactSpec  `json:"artifact"`
}

// WatchRequest asks the CI/CD adapter to watch main's pipeline at a commit until it finishes. Check counts the
// watches of this repo in this merge step, from 1.
type WatchRequest struct {
	Run    RunRef        `json:"run"`
	Repo   events.Repo   `json:"repo"`
	Commit events.Commit `json:"commit"`
	Check  int           `json:"check"`
}

// WatchResult is main's pipeline outcome.
type WatchResult struct {
	Green    bool          `json:"green"`
	Commit   events.Commit `json:"commit"`
	Pipeline events.URL    `json:"pipeline,omitempty"`
	Summary  string        `json:"summary,omitempty"`
}

// FixMainRequest asks the integrator to fix a red main forward, or revert the run's merge. Escalate is set once
// the workflow's fix attempts are used up: the integrator must raise a brief instead of trying again.
type FixMainRequest struct {
	Run       RunRef        `json:"run"`
	Repo      events.Repo   `json:"repo"`
	Commit    events.Commit `json:"commit"`
	Summary   string        `json:"summary,omitempty"`
	Attempt   int           `json:"attempt"`
	Escalate  bool          `json:"escalate,omitempty"`
	Decisions []Decision    `json:"decisions,omitempty"`
	Feedback  []string      `json:"feedback,omitempty"`
	Limit     Budget        `json:"limit,omitzero"`
}

// FixMainResult is main's head after the fix or revert, a fix branch awaiting checks, or the brief the integrator raised.
type FixMainResult struct {
	Commit   events.Commit `json:"commit,omitempty"`
	Branch   events.Branch `json:"branch,omitempty"`
	Reverted bool          `json:"reverted,omitempty"`
	Brief    *BriefRef     `json:"brief,omitempty"`
	Usage    Usage         `json:"usage,omitzero"`
}

// RevertRequest asks the CI/CD adapter to revert a merge commit on main.
type RevertRequest struct {
	Run    RunRef        `json:"run"`
	Repo   events.Repo   `json:"repo"`
	Commit events.Commit `json:"commit"`
	Reason string        `json:"reason,omitempty"`
}

// RevertResult is main's head after reverting the merge.
type RevertResult struct {
	Commit      events.Commit `json:"commit"`
	PullRequest events.URL    `json:"pullRequest,omitempty"`
}

// UpdateBranchRequest asks the integrator agent to update a run branch (merge origin/main, resolve conflicts,
// bump published dependencies).
type UpdateBranchRequest struct {
	Run      RunRef        `json:"run"`
	Repo     events.Repo   `json:"repo"`
	Branch   events.Branch `json:"branch"`
	Reason   string        `json:"reason,omitempty"`
	Bumps    []Artifact    `json:"bumps,omitempty"`
	Attempt  int           `json:"attempt"`
	Feedback []string      `json:"feedback,omitempty"`
	Limit    Budget        `json:"limit,omitzero"`
}

// UpdateBranchResult is the outcome of updating a run branch.
type UpdateBranchResult struct {
	Outcome      Outcome       `json:"outcome"`
	Commit       events.Commit `json:"commit,omitempty"`
	CodeChanged  bool          `json:"codeChanged,omitempty"`
	Brief        *BriefRef     `json:"brief,omitempty"`
	Reason       string        `json:"reason,omitempty"`
	Summary      string        `json:"summary,omitempty"`
	FilesChanged []string      `json:"filesChanged,omitempty"`
	Usage        Usage         `json:"usage,omitzero"`
}

// ReportRequest asks the integrator for the feature result: merged branches, pipeline evidence, test results and
// rule-check reports, traced back to the intent.
type ReportRequest struct {
	Run       RunRef                          `json:"run"`
	Merges    []events.RunDeliveredMergesElem `json:"merges"`
	Published []Artifact                      `json:"published,omitempty"`
	Tasks     []TaskStatus                    `json:"tasks"`
}

// BriefRequest asks for a review brief document, per the review brief template.
type BriefRequest struct {
	Run      RunRef               `json:"run"`
	ID       events.BriefID       `json:"id"`
	Category events.BriefCategory `json:"category"`
	// To is who answers: a role, person or group.
	To       string           `json:"to"`
	RaisedBy events.AgentRole `json:"raisedBy"`
	Urgency  events.Urgency   `json:"urgency"`
	Deadline time.Time        `json:"deadline"`
	// Default is the option id applied at the deadline, or "pause".
	Default        string          `json:"default"`
	Blocks         []events.TaskID `json:"blocks"`
	Question       string          `json:"question"`
	Context        string          `json:"context"`
	Options        []BriefOption   `json:"options"`
	Recommendation string          `json:"recommendation"`
	Precedents     string          `json:"precedents,omitempty"`
}

// BriefOption is one option of a brief; its id is a capital letter.
type BriefOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Text        string `json:"text"`
	Impact      string `json:"impact"`
	Recommended bool   `json:"recommended,omitempty"`
}

// OnboardRequest asks the onboarder to inspect a repo and propose its profile (ADR 0013). Run identifies the
// attachment: RunID and IntentID are the attachment id (ATT-0001), and Package is empty, since an attachment has no
// intent package.
type OnboardRequest struct {
	Run  RunRef      `json:"run"`
	Repo events.Repo `json:"repo"`
	// Round counts the proposals of this attachment, from 1; a revision asked for by the architect is the next round.
	Round int `json:"round"`
	// Current is the repo's approved profile, when it has one: a change starts from it.
	Current *documents.RepoProfile `json:"current,omitempty"`
	// Previous is the proposal the architect asked to revise, and Feedback what they said about it.
	Previous *documents.RepoProfile `json:"previous,omitempty"`
	Feedback []string               `json:"feedback,omitempty"`
	Limit    Budget                 `json:"limit,omitzero"`
}

// OnboardResult is the onboarder's proposal: a profile that passed the repo profile schema, and its evidence for
// the architect (Markdown: which manifests, CI files and Dockerfiles each field comes from).
type OnboardResult struct {
	Profile  documents.RepoProfile `json:"profile"`
	Evidence string                `json:"evidence"`
	Usage    Usage                 `json:"usage,omitzero"`
}

// SmokeRequest asks the agent runtime to smoke-test a toolchain image: run it as an agent Job's containers would,
// with HOTL's tools injected and no credential, and check that it has what a Job needs (ADR 0013).
type SmokeRequest struct {
	// Run identifies the registration or attachment that asks.
	Run RunRef `json:"run"`
	// Image is the pinned image reference to test.
	Image string `json:"image"`
	// Commands are commands that must exist in the image, such as the first words of a profile's checks.
	Commands []string `json:"commands,omitempty"`
}

// SmokeResult is the smoke test's verdict and each check.
type SmokeResult struct {
	Passed bool         `json:"passed"`
	Checks []SmokeCheck `json:"checks"`
}

// SmokeCheck is one check of a smoke test, such as "git", "claude" or "command go".
type SmokeCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Output string `json:"output,omitempty"`
}

// ScopeRequest asks the scoper which repos the run touches (ADR 0018). It reads only the knowledge graph, through
// the MCP server's read-only tools: no clone, no GitHub token.
type ScopeRequest struct {
	Run RunRef `json:"run"`
	// Hints are the intent package's repo hints. The scoper checks them against the graph instead of trusting them.
	Hints []events.Repo `json:"hints"`
	// Version is the scope version this proposal becomes.
	Version events.ScopeVersion `json:"version"`
	// Previous is the scope the run had, when it scopes again: a correction, an approved scope change, a re-signed
	// intent.
	Previous *events.Scope `json:"previous,omitempty"`
	// Feedback is why it scopes again: an architect's answer, an agent's scope change, a new intent version.
	Feedback []string `json:"feedback,omitempty"`
	Limit    Budget   `json:"limit,omitzero"`
}

// ScopeResult is the scoper's proposal, after the agent runtime checked it: every repo exists and is attached or is
// proposed as new, every evidence node is in the graph (with the graph's commit), every hint has a verdict, and
// Boundaries holds the boundary rules the scope crosses by their conditions.
type ScopeResult struct {
	Scope events.Scope `json:"scope"`
	Usage Usage        `json:"usage,omitzero"`
}

// CreateRepoRequest asks the CI/CD adapter to create a repo the architect approved for a run's scope (ADR 0018): in
// the HOTL organisation, with a skeleton for its technology, a CI workflow whose check gates merges, and a ruleset
// on the default branch where GitHub allows one.
type CreateRepoRequest struct {
	Run          RunRef              `json:"run"`
	ScopeVersion events.ScopeVersion `json:"scopeVersion"`
	Repo         events.ScopeNewRepo `json:"repo"`
	// BriefID is the architect's brief that approved it.
	BriefID events.BriefID `json:"briefId"`
}

// CreateRepoResult is the created repo. A retried request finds the repo it created and returns it again.
type CreateRepoResult struct {
	Repo          events.Repo   `json:"repo"`
	URL           events.URL    `json:"url"`
	DefaultBranch events.Branch `json:"defaultBranch"`
	Commit        events.Commit `json:"commit"`
	CICheck       string        `json:"ciCheck"`
	Visibility    string        `json:"visibility"`
	Protected     bool          `json:"protected"`
	// Protection says why the default branch is not protected, when it is not.
	Protection string `json:"protection,omitempty"`
}
