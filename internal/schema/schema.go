// Package schema compiles, validates and bundles the contracts schemas. It is shared by the events and documents
// packages and by the registry tooling.
package schema

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/HumanOnLoop/contracts"
)

// Problem is one way a value fails its schema.
type Problem struct {
	// Path is the JSON pointer of the offending value, for example /data/runid; empty for the value itself.
	Path string `json:"path"`
	// Message says what is wrong.
	Message string `json:"message"`
}

func (p Problem) String() string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

// ValidationError lists every problem a value has against a schema.
type ValidationError struct {
	// Schema is the path of the schema in this repository, for example events/run/RunStarted.schema.json.
	Schema   string
	Problems []Problem
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		parts[i] = p.String()
	}
	return fmt.Sprintf("does not match %s: %s", e.Schema, strings.Join(parts, "; "))
}

// Validator validates values against compiled schemas.
type Validator struct {
	schemas map[string]*jsonschema.Schema // by path in the repository
}

// New compiles every *.schema.json file in fsys. Paths in fsys are paths in this repository; each schema's $id
// must be contracts.BaseURI followed by its path, so references between files resolve.
func New(fsys fs.FS) (*Validator, error) {
	paths, err := Paths(fsys)
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft7)
	c.AssertFormat()
	for _, p := range paths {
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, err
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if id, _ := doc.(map[string]any)["$id"].(string); id != contracts.BaseURI+p {
			return nil, fmt.Errorf("%s: $id is %q, want %q", p, id, contracts.BaseURI+p)
		}
		if err := c.AddResource(contracts.BaseURI+p, doc); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	v := &Validator{schemas: make(map[string]*jsonschema.Schema, len(paths))}
	for _, p := range paths {
		s, err := c.Compile(contracts.BaseURI + p)
		if err != nil {
			return nil, fmt.Errorf("compile %s: %w", p, err)
		}
		v.schemas[p] = s
	}
	return v, nil
}

var defaultValidator = sync.OnceValues(func() (*Validator, error) { return New(contracts.Schemas) })

// Default returns the validator for the schemas embedded in this module. It panics if they do not compile, which
// the tests rule out.
func Default() *Validator {
	v, err := defaultValidator()
	if err != nil {
		panic("contracts: embedded schemas do not compile: " + err.Error())
	}
	return v
}

// ValidateJSON validates a JSON document against the schema at path.
func (v *Validator) ValidateJSON(schemaPath string, doc []byte) error {
	val, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return v.Validate(schemaPath, val)
}

// Validate validates a decoded JSON value (maps, slices, strings, numbers, bools and nil) against the schema at
// path. It returns a *ValidationError when the value does not match.
func (v *Validator) Validate(schemaPath string, val any) error {
	s, ok := v.schemas[schemaPath]
	if !ok {
		return fmt.Errorf("no schema %s", schemaPath)
	}
	err := s.Validate(val)
	var ve *jsonschema.ValidationError
	if errors.As(err, &ve) {
		return &ValidationError{Schema: schemaPath, Problems: problems(ve)}
	}
	return err
}

// printer renders validation messages the way the validator's own Error method does.
var printer = message.NewPrinter(language.English)

// problems lists the leaf problems of a validation error, sorted by path. Leaves are the failed keywords
// themselves (minLength, required, ...), not the $ref or allOf that led to them.
func problems(ve *jsonschema.ValidationError) []Problem {
	var out []Problem
	seen := map[Problem]bool{}
	var walk func(*jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) > 0 {
			for _, c := range e.Causes {
				walk(c)
			}
			return
		}
		p := Problem{Path: pointer(e.InstanceLocation), Message: e.ErrorKind.LocalizedString(printer)}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	walk(ve)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func pointer(tokens []string) string {
	esc := strings.NewReplacer("~", "~0", "/", "~1")
	var sb strings.Builder
	for _, t := range tokens {
		sb.WriteByte('/')
		sb.WriteString(esc.Replace(t))
	}
	return sb.String()
}

// Paths lists every *.schema.json file in fsys, sorted.
func Paths(fsys fs.FS) ([]string, error) {
	var out []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".schema.json") && !strings.Contains(p, "testdata") {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// Name returns the schema's name: its file name without .schema.json.
func Name(schemaPath string) string {
	return strings.TrimSuffix(path.Base(schemaPath), ".schema.json")
}
