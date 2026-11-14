package documents

import (
	"path"
	"strconv"
	"strings"
)

const ruleSchema = "documents/rule.schema.json"

// ParseRule reads a rule definition: its front matter (the rule envelope), validated against the rule schema, and
// the Markdown body after it, which states the rule.
func ParseRule(md []byte) (Rule, []byte, error) {
	var r Rule
	body, err := parse(md, ruleSchema, &r)
	return r, body, err
}

// ValidateRule checks rule front matter built in code.
func ValidateRule(r Rule) error { return validate(ruleSchema, r) }

// Number returns the rule's version as a number; 0 when it is not one (the schema allows only positive numbers).
func (r Rule) Number() int {
	n, err := strconv.Atoi(r.Version)
	if err != nil {
		return 0
	}
	return n
}

// State is the rule's lifecycle state; active when the front matter leaves it out.
func (r Rule) State() RuleStatus {
	if r.Status == nil {
		return RuleStatusActive
	}
	return *r.Status
}

// InScope reports whether a change to repo that touches files (slash-separated, relative to the repo root) is in
// the rule's scope: the repo matches, and at least one file matches both the languages and the paths. A scope
// field left out matches everything; a change with no files is in scope only if the rule names no languages or
// paths.
func (r Rule) InScope(repo string, files []string) bool {
	s := r.Scope
	if len(s.Repos) > 0 && !matchAny(s.Repos, func(p string) bool { return p == "*" || p == repo }) {
		return false
	}
	if len(s.Languages) == 0 && len(s.Paths) == 0 {
		return true
	}
	for _, f := range files {
		lang := Language(f)
		if len(s.Languages) > 0 && !matchAny(s.Languages, func(l string) bool { return l == lang }) {
			continue
		}
		if len(s.Paths) > 0 && !matchAny(s.Paths, func(p string) bool { return MatchPath(p, f) }) {
			continue
		}
		return true
	}
	return false
}

func matchAny(list []string, f func(string) bool) bool {
	for _, s := range list {
		if f(s) {
			return true
		}
	}
	return false
}

// languages maps file extensions and names to the language rule scopes use.
var languages = map[string]string{
	".go": "go", "go.mod": "go", "go.sum": "go", "go.work": "go",
	".yaml": "yaml", ".yml": "yaml", ".json": "json", ".md": "markdown", ".rego": "rego",
	".sh": "shell", ".bash": "shell", ".sql": "sql", ".proto": "proto",
	".ts": "typescript", ".tsx": "typescript", ".js": "javascript", ".jsx": "javascript", ".py": "python",
	"Dockerfile": "dockerfile", "Makefile": "make",
}

// Language returns the language of a file by its name or extension, or "" when it is not known.
func Language(file string) string {
	base := path.Base(file)
	if l, ok := languages[base]; ok {
		return l
	}
	if strings.HasPrefix(base, "Dockerfile.") {
		return "dockerfile"
	}
	return languages[path.Ext(base)]
}

// MatchPath reports whether file matches pattern. Both are slash-separated and relative to the repo root.
// Segments match as in path.Match, and a ** segment matches any number of directories, none included.
func MatchPath(pattern, file string) bool {
	return matchSegments(strings.Split(strings.Trim(pattern, "/"), "/"), strings.Split(strings.Trim(file, "/"), "/"))
}

func matchSegments(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(name); i++ {
				if matchSegments(pat[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, err := path.Match(pat[0], name[0]); err != nil || !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}
