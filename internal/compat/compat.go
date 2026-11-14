// Package compat checks that a new version of a JSON Schema is backward compatible with the previous one: every
// value the old schema accepts, the new schema accepts too, so consumers on the new schema can read every event
// already on a topic.
//
// It follows the rules of the Apicurio Registry's BACKWARD check for draft-07 schemas, so a change that passes
// here also passes `make register`:
//
//   - With additionalProperties false (a closed content model), adding an optional property is compatible;
//     removing one is not. With an open content model it is the other way round.
//   - Adding a required property, removing an enum value, changing a type, and adding or changing a pattern,
//     format, const or a tighter length, range or item bound are incompatible.
//   - Adding an enum value, relaxing a bound and opening a closed content model are compatible.
//
// It adds one rule of HOTL's own: a required property is never dropped, because consumers built against the
// previous release rely on it being there.
//
// Anything it does not understand structurally (allOf, anyOf, oneOf, not, if/then/else, unknown keywords) must stay
// unchanged. Both schemas must be standalone (see schema.Bundle): only local #/definitions references resolve.
package compat

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Issue is one incompatible change.
type Issue struct {
	Path    string // JSON pointer into the new schema
	Message string
}

func (i Issue) String() string { return i.Path + ": " + i.Message }

// Backward returns every incompatible change from oldSchema to newSchema; none means newSchema is backward
// compatible.
func Backward(oldSchema, newSchema []byte) ([]Issue, error) {
	var o, n any
	if err := json.Unmarshal(oldSchema, &o); err != nil {
		return nil, fmt.Errorf("old schema: %w", err)
	}
	if err := json.Unmarshal(newSchema, &n); err != nil {
		return nil, fmt.Errorf("new schema: %w", err)
	}
	c := checker{oldRoot: o, newRoot: n}
	c.check(o, n, "", 0)
	return c.issues, nil
}

type checker struct {
	oldRoot, newRoot any
	issues           []Issue
}

func (c *checker) add(path, format string, args ...any) {
	if path == "" {
		path = "/"
	}
	c.issues = append(c.issues, Issue{Path: path, Message: fmt.Sprintf(format, args...)})
}

// Keywords that only document a schema; changing them never affects which values it accepts.
var annotations = map[string]bool{
	"$schema": true, "$id": true, "$comment": true, "title": true, "description": true, "examples": true,
	"default": true, "readOnly": true, "writeOnly": true, "definitions": true, "$ref": true,
}

// Keywords check compares structurally; any other keyword must be unchanged.
var understood = map[string]bool{
	"type": true, "enum": true, "const": true,
	"pattern": true, "format": true, "minLength": true, "maxLength": true,
	"minimum": true, "maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true, "multipleOf": true,
	"properties": true, "required": true, "additionalProperties": true, "minProperties": true, "maxProperties": true,
	"items": true, "minItems": true, "maxItems": true, "uniqueItems": true,
}

const maxDepth = 64

func (c *checker) check(o, n any, path string, depth int) {
	if depth > maxDepth {
		c.add(path, "schema nests too deeply to check")
		return
	}
	o, n = c.resolve(c.oldRoot, o, path), c.resolve(c.newRoot, n, path)
	ob, oIsBool := o.(bool)
	nb, nIsBool := n.(bool)
	if oIsBool || nIsBool {
		switch {
		case nIsBool && nb: // true accepts everything
		case oIsBool && nIsBool && ob == nb:
		case oIsBool && !ob: // old accepted nothing
		default:
			c.add(path, "boolean schema narrowed")
		}
		return
	}
	om, ok1 := o.(map[string]any)
	nm, ok2 := n.(map[string]any)
	if !ok1 || !ok2 {
		c.add(path, "schema is not an object")
		return
	}

	c.checkType(om, nm, path)
	c.checkEnum(om, nm, path)
	if nv, ok := nm["const"]; ok && !jsonEqual(om["const"], nv) {
		c.add(path, "const added or changed")
	}
	for _, k := range []string{"pattern", "format", "multipleOf"} {
		if nv, ok := nm[k]; ok && !jsonEqual(om[k], nv) {
			c.add(path, "%s added or changed", k)
		}
	}
	c.checkUpper(om, nm, path, "maxLength")
	c.checkLower(om, nm, path, "minLength")
	c.checkUpper(om, nm, path, "maximum")
	c.checkLower(om, nm, path, "minimum")
	c.checkUpper(om, nm, path, "exclusiveMaximum")
	c.checkLower(om, nm, path, "exclusiveMinimum")
	c.checkUpper(om, nm, path, "maxProperties")
	c.checkLower(om, nm, path, "minProperties")
	c.checkUpper(om, nm, path, "maxItems")
	c.checkLower(om, nm, path, "minItems")
	if nm["uniqueItems"] == true && om["uniqueItems"] != true {
		c.add(path, "uniqueItems added")
	}
	c.checkObject(om, nm, path, depth)
	c.checkItems(om, nm, path, depth)

	for _, k := range sortedKeys(om, nm) {
		if annotations[k] || understood[k] {
			continue
		}
		if !jsonEqual(om[k], nm[k]) {
			c.add(path, "%s changed; this checker only accepts it unchanged", k)
		}
	}
}

// resolve follows local $ref chains. In draft-07 a $ref replaces its whole schema object.
func (c *checker) resolve(root, s any, path string) any {
	for range maxDepth {
		m, ok := s.(map[string]any)
		if !ok {
			return s
		}
		ref, ok := m["$ref"].(string)
		if !ok {
			return s
		}
		name, ok := strings.CutPrefix(ref, "#/definitions/")
		if !ok {
			c.add(path, "$ref %q is not local; bundle the schema first", ref)
			return true
		}
		defs, _ := root.(map[string]any)["definitions"].(map[string]any)
		target, ok := defs[name]
		if !ok {
			c.add(path, "$ref %q does not resolve", ref)
			return true
		}
		s = target
	}
	c.add(path, "$ref chain too long")
	return true
}

func types(m map[string]any) (set map[string]bool, present bool) {
	switch t := m["type"].(type) {
	case string:
		return map[string]bool{t: true}, true
	case []any:
		set = map[string]bool{}
		for _, e := range t {
			if s, ok := e.(string); ok {
				set[s] = true
			}
		}
		return set, true
	}
	return nil, false
}

func (c *checker) checkType(om, nm map[string]any, path string) {
	nt, nok := types(nm)
	if !nok {
		return // no type constraint accepts any type
	}
	ot, ook := types(om)
	if !ook {
		c.add(path, "type added")
		return
	}
	for t := range ot {
		// integer values are numbers, so number accepts what integer did.
		if !nt[t] && !(t == "integer" && nt["number"]) {
			c.add(path, "type %s no longer accepted", t)
		}
	}
}

func (c *checker) checkEnum(om, nm map[string]any, path string) {
	ne, ok := nm["enum"].([]any)
	if !ok {
		return
	}
	oe, ok := om["enum"].([]any)
	if !ok {
		c.add(path, "enum added")
		return
	}
	for _, v := range oe {
		if !containsJSON(ne, v) {
			c.add(path, "enum value %v removed", v)
		}
	}
}

// checkUpper reports an upper bound that was added or lowered.
func (c *checker) checkUpper(om, nm map[string]any, path, k string) {
	nv, ok := number(nm[k])
	if !ok {
		return
	}
	if ov, ok := number(om[k]); !ok || nv < ov {
		c.add(path, "%s added or lowered", k)
	}
}

// checkLower reports a lower bound that was added or raised.
func (c *checker) checkLower(om, nm map[string]any, path, k string) {
	nv, ok := number(nm[k])
	if !ok {
		return
	}
	if ov, ok := number(om[k]); !ok || nv > ov {
		c.add(path, "%s added or raised", k)
	}
}

func (c *checker) checkObject(om, nm map[string]any, path string, depth int) {
	oAdd, nAdd := om["additionalProperties"], nm["additionalProperties"]
	oClosed, nClosed := oAdd == false, nAdd == false
	_, oSchema := oAdd.(map[string]any)
	_, nSchema := nAdd.(map[string]any)
	switch {
	case oSchema || nSchema:
		if !jsonEqual(oAdd, nAdd) {
			c.add(path, "additionalProperties schema changed; this checker only accepts it unchanged")
		}
	case !oClosed && nClosed:
		c.add(path, "additional properties are no longer allowed")
	}

	op, _ := om["properties"].(map[string]any)
	np, _ := nm["properties"].(map[string]any)
	for _, name := range sortedKeys(op, np) {
		ov, inOld := op[name]
		nv, inNew := np[name]
		p := path + "/properties/" + name
		switch {
		case inOld && inNew:
			c.check(ov, nv, p, depth+1)
		case inNew && !oClosed:
			// Old values could carry this property with any value; the new schema now constrains it.
			c.add(p, "property added to an open content model (set additionalProperties to false to add properties)")
		case inOld && nClosed:
			c.add(p, "property removed from a closed content model")
		}
	}

	or := stringSet(om["required"])
	nr := stringSet(nm["required"])
	for _, r := range sortedSet(nr) {
		if !or[r] {
			c.add(path+"/required", "new required property %s", r)
		}
	}
	for _, r := range sortedSet(or) {
		if !nr[r] {
			c.add(path+"/required", "required property %s dropped (HOTL rule: consumers on the previous release rely on it)", r)
		}
	}
}

func (c *checker) checkItems(om, nm map[string]any, path string, depth int) {
	oi, oHas := om["items"]
	ni, nHas := nm["items"]
	switch {
	case !nHas:
	case !oHas:
		c.add(path+"/items", "items constraint added")
	default:
		_, oList := oi.([]any)
		_, nList := ni.([]any)
		if oList || nList {
			if !jsonEqual(oi, ni) {
				c.add(path+"/items", "tuple items changed; this checker only accepts them unchanged")
			}
			return
		}
		c.check(oi, ni, path+"/items", depth+1)
	}
}

func number(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

func jsonEqual(a, b any) bool { return reflect.DeepEqual(a, b) }

func containsJSON(list []any, v any) bool {
	for _, e := range list {
		if jsonEqual(e, v) {
			return true
		}
	}
	return false
}

func stringSet(v any) map[string]bool {
	set := map[string]bool{}
	list, _ := v.([]any)
	for _, e := range list {
		if s, ok := e.(string); ok {
			set[s] = true
		}
	}
	return set
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(a, b map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range []map[string]any{a, b} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}
