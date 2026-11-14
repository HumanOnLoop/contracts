package compat

import (
	"encoding/json"
	"strings"
	"testing"
)

const open = `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object",
  "properties":{"a":{"type":"string"},"b":{"type":"string","enum":["x","y"]}},"required":["a"]}`

const closed = `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","additionalProperties":false,
  "properties":{"a":{"type":"string"},"b":{"type":"string","enum":["x","y"]}},"required":["a"]}`

const withDefs = `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","additionalProperties":false,
  "definitions":{"Id":{"type":"string"}},"properties":{"a":{"$ref":"#/definitions/Id"}},"required":["a"]}`

// edit applies fn to a decoded copy of schema and returns it re-encoded.
func edit(t *testing.T, schema string, fn func(m map[string]any)) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(schema), &m); err != nil {
		t.Fatal(err)
	}
	fn(m)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func props(m map[string]any) map[string]any { return m["properties"].(map[string]any) }

// The expectations in the first group are what Apicurio Registry 3.3.4's BACKWARD rule answered for the same
// change, measured against the registry on k3d-hotl. Keep them in step with the registry.
func TestBackwardMatchesRegistry(t *testing.T) {
	cases := []struct {
		name       string
		old        string
		change     func(m map[string]any)
		compatible bool
		want       string // substring of an issue, when incompatible
	}{
		{"identical", open, func(map[string]any) {}, true, ""},
		{"open: add optional property", open, func(m map[string]any) {
			props(m)["c"] = map[string]any{"type": "string"}
		}, false, "open content model"},
		{"open: add required property", open, func(m map[string]any) {
			props(m)["c"] = map[string]any{"type": "string"}
			m["required"] = []any{"a", "c"}
		}, false, "new required property c"},
		{"open: remove optional property", open, func(m map[string]any) { delete(props(m), "b") }, true, ""},
		{"closed: add optional property", closed, func(m map[string]any) {
			props(m)["c"] = map[string]any{"type": "string"}
		}, true, ""},
		{"closed: add required property", closed, func(m map[string]any) {
			props(m)["c"] = map[string]any{"type": "string"}
			m["required"] = []any{"a", "c"}
		}, false, "new required property c"},
		{"closed: remove optional property", closed, func(m map[string]any) { delete(props(m), "b") }, false, "removed from a closed"},
		{"add enum value", closed, func(m map[string]any) {
			props(m)["b"].(map[string]any)["enum"] = []any{"x", "y", "z"}
		}, true, ""},
		{"remove enum value", closed, func(m map[string]any) {
			props(m)["b"].(map[string]any)["enum"] = []any{"x"}
		}, false, "enum value y removed"},
		{"change type", closed, func(m map[string]any) {
			props(m)["a"].(map[string]any)["type"] = "integer"
		}, false, "type string no longer accepted"},
		{"add pattern", closed, func(m map[string]any) {
			props(m)["a"].(map[string]any)["pattern"] = "^A"
		}, false, "pattern added"},
		{"add maxLength", closed, func(m map[string]any) {
			props(m)["a"].(map[string]any)["maxLength"] = 5.0
		}, false, "maxLength added"},
		{"open to closed", open, func(m map[string]any) { m["additionalProperties"] = false }, false, "no longer allowed"},
		{"closed to open", closed, func(m map[string]any) { delete(m, "additionalProperties") }, true, ""},
		{"type change behind a $ref", withDefs, func(m map[string]any) {
			m["definitions"].(map[string]any)["Id"].(map[string]any)["type"] = "integer"
		}, false, "type string no longer accepted"},
		{"pattern added behind a $ref", withDefs, func(m map[string]any) {
			m["definitions"].(map[string]any)["Id"].(map[string]any)["pattern"] = "^X"
		}, false, "pattern added"},
		{"open: nested object with a required property", open, func(m map[string]any) {
			props(m)["n"] = map[string]any{"type": "object", "required": []any{"q"},
				"properties": map[string]any{"q": map[string]any{"type": "string"}}}
		}, false, "open content model"},

		// HOTL's own rule, stricter than the registry, which accepts this change.
		{"drop a required property", closed, func(m map[string]any) { m["required"] = []any{} }, false, "required property a dropped"},

		// Further cases the checker must get right.
		{"description only", closed, func(m map[string]any) {
			props(m)["a"].(map[string]any)["description"] = "now documented"
			m["title"] = "Renamed"
		}, true, ""},
		{"relax maxLength", edit(t, closed, func(m map[string]any) { props(m)["a"].(map[string]any)["maxLength"] = 5.0 }),
			func(m map[string]any) { props(m)["a"].(map[string]any)["maxLength"] = 9.0 }, true, ""},
		{"raise minimum", edit(t, closed, func(m map[string]any) { props(m)["a"] = map[string]any{"type": "integer", "minimum": 0.0} }),
			func(m map[string]any) { props(m)["a"].(map[string]any)["minimum"] = 1.0 }, false, "minimum added or raised"},
		{"widen integer to number", edit(t, closed, func(m map[string]any) { props(m)["a"] = map[string]any{"type": "integer"} }),
			func(m map[string]any) { props(m)["a"].(map[string]any)["type"] = "number" }, true, ""},
		{"add uniqueItems", edit(t, closed, func(m map[string]any) {
			props(m)["b"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		}),
			func(m map[string]any) { props(m)["b"].(map[string]any)["uniqueItems"] = true }, false, "uniqueItems added"},
		{"narrow array items", edit(t, closed, func(m map[string]any) {
			props(m)["b"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		}),
			func(m map[string]any) { props(m)["b"].(map[string]any)["items"].(map[string]any)["minLength"] = 1.0 }, false, "minLength added"},
		{"change if/then", edit(t, closed, func(m map[string]any) { m["if"] = map[string]any{"required": []any{"b"}} }),
			func(m map[string]any) { m["if"] = map[string]any{"required": []any{"a"}} }, false, "if changed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues, err := Backward([]byte(tc.old), []byte(edit(t, tc.old, tc.change)))
			if err != nil {
				t.Fatal(err)
			}
			if tc.compatible {
				if len(issues) > 0 {
					t.Fatalf("want compatible, got %v", issues)
				}
				return
			}
			for _, i := range issues {
				if strings.Contains(i.Message, tc.want) {
					return
				}
			}
			t.Fatalf("want an issue containing %q, got %v", tc.want, issues)
		})
	}
}
