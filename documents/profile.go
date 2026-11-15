package documents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
)

const repoProfileSchema = "documents/repo-profile.schema.json"

// ParseRepoProfile reads a repo profile from JSON, validated against the repo profile schema. Unknown fields are
// refused, as the schema's closed content model says.
func ParseRepoProfile(raw []byte) (RepoProfile, error) {
	var p RepoProfile
	raw = bytes.TrimPrefix(raw, []byte("\uFEFF"))
	if err := schemaValidateJSON(repoProfileSchema, raw); err != nil {
		return p, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, fmt.Errorf("repo profile: %w", err)
	}
	return p, nil
}

// ValidateRepoProfile checks a repo profile built in code.
func ValidateRepoProfile(p RepoProfile) error { return validate(repoProfileSchema, p) }

// Number is the profile's version; 0 when it has none yet (a proposal).
func (p RepoProfile) Number() int {
	if p.Version == nil {
		return 0
	}
	return *p.Version
}

// ProfileCommand is one command of the verified run.
type ProfileCommand struct {
	// Name is format, lint, build or test.
	Name    string
	Command string
}

// Checks are the verified run's commands in the order they run: format, lint, build, test. Commands the profile
// leaves out are not listed.
func (p RepoProfile) Checks() []ProfileCommand {
	var out []ProfileCommand
	for _, c := range []struct {
		name string
		cmd  *string
	}{{"format", p.Commands.Format}, {"lint", p.Commands.Lint}, {"build", p.Commands.Build}, {"test", &p.Commands.Test}} {
		if c.cmd != nil && *c.cmd != "" {
			out = append(out, ProfileCommand{Name: c.name, Command: *c.cmd})
		}
	}
	return out
}

// IsTestFile reports whether a slash-separated path relative to the repo root is a test file per testFiles.
func (p RepoProfile) IsTestFile(file string) bool {
	return slices.ContainsFunc(p.TestFiles, func(g string) bool { return MatchPath(g, file) })
}

// HasLanguage reports whether the profile lists the language.
func (p RepoProfile) HasLanguage(lang string) bool { return slices.Contains(p.Languages, lang) }

// MergeStrategy is the profile's merge strategy as GitHub names merge methods: squash, merge or rebase.
func (p RepoProfile) MergeStrategy() string { return string(p.Merge.Strategy) }
