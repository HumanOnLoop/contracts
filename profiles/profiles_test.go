package profiles

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HumanOnLoop/contracts/documents"
)

func TestClient(t *testing.T) {
	prof := documents.RepoProfile{Repo: "fixture", DefaultBranch: "main", Languages: []string{"python"}, Image: "hotl/agent-python:dev",
		Commands:  documents.RepoProfileCommands{Test: "pytest", TestReport: documents.RepoProfileCommandsTestReportJunitXml},
		TestFiles: []string{"tests/**"}, Ci: documents.RepoProfileCi{Check: "checks"},
		Merge: documents.RepoProfileMerge{Strategy: documents.RepoProfileMergeStrategyMerge}}
	var proposed Proposal
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos":
			_ = json.NewEncoder(w).Encode(map[string]any{"repos": []documents.RepoProfile{prof}})
		case "GET /repos/fixture/profile":
			_ = json.NewEncoder(w).Encode(prof)
		case "GET /repos/missing/profile":
			http.Error(w, `{"error":"no approved profile"}`, http.StatusNotFound)
		case "POST /repos/fixture/profiles":
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &proposed)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"version":3,"document":{"uri":"http://x/repos/fixture/profiles/3","sha256":"00"}}`))
		case "POST /repos/bad/profiles":
			http.Error(w, `{"error":"invalid"}`, http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(srv.URL+"/", nil)
	ctx := context.Background()
	got, err := c.Get(ctx, "fixture")
	if err != nil || got.Ci.Check != "checks" {
		t.Fatalf("Get: %+v, %v", got, err)
	}
	if _, err := c.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get missing: %v", err)
	}
	list, err := c.List(ctx)
	if err != nil || len(list) != 1 {
		t.Errorf("List: %v, %v", list, err)
	}
	p, err := c.Propose(ctx, Proposal{Profile: prof, Attachment: "ATT-0001", RunBriefID: "ATT-0001-profile-1", ApproveOption: "A"})
	if err != nil || p.Version != 3 || proposed.RunBriefID != "ATT-0001-profile-1" {
		t.Errorf("Propose: %+v %v %+v", p, err, proposed)
	}
	bad := prof
	bad.Repo = "bad"
	_, err = c.Propose(ctx, Proposal{Profile: bad})
	var he *HTTPError
	if !errors.As(err, &he) || !he.Permanent() {
		t.Errorf("Propose bad: %v", err)
	}
}
