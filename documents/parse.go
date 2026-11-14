// Package documents parses and validates the front matter of HOTL's Markdown documents: intent packages and review
// briefs, per the templates in docs/design. The Go types are generated from documents/*.schema.json.
//
// Front matter values are read as text, the way a person writes them: `due: 2026-12-15` is the string
// "2026-12-15", not a timestamp, and a key with no value (`due:`) counts as absent.
package documents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"go.yaml.in/yaml/v3"

	"github.com/HumanOnLoop/contracts/internal/schema"
)

// ValidationError lists every way a front matter fails its schema. Problems[i].Path is a JSON pointer such as
// /title; empty for the front matter as a whole.
type ValidationError = schema.ValidationError

const (
	intentPackageSchema = "documents/intent-package.schema.json"
	reviewBriefSchema   = "documents/review-brief.schema.json"
)

// ErrNoFrontMatter reports a document that does not start with a front matter block between --- lines.
var ErrNoFrontMatter = errors.New("document does not start with front matter between --- lines")

// ParseIntentPackage reads an intent package: its front matter, validated against the intent package schema, and
// the Markdown body after it.
func ParseIntentPackage(md []byte) (IntentPackage, []byte, error) {
	var ip IntentPackage
	body, err := parse(md, intentPackageSchema, &ip)
	return ip, body, err
}

// ParseReviewBrief reads a review brief: its front matter, validated against the review brief schema, and the
// Markdown body after it.
func ParseReviewBrief(md []byte) (ReviewBrief, []byte, error) {
	var rb ReviewBrief
	body, err := parse(md, reviewBriefSchema, &rb)
	return rb, body, err
}

// ValidateIntentPackage checks intent package front matter built in code, for example after intake assigns the id.
func ValidateIntentPackage(ip IntentPackage) error { return validate(intentPackageSchema, ip) }

// ValidateReviewBrief checks review brief front matter built in code, for example after the review service assigns
// the id and resolves the recipient.
func ValidateReviewBrief(rb ReviewBrief) error { return validate(reviewBriefSchema, rb) }

func validate(schemaPath string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return schema.Default().ValidateJSON(schemaPath, raw)
}

func parse(md []byte, schemaPath string, into any) ([]byte, error) {
	front, body, err := FrontMatter(md)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(front)
	if err != nil {
		return nil, err
	}
	if err := schema.Default().ValidateJSON(schemaPath, raw); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return nil, err
	}
	return body, nil
}

// FrontMatter splits a Markdown document into its front matter, decoded with every value as text, and its body.
func FrontMatter(md []byte) (map[string]any, []byte, error) {
	md = bytes.TrimPrefix(md, []byte("\uFEFF"))
	md = bytes.ReplaceAll(md, []byte("\r\n"), []byte("\n"))
	rest, ok := bytes.CutPrefix(md, []byte("---\n"))
	if !ok {
		return nil, nil, ErrNoFrontMatter
	}
	var yamlPart, body []byte
	switch {
	case bytes.HasPrefix(rest, []byte("---\n")) || bytes.Equal(rest, []byte("---")):
		body = bytes.TrimPrefix(bytes.TrimPrefix(rest, []byte("---")), []byte("\n"))
	default:
		i := bytes.Index(rest, []byte("\n---\n"))
		if i < 0 {
			if !bytes.HasSuffix(rest, []byte("\n---")) {
				return nil, nil, fmt.Errorf("front matter has no closing --- line")
			}
			i = len(rest) - len("\n---")
			yamlPart, body = rest[:i], nil
		} else {
			yamlPart, body = rest[:i], rest[i+len("\n---\n"):]
		}
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(yamlPart, &doc); err != nil {
		return nil, nil, fmt.Errorf("front matter: %w", err)
	}
	if len(doc.Content) == 0 {
		return map[string]any{}, body, nil
	}
	v, err := textValue(doc.Content[0], 0)
	if err != nil {
		return nil, nil, fmt.Errorf("front matter: %w", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("front matter: line %d: want key: value pairs", doc.Content[0].Line)
	}
	return m, body, nil
}

// maxDepth bounds nesting in front matter; the templates need two levels.
const maxDepth = 16

// textValue converts a YAML node to JSON-compatible values with every scalar as its literal text, except null.
// Aliases are refused: front matter never needs them, and expanding them lets a few lines grow without bound.
func textValue(n *yaml.Node, depth int) (any, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("line %d: nested more than %d levels deep", n.Line, maxDepth)
	}
	switch n.Kind {
	case yaml.AliasNode:
		return nil, fmt.Errorf("line %d: YAML aliases (*name) are not allowed in front matter", n.Line)
	case yaml.ScalarNode:
		if n.ShortTag() == "!!null" {
			return nil, nil
		}
		return n.Value, nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := textValue(c, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.MappingNode:
		out := make(map[string]any, len(n.Content)/2)
		seen := make(map[string]bool, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, vn := n.Content[i], n.Content[i+1]
			if k.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("line %d: keys must be plain text", k.Line)
			}
			if seen[k.Value] {
				return nil, fmt.Errorf("line %d: %s is set twice", k.Line, k.Value)
			}
			seen[k.Value] = true
			v, err := textValue(vn, depth+1)
			if err != nil {
				return nil, err
			}
			if v != nil { // an empty value counts as absent
				out[k.Value] = v
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("line %d: unsupported YAML", n.Line)
}
