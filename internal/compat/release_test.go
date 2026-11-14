package compat

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/HumanOnLoop/contracts/internal/schema"
)

// TestBackwardCompatibleWithLastRelease fails when a schema in the working tree is not backward compatible with
// the same schema in the last release tag (v*), or when a released schema was removed. It is what stops an
// incompatible change from being committed; `make register` then has the registry check it again.
func TestBackwardCompatibleWithLastRelease(t *testing.T) {
	root := repoRoot(t)
	tag, err := git(root, "describe", "--tags", "--abbrev=0", "--match", "v[0-9]*", "HEAD")
	if err != nil {
		t.Skipf("no release tag to compare with (%v)", err)
	}
	t.Logf("comparing with %s", tag)

	released, err := gitFS(root, tag)
	if err != nil {
		t.Fatal(err)
	}
	current := os.DirFS(root)

	before, err := schema.Artifacts(released)
	if err != nil {
		t.Fatalf("%s: %v", tag, err)
	}
	now, err := schema.Artifacts(current)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]schema.Artifact{}
	for _, a := range now {
		byKey[a.Group+"/"+a.ID] = a
	}
	for _, old := range before {
		cur, ok := byKey[old.Group+"/"+old.ID]
		if !ok {
			t.Errorf("%s/%s (%s) was released in %s and is gone; released schemas are never removed",
				old.Group, old.ID, old.Path, tag)
			continue
		}
		oldDoc, err := schema.Bundle(released, old.Path)
		if err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
		newDoc, err := schema.Bundle(current, cur.Path)
		if err != nil {
			t.Fatal(err)
		}
		issues, err := Backward(oldDoc, newDoc)
		if err != nil {
			t.Fatalf("%s: %v", cur.Path, err)
		}
		for _, i := range issues {
			t.Errorf("%s is not backward compatible with %s: %s", cur.Path, tag, i)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := git(".", "rev-parse", "--show-toplevel")
	if err != nil {
		t.Skipf("not in a git checkout (%v)", err)
	}
	return root
}

// gitFS returns the schema files as they are at rev.
func gitFS(root, rev string) (fstest.MapFS, error) {
	list, err := git(root, "ls-tree", "-r", "--name-only", rev, "--", "events", "documents")
	if err != nil {
		return nil, err
	}
	fsys := fstest.MapFS{}
	for _, p := range strings.Split(list, "\n") {
		if !strings.HasSuffix(p, ".schema.json") {
			continue
		}
		out, err := exec.Command("git", "-C", root, "show", rev+":"+p).Output()
		if err != nil {
			return nil, err
		}
		fsys[p] = &fstest.MapFile{Data: out}
	}
	return fsys, nil
}

func git(dir string, args ...string) (string, error) {
	var stderr bytes.Buffer
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", &gitError{msg}
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

type gitError struct{ msg string }

func (e *gitError) Error() string { return e.msg }
