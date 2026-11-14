package agents

import (
	"errors"
	"fmt"
	"regexp"

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
