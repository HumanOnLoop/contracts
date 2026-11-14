package schema

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"reflect"
	"sort"
	"strings"

	"github.com/osbe/contracts"
)

// Artifact is a schema as registered in the schema registry.
type Artifact struct {
	Path  string // path in this repository
	Group string // registry group: hotl for the envelope, hotl.<topic> for events, hotl.documents for documents
	ID    string // registry artifact id: the schema's name, for example RunStarted or IntentPackage
}

// Artifacts lists the schemas that are registered: the envelope, every event and every document. Shared
// definitions (events/common.schema.json) are not registered on their own; Bundle inlines them.
func Artifacts(fsys fs.FS) ([]Artifact, error) {
	paths, err := Paths(fsys)
	if err != nil {
		return nil, err
	}
	var out []Artifact
	for _, p := range paths {
		dir, file := path.Split(p)
		dir = strings.TrimSuffix(dir, "/")
		var group string
		switch {
		case file == "common.schema.json":
			continue
		case dir == "events":
			group = "hotl"
		case strings.HasPrefix(dir, "events/"):
			group = "hotl." + strings.TrimPrefix(dir, "events/")
		case dir == "documents":
			group = "hotl.documents"
		default:
			return nil, fmt.Errorf("%s: no registry group for this directory", p)
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, err
		}
		var head struct {
			Title string `json:"title"`
		}
		if err := json.Unmarshal(b, &head); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if head.Title == "" {
			return nil, fmt.Errorf("%s: no title; the title is the registry artifact id", p)
		}
		out = append(out, Artifact{Path: p, Group: group, ID: head.Title})
	}
	return out, nil
}

// Bundle returns the schema at schemaPath as one standalone document: every definition it references in another
// schema file is copied under its own definitions and the reference rewritten to point there. The registry stores
// and compares standalone schemas, so this is what gets registered and what compatibility is checked on.
func Bundle(fsys fs.FS, schemaPath string) ([]byte, error) {
	b := bundler{root: schemaPath, fsys: fsys, docs: map[string]map[string]any{}, defs: map[string]any{}}
	root, err := b.load(schemaPath)
	if err != nil {
		return nil, err
	}
	out := deepCopy(root).(map[string]any)
	if err := b.rewrite(out, schemaPath); err != nil {
		return nil, fmt.Errorf("%s: %w", schemaPath, err)
	}
	if len(b.defs) > 0 {
		own, _ := out["definitions"].(map[string]any)
		if own == nil {
			own = map[string]any{}
		}
		names := make([]string, 0, len(b.defs))
		for n := range b.defs {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if prev, ok := own[n]; ok && !reflect.DeepEqual(prev, b.defs[n]) {
				return nil, fmt.Errorf("%s: definition %q clashes with an imported one", schemaPath, n)
			}
			own[n] = b.defs[n]
		}
		out["definitions"] = own
	}
	return json.MarshalIndent(out, "", "  ")
}

type bundler struct {
	root string // path of the file being bundled
	fsys fs.FS
	docs map[string]map[string]any // loaded schema files by path
	defs map[string]any            // imported definitions by name, already rewritten
}

func (b *bundler) load(p string) (map[string]any, error) {
	if d, ok := b.docs[p]; ok {
		return d, nil
	}
	raw, err := fs.ReadFile(b.fsys, p)
	if err != nil {
		return nil, err
	}
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	b.docs[p] = d
	return d, nil
}

// rewrite walks node, which lives in the file at docPath, and rewrites every $ref to a local one, importing
// definitions from other files as it goes.
func (b *bundler) rewrite(node any, docPath string) error {
	switch n := node.(type) {
	case []any:
		for _, v := range n {
			if err := b.rewrite(v, docPath); err != nil {
				return err
			}
		}
	case map[string]any:
		for k, v := range n {
			if k == "$ref" {
				ref, ok := v.(string)
				if !ok {
					return fmt.Errorf("$ref is not a string")
				}
				local, err := b.importRef(ref, docPath)
				if err != nil {
					return err
				}
				n[k] = local
				continue
			}
			if err := b.rewrite(v, docPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// importRef resolves ref against the file at docPath. A reference within the root file stays as it is; a
// reference to a definition in another file imports that definition and returns its local reference.
func (b *bundler) importRef(ref, docPath string) (string, error) {
	base, err := url.Parse(contracts.BaseURI + docPath)
	if err != nil {
		return "", err
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("$ref %q: %w", ref, err)
	}
	abs := base.ResolveReference(r)
	frag := abs.Fragment
	abs.Fragment = ""
	target, ok := strings.CutPrefix(abs.String(), contracts.BaseURI)
	if !ok {
		return "", fmt.Errorf("$ref %q points outside this repository", ref)
	}
	if !strings.HasPrefix(frag, "/definitions/") || strings.Count(frag, "/") != 2 {
		if target == b.root && docPath == b.root {
			return ref, nil // a reference within the root file to something other than a definition
		}
		return "", fmt.Errorf("$ref %q: only references to <file>#/definitions/<name> are supported", ref)
	}
	name := strings.TrimPrefix(frag, "/definitions/")
	if target == b.root {
		return "#/definitions/" + name, nil
	}
	if _, done := b.defs[name]; done {
		return "#/definitions/" + name, nil
	}
	doc, err := b.load(target)
	if err != nil {
		return "", fmt.Errorf("$ref %q: %w", ref, err)
	}
	defs, _ := doc["definitions"].(map[string]any)
	def, ok := defs[name]
	if !ok {
		return "", fmt.Errorf("$ref %q: %s has no definition %q", ref, target, name)
	}
	cp := deepCopy(def)
	b.defs[name] = cp // set before recursing so cycles terminate
	if err := b.rewrite(cp, target); err != nil {
		return "", err
	}
	return "#/definitions/" + name, nil
}

func deepCopy(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = deepCopy(e)
		}
		return m
	case []any:
		s := make([]any, len(x))
		for i, e := range x {
			s[i] = deepCopy(e)
		}
		return s
	default:
		return v
	}
}
