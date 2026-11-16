package agents

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/HumanOnLoop/contracts/events"
)

// ValidateTasks checks that a plan's tasks have unique ids and known roles, and that their dependencies exist and
// form no cycle. The run workflow rejects a plan that fails it, so a planner checks its own plan with it first.
func ValidateTasks(tasks []TaskSpec) error {
	if len(tasks) == 0 {
		return errors.New("the plan has no tasks")
	}
	idx := map[events.TaskID]int{}
	for i, t := range tasks {
		if t.ID == "" {
			return fmt.Errorf("task %d has no id", i+1)
		}
		if _, dup := idx[t.ID]; dup {
			return fmt.Errorf("task id %s is used twice", t.ID)
		}
		if t.Role != events.AgentRoleImplementer && t.Role != events.AgentRoleTester {
			return fmt.Errorf("task %s has role %q; plan tasks are for an implementer or a tester", t.ID, t.Role)
		}
		if t.Criteria != nil {
			seen := map[int]bool{}
			for _, c := range *t.Criteria {
				if c < 1 || seen[c] {
					return fmt.Errorf("task %s names criterion %d; criteria are numbered from 1, each once", t.ID, c)
				}
				seen[c] = true
			}
		}
		idx[t.ID] = i
	}
	for _, t := range tasks {
		for _, d := range t.DependsOn {
			if _, ok := idx[d]; !ok {
				return fmt.Errorf("task %s depends on unknown task %s", t.ID, d)
			}
		}
	}
	// Depth-first search for a cycle, in plan order so the error is stable.
	const (
		unseen = iota
		open
		closed
	)
	mark := make([]int, len(tasks))
	var visit func(i int) error
	visit = func(i int) error {
		switch mark[i] {
		case open:
			return fmt.Errorf("task %s is part of a dependency cycle", tasks[i].ID)
		case closed:
			return nil
		}
		mark[i] = open
		for _, d := range tasks[i].DependsOn {
			if err := visit(idx[d]); err != nil {
				return err
			}
		}
		mark[i] = closed
		return nil
	}
	for i := range tasks {
		if err := visit(i); err != nil {
			return err
		}
	}
	return nil
}

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// ValidCommit reports whether c is a full git commit id, as the events' Commit format requires.
func ValidCommit(c events.Commit) bool { return commitPattern.MatchString(string(c)) }

// ValidateCoverage checks the plan's tasks against an intent with n acceptance criteria (ADR 0022): a task names
// only criteria the intent has, and every criterion is covered by at least one task. A plan from before v0.14.0, in
// which no task names criteria, covers every one; a plan that names them for some tasks but not others is refused.
func ValidateCoverage(tasks []TaskSpec, n int) error {
	legacy := 0
	for _, t := range tasks {
		if t.Criteria == nil {
			legacy++
		}
	}
	switch {
	case legacy == len(tasks):
		return nil
	case legacy > 0:
		return errors.New("some tasks name their criteria and some do not; give every task its criteria, an empty list for none")
	}
	covered := make([]bool, n+1)
	for _, t := range tasks {
		for _, c := range *t.Criteria {
			if c > n {
				return fmt.Errorf("task %s names criterion %d; the intent has %d", t.ID, c, n)
			}
			covered[c] = true
		}
	}
	var missing []string
	for c := 1; c <= n; c++ {
		if !covered[c] {
			missing = append(missing, fmt.Sprint(c))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("no task covers criterion %s; give each criterion to the task whose change makes it hold",
			strings.Join(missing, ", "))
	}
	return nil
}
