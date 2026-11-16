// Package knowledge defines the knowledge graph's model as the harvesters and the knowledge graph share it (ADR
// 0015, ADR 0016): node labels, edge types, and the harvest result a RepoHarvested event references.
//
// Every node carries Key, unique across the graph, and the provenance props Repo, Commit and HarvestedAt (PropRepo,
// PropCommit, PropHarvestedAt), so agents can tell fresh knowledge from stale. A node's Origin says who owns it:
// a harvest (replaced when its repo is harvested again), a stub (a node another repo's code refers to before its own
// repo is harvested), or an event (run history).
package knowledge

import (
	"fmt"
	"time"
)

// Node labels.
const (
	LabelRepo         = "Repo"
	LabelPackage      = "Package"
	LabelSymbol       = "Symbol"
	LabelEndpoint     = "Endpoint"
	LabelTopic        = "Topic"
	LabelEvent        = "Event"
	LabelSchema       = "Schema"
	LabelModule       = "Module"
	LabelRule         = "Rule"
	LabelRunbook      = "Runbook"
	LabelADR          = "ADR"
	LabelIntent       = "Intent"
	LabelRun          = "Run"
	LabelTask         = "Task"
	LabelBrief        = "Brief"
	LabelDecision     = "Decision"
	LabelIntervention = "Intervention"
	LabelPerson       = "Person"
	LabelRole         = "Role"
)

// Labels lists every node label.
var Labels = []string{LabelRepo, LabelPackage, LabelSymbol, LabelEndpoint, LabelTopic, LabelEvent, LabelSchema,
	LabelModule, LabelRule, LabelRunbook, LabelADR, LabelIntent, LabelRun, LabelTask, LabelBrief, LabelDecision,
	LabelIntervention, LabelPerson, LabelRole}

// Edge types.
const (
	EdgeImports   = "IMPORTS"    // Package → Package
	EdgeDependsOn = "DEPENDS_ON" // Repo → Module
	EdgeProduces  = "PRODUCES"   // Package → Event
	EdgeConsumes  = "CONSUMES"   // Package → Event or Topic
	EdgeExposes   = "EXPOSES"    // Package → Endpoint
	EdgeGoverns   = "GOVERNS"    // Rule → Repo (its scope)
	EdgeTouched   = "TOUCHED"    // Run → Package
	EdgeDecided   = "DECIDED"    // Brief → Decision
	EdgeCites     = "CITES"      // Decision → Rule or Decision (a precedent)

	EdgeContains  = "CONTAINS"   // Repo → Package, Package → Symbol, Repo → ADR, Repo → Runbook, Repo → Schema
	EdgeInTopic   = "IN_TOPIC"   // Event → Topic
	EdgeDescribes = "DESCRIBES"  // Schema → Event
	EdgeFor       = "FOR"        // Run → Intent
	EdgeHasTask   = "HAS_TASK"   // Run → Task
	EdgeRaised    = "RAISED"     // Run → Brief
	EdgeDecidedBy = "DECIDED_BY" // Decision → Person
	EdgeSteered   = "STEERED"    // Run → Intervention
	EdgeBy        = "BY"         // Intervention → Person
	EdgeHasRole   = "HAS_ROLE"   // Person → Role
)

// Provenance props every node carries.
const (
	PropKey         = "key"
	PropRepo        = "repo"
	PropCommit      = "commit"
	PropHarvestedAt = "harvestedAt"
	PropOrigin      = "origin"
)

// Origins of a node or edge.
const (
	OriginHarvest = "harvest"
	OriginStub    = "stub"
	OriginEvent   = "event"
)

// Keys of nodes, unique across the graph.
func RepoKey(repo string) string          { return "repo:" + repo }
func PackageKey(importPath string) string { return "pkg:" + importPath }
func SymbolKey(importPath, name string) string {
	return "sym:" + importPath + "." + name
}
func EndpointKey(repo, route string) string { return "endpoint:" + repo + ":" + route }
func TopicKey(topic string) string          { return "topic:" + topic }
func EventKey(event string) string          { return "event:" + event }
func SchemaKey(path string) string          { return "schema:" + path }
func ModuleKey(path string) string          { return "module:" + path }
func RuleKey(id string) string              { return "rule:" + id }
func RunbookKey(role string) string         { return "runbook:" + role }
func ADRKey(number string) string           { return "adr:" + number }
func IntentKey(id string) string            { return "intent:" + id }
func RunKey(id string) string               { return "run:" + id }
func TaskKey(run, task string) string       { return "task:" + run + ":" + task }
func BriefKey(id string) string             { return "brief:" + id }
func DecisionKey(briefID string) string     { return "decision:" + briefID }
func InterventionKey(eventID string) string { return "intervention:" + eventID }
func PersonKey(name string) string          { return "person:" + name }
func RoleKey(role string) string            { return "role:" + role }

// Node is a node of the graph: its label, key and props (provenance included).
type Node struct {
	Label string         `json:"label"`
	Key   string         `json:"key"`
	Props map[string]any `json:"props"`
}

// Ref names a node by label and key.
type Ref struct {
	Label string `json:"label"`
	Key   string `json:"key"`
}

// Edge is a directed edge.
type Edge struct {
	Type  string         `json:"type"`
	From  Ref            `json:"from"`
	To    Ref            `json:"to"`
	Props map[string]any `json:"props,omitempty"`
}

// HarvestResult is what a harvester found in a repo at a commit: the repo's whole harvested subgraph. Loading it
// replaces the nodes and edges a previous harvest of the repo made; edges may point at nodes of other repos, which
// the loader creates as stubs when they are missing.
type HarvestResult struct {
	Version     int       `json:"version"`
	Repo        string    `json:"repo"`
	Commit      string    `json:"commit"`
	Harvester   string    `json:"harvester"`
	Mode        string    `json:"mode"`
	HarvestedAt time.Time `json:"harvestedAt"`
	Nodes       []Node    `json:"nodes"`
	Edges       []Edge    `json:"edges"`
	// Problems are what the harvester could not read (a package that does not type-check, a malformed rule), so
	// a partial harvest is visible.
	Problems []string `json:"problems,omitempty"`
}

// ResultVersion is the HarvestResult format this package writes and reads.
const ResultVersion = 1

// Check reports a result whose nodes and edges are inconsistent: a node without a key or label, a duplicate key,
// a node of another repo, or an edge whose source is not one of the result's nodes.
func (r HarvestResult) Check() error {
	if r.Version != ResultVersion {
		return fmt.Errorf("harvest result version %d; this reads version %d", r.Version, ResultVersion)
	}
	keys := map[string]string{}
	for _, n := range r.Nodes {
		if n.Key == "" || n.Label == "" {
			return fmt.Errorf("a node has no key or label: %+v", n)
		}
		if _, dup := keys[n.Key]; dup {
			return fmt.Errorf("node %s appears twice", n.Key)
		}
		if repo, _ := n.Props[PropRepo].(string); repo != r.Repo {
			return fmt.Errorf("node %s is of repo %q, not %q", n.Key, repo, r.Repo)
		}
		keys[n.Key] = n.Label
	}
	for _, e := range r.Edges {
		if label, ok := keys[e.From.Key]; !ok || label != e.From.Label {
			return fmt.Errorf("edge %s from %s: not a node of the result", e.Type, e.From.Key)
		}
		if e.Type == "" || e.To.Key == "" || e.To.Label == "" {
			return fmt.Errorf("edge %+v is incomplete", e)
		}
	}
	return nil
}
