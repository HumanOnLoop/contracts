package documents

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestParseRepoProfile(t *testing.T) {
	raw, err := os.ReadFile("testdata/repo-profile-playground.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseRepoProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Repo != "playground" || p.Ci.Check != "ci" || p.MergeStrategy() != "squash" || p.Number() != 0 {
		t.Errorf("parsed %+v", p)
	}
	var names []string
	for _, c := range p.Checks() {
		names = append(names, c.Name)
	}
	if !slices.Equal(names, []string{"format", "lint", "build", "test"}) {
		t.Errorf("checks %v", names)
	}
	for f, want := range map[string]bool{"main_test.go": true, "a/b/x_test.go": true, "testdata/in.txt": true,
		"a/testdata/b/c": true, "main.go": false, "test.go": false} {
		if got := p.IsTestFile(f); got != want {
			t.Errorf("IsTestFile(%q) = %v", f, got)
		}
	}
	if err := ValidateRepoProfile(p); err != nil {
		t.Error(err)
	}
}

func TestParseRepoProfileRefuses(t *testing.T) {
	for name, raw := range map[string]string{
		"unknown field":  `{"repo":"r","defaultBranch":"main","languages":["go"],"image":"i","commands":{"test":"t","testReport":"go-test-json"},"testFiles":["*"],"ci":{"check":"ci"},"merge":{"strategy":"squash"},"extra":1}`,
		"missing ci":     `{"repo":"r","defaultBranch":"main","languages":["go"],"image":"i","commands":{"test":"t","testReport":"go-test-json"},"testFiles":["*"],"merge":{"strategy":"squash"}}`,
		"unknown report": `{"repo":"r","defaultBranch":"main","languages":["go"],"image":"i","commands":{"test":"t","testReport":"tap"},"testFiles":["*"],"ci":{"check":"ci"},"merge":{"strategy":"squash"}}`,
		"unknown merge":  `{"repo":"r","defaultBranch":"main","languages":["go"],"image":"i","commands":{"test":"t","testReport":"go-test-json"},"testFiles":["*"],"ci":{"check":"ci"},"merge":{"strategy":"ff"}}`,
		"two publishes":  `{"repo":"r","defaultBranch":"main","languages":["go"],"image":"i","commands":{"test":"t","testReport":"go-test-json"},"testFiles":["*"],"ci":{"check":"ci"},"merge":{"strategy":"squash"},"publish":[{"kind":"k","name":"n","how":"semver-tag"},{"kind":"k","name":"m","how":"semver-tag"}]}`,
		"no languages":   `{"repo":"r","defaultBranch":"main","languages":[],"image":"i","commands":{"test":"t","testReport":"go-test-json"},"testFiles":["*"],"ci":{"check":"ci"},"merge":{"strategy":"squash"}}`,
		"not json":       `repo: r`,
	} {
		if _, err := ParseRepoProfile([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		} else if testing.Verbose() {
			t.Logf("%s: %v", name, strings.SplitN(err.Error(), "\n", 2)[0])
		}
	}
}
