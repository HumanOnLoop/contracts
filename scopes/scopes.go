// Package scopes holds what HOTL decides about a run's scope (ADR 0018) in plain code, so every service decides it
// the same way: which boundary rules a scope crosses, whether a new repo's name is acceptable, and what the run does
// with a proposal (confirm it, ask the developer, or ask the architect).
package scopes

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/HumanOnLoop/contracts/documents"
	"github.com/HumanOnLoop/contracts/events"
)

// DefaultThreshold is the confidence below which a repo of a proposed scope needs the developer.
const DefaultThreshold = 0.7

var repoName = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// ValidRepoName reports whether name can be a new repo's name: lower case letters and digits in hyphen-separated
// words, at most 64 characters.
func ValidRepoName(name string) bool { return len(name) <= 64 && repoName.MatchString(name) }

// Repos lists a scope's existing repos, then its new ones, each once.
func Repos(s events.Scope) []events.Repo {
	var out []events.Repo
	add := func(r events.Repo) {
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	for _, r := range s.Repos {
		add(r.Repo)
	}
	for _, n := range s.NewRepos {
		add(events.Repo(n.Name))
	}
	return out
}

// Next is what a run does with a proposed scope, and why when it asks someone:
//   - architect: the scope proposes a new repo or crosses a protected boundary;
//   - developer: a repo's confidence is below threshold, or the scope names no repo at all;
//   - confirm: none of these.
func Next(s events.Scope, threshold float64) (events.ScopeProposedNext, []string) {
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	var architect, developer []string
	for _, n := range s.NewRepos {
		architect = append(architect, fmt.Sprintf("the scope needs a new repo, %s: %s", n.Name, n.Why))
	}
	for _, b := range s.Boundaries {
		architect = append(architect, fmt.Sprintf("the scope crosses the boundary protected by rule %s: %s", b.Rule, b.Reason))
	}
	if len(architect) > 0 {
		return events.ScopeProposedNextArchitect, architect
	}
	if len(s.Repos) == 0 {
		return events.ScopeProposedNextDeveloper, []string{"the scope names no repo"}
	}
	for _, r := range s.Repos {
		if float64(r.Confidence) < threshold {
			developer = append(developer, fmt.Sprintf("the scoper is %.0f%% sure of %s, below the %.0f%% the run confirms on its own",
				float64(r.Confidence)*100, r.Repo, threshold*100))
		}
	}
	if len(developer) > 0 {
		return events.ScopeProposedNextDeveloper, developer
	}
	return events.ScopeProposedNextConfirm, nil
}

// DependsOn returns the repos a repo depends on, by its approved profile (documents.RepoProfile.DependsOn).
type DependsOn func(repo events.Repo) []events.Repo

// Boundaries returns the boundaries a scope crosses: one entry per active boundary rule whose condition the scope
// meets. Rules of another kind, and boundary rules that are proposed, shadow or retired, are ignored; so is a
// boundary rule without a condition, which would otherwise match every scope.
func Boundaries(s events.Scope, rules []documents.Rule, dependsOn DependsOn) []events.ScopeBoundary {
	repos := Repos(s)
	out := []events.ScopeBoundary{}
	for _, r := range rules {
		if r.Kind == nil || *r.Kind != documents.RuleKindBoundary || r.State() != documents.RuleStatusActive || r.Boundary == nil {
			continue
		}
		if met, which, why := Meets(*r.Boundary, repos, len(s.NewRepos) > 0, dependsOn); met {
			title := r.Title
			out = append(out, events.ScopeBoundary{Rule: events.RuleID(r.ID), Title: &title, Repos: which, Reason: why})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rule < out[j].Rule })
	return out
}

// Meets reports whether a scope with these repos (new ones included) meets a boundary condition: every part the
// condition gives holds. It also returns the repos that meet it and how, for the architect. A condition with no
// part is never met.
func Meets(b documents.RuleBoundary, repos []events.Repo, newRepo bool, dependsOn DependsOn) (bool, []events.Repo, string) {
	var which []events.Repo
	var why []string
	parts := 0
	add := func(rs ...events.Repo) {
		for _, r := range rs {
			if !slices.Contains(which, r) {
				which = append(which, r)
			}
		}
	}
	if len(b.Repos) > 0 {
		parts++
		for _, want := range b.Repos {
			if !slices.Contains(repos, events.Repo(want)) {
				return false, nil, ""
			}
			add(events.Repo(want))
		}
		why = append(why, "it includes "+strings.Join(b.Repos, ", "))
	}
	if len(b.AnyRepos) > 0 {
		parts++
		var hit []string
		for _, want := range b.AnyRepos {
			if slices.Contains(repos, events.Repo(want)) {
				hit = append(hit, want)
				add(events.Repo(want))
			}
		}
		if len(hit) == 0 {
			return false, nil, ""
		}
		why = append(why, "it includes "+strings.Join(hit, ", "))
	}
	if b.Dependents != nil {
		parts++
		var consumers []string
		for _, r := range repos {
			if slices.Contains(b.Dependents.Of, string(r)) || dependsOn == nil {
				continue
			}
			for _, d := range dependsOn(r) {
				if slices.Contains(b.Dependents.Of, string(d)) {
					consumers = append(consumers, string(r))
					add(r)
					break
				}
			}
		}
		atLeast, err := strconv.Atoi(b.Dependents.AtLeast)
		if err != nil || atLeast < 1 || len(consumers) < atLeast {
			return false, nil, ""
		}
		why = append(why, fmt.Sprintf("%d of its repos depend on %s (%s), at least %d", len(consumers),
			strings.Join(b.Dependents.Of, " or "), strings.Join(consumers, ", "), atLeast))
	}
	if b.NewRepo != nil && *b.NewRepo == documents.RuleBoundaryNewRepoTrue {
		parts++
		if !newRepo {
			return false, nil, ""
		}
		why = append(why, "it proposes a new repo")
	}
	if parts == 0 {
		return false, nil, ""
	}
	return true, which, strings.Join(why, ", and ")
}

// Normalize returns s with its arrays never nil, as the events' required arrays must be sent.
func Normalize(s events.Scope) events.Scope {
	if s.Repos == nil {
		s.Repos = []events.ScopeRepo{}
	}
	for i := range s.Repos {
		if s.Repos[i].Evidence == nil {
			s.Repos[i].Evidence = []events.ScopeEvidence{}
		}
	}
	if s.NewRepos == nil {
		s.NewRepos = []events.ScopeNewRepo{}
	}
	if s.Boundaries == nil {
		s.Boundaries = []events.ScopeBoundary{}
	}
	if s.Hints == nil {
		s.Hints = []events.ScopeHint{}
	}
	return s
}
