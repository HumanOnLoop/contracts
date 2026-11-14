package schema

import (
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/HumanOnLoop/contracts"
)

func TestEmbeddedSchemasCompile(t *testing.T) {
	if _, err := New(contracts.Schemas); err != nil {
		t.Fatal(err)
	}
}

func TestArtifacts(t *testing.T) {
	arts, err := Artifacts(contracts.Schemas)
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string]int{}
	for _, a := range arts {
		groups[a.Group]++
		if strings.HasPrefix(a.Group, "hotl.") && a.Group != "hotl.documents" && a.ID != Name(a.Path) {
			t.Errorf("%s: title %q must equal the file name", a.Path, a.ID)
		}
	}
	want := map[string]int{"hotl": 1, "hotl.intent": 5, "hotl.run": 19, "hotl.brief": 7, "hotl.rule": 5,
		"hotl.knowledge": 3, "hotl.documents": 3}
	for g, n := range want {
		if groups[g] != n {
			t.Errorf("group %s has %d schemas, want %d", g, groups[g], n)
		}
	}
	if len(groups) != len(want) {
		t.Errorf("groups = %v, want %v", groups, want)
	}
}

// TestConventions holds every schema to the rules in the README: draft-07 (the only draft the registry's
// compatibility check reads), a closed content model on every object (so optional properties can be added),
// and required properties that exist.
func TestConventions(t *testing.T) {
	paths, err := Paths(contracts.Schemas)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		b, err := fs.ReadFile(contracts.Schemas, p)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if doc["$schema"] != "http://json-schema.org/draft-07/schema#" {
			t.Errorf("%s: $schema must be draft-07", p)
		}
		if p != "events/common.schema.json" {
			if s, _ := doc["description"].(string); s == "" {
				t.Errorf("%s: needs a description", p)
			}
		}
		walkObjects(doc, "", func(ptr string, o map[string]any) {
			if o["additionalProperties"] != false {
				t.Errorf("%s%s: objects must set additionalProperties to false", p, ptr)
			}
			props, _ := o["properties"].(map[string]any)
			req, _ := o["required"].([]any)
			for _, r := range req {
				if _, ok := props[r.(string)]; !ok {
					t.Errorf("%s%s: required property %v is not defined", p, ptr, r)
				}
			}
		})
	}
}

// walkObjects calls fn for every schema with "type": "object" and properties.
func walkObjects(node any, ptr string, fn func(string, map[string]any)) {
	switch n := node.(type) {
	case map[string]any:
		if n["type"] == "object" {
			if _, ok := n["properties"]; ok {
				fn(ptr, n)
			}
		}
		for k, v := range n {
			walkObjects(v, ptr+"/"+k, fn)
		}
	case []any:
		for _, v := range n {
			walkObjects(v, ptr, fn)
		}
	}
}

func TestBundleIsStandalone(t *testing.T) {
	arts, err := Artifacts(contracts.Schemas)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range arts {
		b, err := Bundle(contracts.Schemas, a.Path)
		if err != nil {
			t.Fatalf("%s: %v", a.Path, err)
		}
		var refs []string
		var doc any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		collectRefs(doc, &refs)
		for _, r := range refs {
			if !strings.HasPrefix(r, "#/definitions/") {
				t.Errorf("%s: bundled schema still has $ref %q", a.Path, r)
			}
		}
		// The bundle must compile on its own, under its own $id, and judge values like the original.
		fsys := fstest.MapFS{a.Path: &fstest.MapFile{Data: b}}
		v, err := New(fsys)
		if err != nil {
			t.Fatalf("%s: bundle does not compile alone: %v", a.Path, err)
		}
		if err := v.ValidateJSON(a.Path, []byte(`{"unexpected": true}`)); err == nil {
			t.Errorf("%s: bundle accepted an unexpected property", a.Path)
		}
	}
}

func collectRefs(node any, out *[]string) {
	switch n := node.(type) {
	case map[string]any:
		if r, ok := n["$ref"].(string); ok {
			*out = append(*out, r)
		}
		for _, v := range n {
			collectRefs(v, out)
		}
	case []any:
		for _, v := range n {
			collectRefs(v, out)
		}
	}
}

func TestBundleImportsTransitively(t *testing.T) {
	fsys := fstest.MapFS{
		"events/common.schema.json": {Data: []byte(`{"$id":"` + contracts.BaseURI + `events/common.schema.json",
			"definitions":{"A":{"type":"object","additionalProperties":false,"properties":{"b":{"$ref":"#/definitions/B"}}},
			"B":{"type":"string"}, "Unused":{"type":"string"}}}`)},
		"events/x/E.schema.json": {Data: []byte(`{"$id":"` + contracts.BaseURI + `events/x/E.schema.json",
			"type":"object","properties":{"a":{"$ref":"../common.schema.json#/definitions/A"}}}`)},
	}
	b, err := Bundle(fsys, "events/x/E.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Definitions map[string]any `json:"definitions"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Definitions["A"]; !ok {
		t.Error("A not imported")
	}
	if _, ok := doc.Definitions["B"]; !ok {
		t.Error("B, referenced from A, not imported")
	}
	if _, ok := doc.Definitions["Unused"]; ok {
		t.Error("Unused imported")
	}
}

func TestValidationErrorListsEveryProblem(t *testing.T) {
	err := Default().ValidateJSON("events/run/TaskFailed.schema.json", []byte(`{"taskId":"","attempts":0,"extra":1}`))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %v", err)
	}
	got := map[string]bool{}
	for _, p := range ve.Problems {
		got[p.Path] = true
	}
	for _, want := range []string{"", "/taskId", "/attempts"} {
		if !got[want] {
			t.Errorf("no problem reported at %q; got %v", want, ve.Problems)
		}
	}
}
