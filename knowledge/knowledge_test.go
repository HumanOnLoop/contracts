package knowledge

import "testing"

func TestCheck(t *testing.T) {
	node := func(label, key, repo string) Node {
		return Node{Label: label, Key: key, Props: map[string]any{PropRepo: repo}}
	}
	ok := HarvestResult{Version: ResultVersion, Repo: "intake", Nodes: []Node{node(LabelRepo, RepoKey("intake"), "intake"),
		node(LabelPackage, PackageKey("github.com/HumanOnLoop/intake/internal/x"), "intake")},
		Edges: []Edge{{Type: EdgeContains, From: Ref{LabelRepo, RepoKey("intake")}, To: Ref{LabelPackage, PackageKey("github.com/HumanOnLoop/intake/internal/x")}},
			{Type: EdgeImports, From: Ref{LabelPackage, PackageKey("github.com/HumanOnLoop/intake/internal/x")}, To: Ref{LabelPackage, PackageKey("github.com/HumanOnLoop/contracts/events")}}}}
	if err := ok.Check(); err != nil {
		t.Fatal(err)
	}
	for name, mod := range map[string]func(*HarvestResult){
		"version":    func(r *HarvestResult) { r.Version = 9 },
		"duplicate":  func(r *HarvestResult) { r.Nodes = append(r.Nodes, r.Nodes[0]) },
		"other repo": func(r *HarvestResult) { r.Nodes = append(r.Nodes, node(LabelPackage, "pkg:y", "contracts")) },
		"foreign from": func(r *HarvestResult) {
			r.Edges = append(r.Edges, Edge{Type: EdgeImports, From: Ref{LabelPackage, "pkg:z"}, To: Ref{LabelPackage, "pkg:x"}})
		},
		"label mismatch": func(r *HarvestResult) {
			r.Edges = append(r.Edges, Edge{Type: EdgeImports, From: Ref{LabelSymbol, RepoKey("intake")}, To: Ref{LabelPackage, "pkg:x"}})
		},
	} {
		r := ok
		r.Nodes = append([]Node(nil), ok.Nodes...)
		r.Edges = append([]Edge(nil), ok.Edges...)
		mod(&r)
		if err := r.Check(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
