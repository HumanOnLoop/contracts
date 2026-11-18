package documents

import (
	"strings"
	"testing"
)

func TestParseRule(t *testing.T) {
	doc := fixture(t, "rule-GO-ERR-1.md")
	r, body, err := ParseRule([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "GO-ERR-1" || r.Severity != RuleSeverityHard || r.Evaluator != RuleEvaluatorLlm || r.Number() != 1 ||
		r.State() != RuleStatusActive || r.Owner != "architects" || len(r.Scope.Paths) != 1 {
		t.Errorf("rule %+v", r)
	}
	if !strings.HasPrefix(string(body), "\n## Rule\n") {
		t.Errorf("body %q", body)
	}
	if err := ValidateRule(r); err != nil {
		t.Error(err)
	}
	for name, tc := range map[string][2]string{
		"lower-case id":  {"id: GO-ERR-1", "id: go-err-1"},
		"no severity":    {"severity: hard\n", ""},
		"bad severity":   {"severity: hard", "severity: blocking"},
		"version 0":      {"version: 1", "version: 0"},
		"bad evaluator":  {"evaluator: llm", "evaluator: gpt"},
		"unknown field":  {"owner: architects", "owner: architects\nreviewer: me"},
		"unknown status": {"status: active", "status: draft"},
		"scope extra":    {"  paths: [\"**/*.go\"]", "  paths: [\"**/*.go\"]\n  layers: [api]"},
	} {
		if !strings.Contains(doc, tc[0]) {
			t.Fatalf("%s: fixture lacks %q", name, tc[0])
		}
		if _, _, err := ParseRule([]byte(strings.Replace(doc, tc[0], tc[1], 1))); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	shadow := strings.Replace(doc, "status: active\n", "status: shadow\n", 1)
	if r, _, err := ParseRule([]byte(shadow)); err != nil || r.State() != RuleStatusShadow {
		t.Errorf("shadow: %+v, %v", r, err)
	}
	if r, _, err := ParseRule([]byte(strings.Replace(doc, "status: active\n", "", 1))); err != nil || r.State() != RuleStatusActive {
		t.Errorf("no status: %+v, %v", r, err)
	}
}

func TestRuleScope(t *testing.T) {
	goRule := Rule{Scope: RuleScope{Repos: []string{"*"}, Languages: []string{"go"}, Paths: []string{"**/*.go"}}}
	handlers := Rule{Scope: RuleScope{Repos: []string{"playground", "intake"}, Paths: []string{"internal/http/**"}}}
	everywhere := Rule{}
	for _, tc := range []struct {
		rule  Rule
		repo  string
		files []string
		want  bool
	}{
		{goRule, "playground", []string{"main.go"}, true},
		{goRule, "playground", []string{"README.md", "cmd/x/main.go"}, true},
		{goRule, "playground", []string{"README.md", "go.mod"}, false}, // go, but not *.go
		{goRule, "playground", nil, false},
		{handlers, "intake", []string{"internal/http/server.go"}, true},
		{handlers, "intake", []string{"internal/store/db.go"}, false},
		{handlers, "orders-service", []string{"internal/http/server.go"}, false},
		{everywhere, "anything", nil, true},
	} {
		if got := tc.rule.InScope(tc.repo, tc.files); got != tc.want {
			t.Errorf("%+v InScope(%s, %v) = %v", tc.rule.Scope, tc.repo, tc.files, got)
		}
	}
}

func TestMatchPathAndLanguage(t *testing.T) {
	for _, tc := range []struct {
		pattern, file string
		want          bool
	}{
		{"**/*.go", "main.go", true},
		{"**/*.go", "a/b/c.go", true},
		{"*.go", "a/b.go", false},
		{"internal/**", "internal/x/y.go", true},
		{"internal/**/*_test.go", "internal/a_test.go", true},
		{"internal/**/*_test.go", "internal/a.go", false},
		{"cmd/*/main.go", "cmd/agent/main.go", true},
		{"[", "x", false},
	} {
		if got := MatchPath(tc.pattern, tc.file); got != tc.want {
			t.Errorf("MatchPath(%q, %q) = %v", tc.pattern, tc.file, got)
		}
	}
	for file, want := range map[string]string{"a/b.go": "go", "go.mod": "go", "x.yml": "yaml", "Dockerfile.agent": "dockerfile",
		"Makefile": "make", "LICENSE": ""} {
		if got := Language(file); got != want {
			t.Errorf("Language(%s) = %q, want %q", file, got, want)
		}
	}
}

func TestParseApprovalRule(t *testing.T) {
	doc := `---
id: APR-ADR-1
title: A new or changed ADR needs an architect
scope:
  repos: [docs]
  paths: ["adr/*.md"]
severity: hard
owner: architects
version: 1
kind: approval
evaluator: condition
---

## Rule
`
	r, _, err := ParseRule([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind == nil || *r.Kind != RuleKindApproval || r.Evaluator != RuleEvaluatorCondition || r.Scope.Paths[0] != "adr/*.md" {
		t.Errorf("rule %+v", r)
	}
	if _, _, err := ParseRule([]byte(strings.Replace(doc, "kind: approval", "kind: approve", 1))); err == nil {
		t.Error("accepted kind approve")
	}
}

func TestRuleProjectScope(t *testing.T) {
	projLevel := RuleLevelProject
	projRule := Rule{
		Level: &projLevel,
		Scope: RuleScope{
			Projects: []string{"java-k8s"},
			Repos:    []string{"*"},
			Paths:    []string{"**/*.java"},
		},
	}
	orgRule := Rule{
		Scope: RuleScope{
			Repos: []string{"*"},
		},
	}
	if projRule.EffectiveLevel() != RuleLevelProject {
		t.Errorf("got %v", projRule.EffectiveLevel())
	}
	if orgRule.EffectiveLevel() != RuleLevelOrg {
		t.Errorf("got %v", orgRule.EffectiveLevel())
	}
	if !projRule.InProjectScope("acme", "java-k8s", "orders", []string{"src/Main.java"}) {
		t.Error("expected java-k8s repo to match")
	}
	if projRule.InProjectScope("acme", "go-cli", "orders", []string{"src/Main.java"}) {
		t.Error("project rule must not match another project")
	}
	if !orgRule.InProjectScope("acme", "go-cli", "cli", []string{"main.go"}) {
		t.Error("org rule should match any project")
	}
}
