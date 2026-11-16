# contracts

Part of HOTL (Human On The Loop). The schemas every HOTL service shares, and the Go library generated from them:

- the **event envelope** and a schema for each of the **42 events** in the catalogue, plus RepoAttachRequested,
  RepoProfileApproved, ToolchainImageRequested and ToolchainImageApproved (ADR 0013), one directory per topic;
- the **front matter** of intent packages, review briefs and rules (the rule envelope), and the **repo profile**
  and the **toolchain image** catalogue entry (JSON, ADR 0013);
- a Go module, `github.com/HumanOnLoop/contracts`, with generated types and helpers to build, validate, produce and
  consume events.

The schemas are the source of truth. Change a schema, run `make generate`, commit both. The knowledge layer's
shared types are in `knowledge/` (the graph's labels, edges and the harvest result, ADRs 0015 and 0016) and `mcp/`
(the MCP server's tools, their inputs and outputs, ADR 0017).

Why it is built this way (generator, Kafka client, versioning): [ADR 0002](../docs/adr/0002-contracts-schemas-library-and-versioning.md).

## Layout

| Path | What |
| --- | --- |
| `events/envelope.schema.json` | The CloudEvents envelope every event travels in |
| `events/common.schema.json` | Shared definitions (ids, document references, enums). Not an event |
| `events/<topic>/<Event>.schema.json` | One payload schema per event; the directory is the topic (`run` → `hotl.run`) |
| `events/testdata/<Event>.json` | A valid example payload for every event, checked by the tests |
| `documents/*.schema.json` | Intent package, review brief and rule front matter |
| `documents/testdata/` | The worked examples from the template docs, verbatim, and a rule |
| `events`, `documents`, `kafka` | Go packages (`*_gen.go` is generated, never edit it) |
| `agents` | The Temporal activity contract between the orchestrator and the workers that do a run's work: activity names, request and result types, plan validation, review brief rendering (ADR 0006), the tester's report and the reviewer's findings (ADR 0007) |
| `internal/` | Schema compiler and bundler, the compatibility checker, the registry client, code generators |

## Events

Every event is a CloudEvents 1.0 event in **structured JSON mode**: the Kafka record value is the whole envelope,
with header `content-type: application/cloudevents+json; charset=UTF-8`.

```json
{
  "specversion": "1.0",
  "id": "9b2f0c5e-6f1a-4c43-8f0e-2d4c1b7a9e10",
  "source": "/hotl/orchestrator",
  "type": "RunStarted",
  "time": "2026-11-14T09:00:00Z",
  "subject": "RUN-0042-1",
  "datacontenttype": "application/json",
  "runid": "RUN-0042-1",
  "intentid": "INT-0042",
  "traceid": "4bf92f3577b34da6a3ce929d0e0e4736",
  "data": { "intentVersion": 1, "developer": "Developer, Orders team", "autonomy": "L3", "repos": ["orders-service"] }
}
```

- **Extension attributes are `runid`, `intentid` and `traceid`**, all lowercase. The design calls them runId,
  intentId and traceId, but CloudEvents only allows lowercase letters and digits in attribute names. In Go they
  are `RunID`, `IntentID` and `TraceID`.
- **`subject` is the aggregate id and the Kafka key**: the run, intent, brief or rule id, or the repo for
  `hotl.knowledge`. Two kinds of event come before an id exists. `IntentSubmitted` and `IntentRejected` use the
  submission id, because intake assigns intent ids. `BriefRaised` and `BriefRejected` use the agent's request id,
  because the review service assigns brief ids. `IntentValidated` and `BriefDelivered` then carry that
  submission or request id, so the two can be matched up.
- **Payloads are minimal**: ids, state, and the facts a consumer acts on. They don't repeat what the envelope
  already carries (subject, run, intent). Documents (intent packages, plans, briefs, rules, reports) are never
  copied in. A `DocumentRef` (`uri` + `sha256`) points at the exact immutable content.
- **Identifier formats are not constrained** (`RUN-0042-1` is a plain non-empty string). The registry treats any
  pattern change as breaking, even a looser one, so only formats fixed by a standard carry a pattern: git commit
  ids, SHA-256 digests and W3C trace ids. The service that assigns an id checks its format.

| Topic | Events |
| --- | --- |
| `hotl.intent` | IntentSubmitted, IntentValidated, IntentRejected, IntentSignedOff, IntentVersionSigned, RepoAttachRequested, ToolchainImageRequested |
| `hotl.run` | RunStarted, PlanCreated, PlanApproved, TaskStarted, TaskChecked, TaskCompleted, TaskFailed, BranchPushed, MergeCompleted, ArtifactPublished, MainVerified, MainBroken, BudgetExceeded, RunPaused, RunResumed, RunSteered, RunDelivered, RunAccepted, RunAbandoned |
| `hotl.brief` | BriefRaised, BriefRejected, BriefDelivered, BriefForwarded, BriefEscalated, BriefWithdrawn, DecisionRecorded |
| `hotl.rule` | RuleProposed, RuleApproved, RuleRejected, RuleActivated, RuleRetired, RepoProfileApproved, ToolchainImageApproved |
| `hotl.knowledge` | RepoHarvested, GraphUpdated, PrecedentMatched |

Each schema's description names its producer, main consumers and subject; the design's
[event catalogue](../docs/design/HOTL%20—%20Run%20State%20Machine%20and%20Event%20Catalogue.md) has the full picture.

## Agent activities

Package `agents` is not generated from a schema. It holds the Go types that cross Temporal between the
orchestrator's run workflow and the workers that implement its activities, such as the agent runtime on task queue
`agents.TaskQueue` (`hotl-agents`). The activity names (`agents.ActivityPlan`, `agents.ActivityImplement`, …) and
the JSON field names are recorded in workflow histories, so they change only compatibly: add optional fields, and
never rename or remove one. It also has `ValidateTasks` (the plan checks the workflow applies), `RenderBrief` (a
review brief per the template, checked against its schema) and `Inline`/`DecodeInline` (documents carried in
`data:` URIs until a document store exists).

Since v0.6.0 `TaskResult.Rejected` carries a brief the review service rejected on every try (`RejectedBrief`), and
`BriefOnRejected` writes the budget brief the orchestrator raises on it: the rejected brief and its reasons in
fenced blocks nothing in them can close, so it passes the review service's checks (ADR 0010).

Since v0.7.0 `PushResult.NoChanges` reports a task whose work adds nothing to the default branch, so the run
can tell an intent that is already satisfied on main from a failed push (ADR 0011).

## Repo profiles

`documents/repo-profile.schema.json` (since v0.8.0, ADR 0013) is how HOTL works on one attached repo: its
languages, the agent toolchain image, the format, lint, build and test commands of the tester's verified run (and
how to read its test report: `go-test-json` or `junit-xml`), the command prefixes agents may run, the test files,
the CI check that gates merges, the merge strategy, publish steps, the default branch and its dependencies.
`documents.ParseRepoProfile` validates one; `RepoProfile.Checks()` lists the verified run's commands in order.
The review service stores the versions and records their approval; package `profiles` is the client the other
services read them with. `agents.ActivityOnboard` (`OnboardRequest`, `OnboardResult`) asks the onboarder agent for
a proposal, and `onboarder` is a new `AgentRole`.

Since v0.9.0 a profile's `image` names an entry of the **toolchain image catalogue**
(`documents/toolchain-image.schema.json`): a pinned image (`registry/name@sha256:…` or a node-local `sha256:<id>`),
its languages and description. `documents.ParseToolchainImage` validates one; the `profiles` client reads and
proposes entries (`GetImage`, `ListImages`, `ProposeImage`); `agents.ActivitySmokeImage` (`SmokeRequest`,
`SmokeResult`) smoke-tests an image.

## Approvals

Since v0.13.0 (ADR 0021) a rule can be `kind: approval`: a change touching a file in its scope (repos and paths)
needs the rule's owner to approve those files before it is pushed, such as an architect approving a new ADR.
`ReviewResult.Approvals` (`Approval`, `ApprovalFile`) carries the rules a change meets and each file's content at the
reviewed commit (at most `MaxApprovalContent`) with its SHA-256; an approval holds for exactly that content.

## Scopes

Since v0.12.0 (ADR 0018) a run is scoped before it is planned. An intent package names optional `repo_hints`;
`repos` is deprecated and read as hints (`IntentPackage.Hints()`). `IntentSignedOff` carries them as `repoHints`
(and, for older consumers, `repos`, which may now be empty). The **scope** (`events.Scope` in
`events/common.schema.json`) lists, per existing repo, a reason, a confidence and evidence (knowledge graph node
keys with their source commit); proposed new repos (name, purpose, technology, why, a draft profile); the
rule-protected boundaries it crosses; and a verdict on every hint. Three events carry it on `hotl.run`:
`ScopeProposed`, `ScopeConfirmed` and `RepoCreated`. `agents.ActivityScope` (`ScopeRequest`, `ScopeResult`) asks the
scoper, `agents.ActivityCreateRepo` (`CreateRepoRequest`, `CreateRepoResult`) asks the CI/CD adapter for a new repo,
and `PlanResult.OutOfScope` and `TaskResult.OutOfScope` (`ScopeChange`) are how the planner and the implementer ask
for more. `scoper` is a new `AgentRole`.

Rules gain a kind: `kind: boundary` with `evaluator: condition` and a `boundary` condition (`repos`, `anyRepos`,
`dependents: {of, atLeast}`, `newRepo`). Package `scopes` evaluates boundary rules against a scope
(`Boundaries`, with dependencies from the repos' approved profiles), decides what a run does with a proposal
(`Next`: confirm, developer or architect) and checks new repo names. `runs.QueryScope` (`ScopeStatus`) is the run
workflow's query for its scope, which the agent runtime asks before it mints a GitHub token. The MCP server gains
`list_repos` and `get_nodes`, and packages, endpoints and symbols carry their graph `node` key.

Dropping `repos` from the intent package's required properties breaks HOTL's rule that a required property is
never dropped; `internal/compat` allows it against v0.11.0 only, with the reason, as a named deprecation.

## Run signals

Package `runs` (since v0.6.0) has the signals other services send a run's workflow: `SignalDecision` with
`DecisionSignal`, which the review service sends when a decision is recorded, and `SignalAbort` with `AbortSignal`.
Their names and JSON are recorded in workflow histories and change only compatibly.

Since v0.4.0 the tester and the reviewer have their types (ADR 0007): `TestReport` (criterion-to-test mapping and
the runtime's verified run, on `TaskResult.TestReport` and `CheckRequest.TestReport`) and `Finding` (rule id or
`general`, file and line, `blocking` or `advisory`, issue and fix, on `ReviewResult.Findings`; `Finding.String`
is the line the implementer's next attempt gets).

## Using the Go library

The module is private on GitHub. Fetch it directly, not through the public proxy:

```sh
go env -w GOPRIVATE='github.com/HumanOnLoop/*'   # once per machine; git must be able to read the org (gh auth setup-git)
go get github.com/HumanOnLoop/contracts@v0.4.0
```

Services depend on a tagged release like any Go module. To work on contracts and a service together, use the
uncommitted `go.work` described in the workspace's CLAUDE.md, not a `replace` directive.

```go
import (
	"github.com/HumanOnLoop/contracts/documents"
	"github.com/HumanOnLoop/contracts/events"
	"github.com/HumanOnLoop/contracts/kafka"
)

// Build: New fills in specversion, id, time and type, and validates against both schemas.
env, err := events.New(events.RunStarted{
	IntentVersion: 1, Developer: "Developer, Orders team",
	Autonomy: events.AutonomyL3, Repos: []events.Repo{"orders-service"},
}, events.Meta{Source: "/hotl/orchestrator", Subject: "RUN-0042-1", RunID: "RUN-0042-1", IntentID: "INT-0042"})

// Produce: validates again, keys by subject, waits for acks.
p, err := kafka.NewProducer([]string{"localhost:19092"})
err = p.Publish(ctx, env)

// Consume: at least once; offsets are committed after the handler returns nil. Dedupe on msg.ID.
c, err := kafka.NewConsumer([]string{"localhost:19092"}, "orchestrator", []string{events.TopicRun, events.TopicBrief})
err = c.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
	payload, err := msg.Decode()    // *events.RunStarted, *events.DecisionRecorded, ...
	if errors.Is(err, events.ErrUnknownType) {
		return nil                  // a newer producer's event type; not ours to handle
	}
	...
})

// Read without a group, from a time on (since v0.6.0): nothing is committed; read again from a recent time after
// a restart.
r, err := kafka.NewReader([]string{"localhost:19092"}, []string{events.TopicBrief}, time.Now().Add(-time.Hour))
err = r.Run(ctx, handler)

// Documents: parse and validate front matter, get the Markdown body.
ip, body, err := documents.ParseIntentPackage(markdown)
rule, text, err := documents.ParseRule(markdown)   // rule.InScope(repo, changedFiles) applies its scope
```

- `events.Validate` / `events.ValidateJSON` check an event against its schemas and return a
  `*events.ValidationError` that lists every problem with a JSON pointer.
- In Temporal workflows, set `Meta.ID` to a deterministic id so a replayed workflow never emits a duplicate.
- Producers outside Temporal write envelopes to an outbox and relay them with `kafka.NewRecord`.
- **Producers validate; consumers are tolerant readers.** Decoding ignores unknown properties and keeps enum
  values it doesn't know, so a consumer keeps working when a producer moves to a newer contracts release first.
  `Required` arrays must be non-nil (`[]` not `null`); `New` tells you if one is.

## Changing a schema

Changes must be **backward compatible**: the new schema accepts every event the old one accepted, because the
topics keep events forever and consumers replay them. Two checks enforce this:

1. `go test ./...` compares every schema with the last release tag (`v*`). It fails on an incompatible change,
   and on a released schema that was removed. This is `internal/compat`, which follows the registry's rules.
2. `make register` has the registry check each new version under its BACKWARD rule before storing it.

| Allowed | Not allowed |
| --- | --- |
| Add an optional property | Add a required property, or make an optional one required |
| Add an enum value | Remove or rename a property, or an enum value |
| Add a new event (a new file) | Change a type, or add or change a pattern, format or const |
| Relax a bound (`maxLength` up, `minimum` down) | Tighten a bound, or add one |
| Edit descriptions and titles | Drop a property from `required` (HOTL rule: old consumers rely on it) |

Rules every schema follows (checked by `internal/schema` tests):

- **draft-07** (`"$schema": "http://json-schema.org/draft-07/schema#"`). Apicurio 3.3's compatibility checker
  rejects draft 2019-09 and 2020-12 schemas with "could not determine version".
- **`"additionalProperties": false` on every object.** With an open content model the registry calls *adding* an
  optional property breaking, which would rule out the one change we make most.
- `$id` is `https://github.com/osbe/contracts/<path>`. A file shares definitions only by `$ref` into
  `events/common.schema.json#/definitions/<Name>`. Registration bundles them into each schema, so registered
  schemas stand alone.
- Event schemas: `title` equals the file name, and `description` names the producer, consumers and subject.

A change you can't make compatibly is a **new event** (for example `RunStartedV2` next to `RunStarted`), produced
alongside the old one until no consumer needs it.

## Make targets

| Target | Does |
| --- | --- |
| `make test` | Unit tests, including the compatibility check against the last release |
| `make lint` | `go vet` and a check that generated code is up to date |
| `make generate` | Regenerate `events/types_gen.go`, `events/catalog_gen.go`, `documents/types_gen.go` |
| `make register` | Register every schema in Apicurio on `k3d-hotl`, setting COMPATIBILITY=BACKWARD and VALIDITY=FULL on each artifact |
| `make check-registry` | Dry-run every schema against the registry's latest version (stores nothing) |
| `make test-kafka` | Produce and consume through Kafka on `k3d-hotl`, on temporary topics it deletes afterwards |

The cluster targets need the stack from `platform` (`make up` there). They always use context `k3d-hotl`. They
reach the registry at http://hotl-registry.localhost:8480 and Kafka at localhost:19092 (ADR 0019). In the registry, the envelope is `hotl/Envelope`, events are `hotl.<topic>/<Event>`, and documents
are `hotl.documents/IntentPackage`, `hotl.documents/ReviewBrief` and `hotl.documents/Rule`. Each version created carries a
`contracts-version` label with the release that registered it.

## Versioning

The module follows semantic versioning with tags `vMAJOR.MINOR.PATCH`. Adding events, optional properties or
enum values is a minor release. Fixes to docs or the library are patch releases. Compatibility is enforced from
v0.1.0 on, even before 1.0: retained topics don't care about semver. Registry versions are numbered per artifact
by the registry (1, 2, ...) and only grow when that schema's content changes. See ADR 0002.

## Dependencies

Go 1.27. Runtime dependencies, each approved: [franz-go](https://github.com/twmb/franz-go) (Kafka client),
[santhosh-tekuri/jsonschema](https://github.com/santhosh-tekuri/jsonschema) v6 (validation, with
`golang.org/x/text` for its messages) and [go.yaml.in/yaml/v3](https://github.com/yaml/go-yaml) (front matter).
The code generator, [go-jsonschema](https://github.com/atombender/go-jsonschema), runs through `go run` at a
pinned version and is not a module dependency.

## Design references

- Design: https://claude.ai/artifact/KyWaitPCUe8PWBMs6qEetb
- Intent package template: https://claude.ai/artifact/CmVcaGSmePYkz6Qr11QZgP
- Review brief template: https://claude.ai/artifact/CNLabXr8ZbvQRu9GHrWunn
- Run state machine and event catalogue: https://claude.ai/artifact/KefGCL1P5jbJpxBqyktvhV
