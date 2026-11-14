package registry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/osbe/contracts/internal/compat"
	"github.com/osbe/contracts/internal/schema"
)

// fakeRegistry is the slice of the Apicurio v3 API that Client uses. It decides compatibility with compat.Backward.
type fakeRegistry struct {
	mu        sync.Mutex
	versions  map[string][]string          // group/id -> contents
	labels    map[string][]map[string]any  // group/id -> labels per version
	rules     map[string]map[string]string // group/id -> ruleType -> config
	ruleCalls int
}

func newFake() *fakeRegistry {
	return &fakeRegistry{versions: map[string][]string{}, labels: map[string][]map[string]any{}, rules: map[string]map[string]string{}}
}

func (f *fakeRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/apis/registry/v3/"), "/")
	body, _ := io.ReadAll(r.Body)
	notFound := func() { http.Error(w, `{"status":404}`, http.StatusNotFound) }
	if len(parts) < 3 || parts[0] != "groups" || parts[2] != "artifacts" {
		notFound()
		return
	}
	group := parts[1]
	if len(parts) == 3 && r.Method == http.MethodPost {
		var req struct {
			ArtifactID   string `json:"artifactId"`
			FirstVersion struct {
				Content struct {
					Content string `json:"content"`
				} `json:"content"`
				Labels map[string]any `json:"labels"`
			} `json:"firstVersion"`
		}
		_ = json.Unmarshal(body, &req)
		key := group + "/" + req.ArtifactID
		f.versions[key] = []string{req.FirstVersion.Content.Content}
		f.labels[key] = []map[string]any{req.FirstVersion.Labels}
		f.rules[key] = map[string]string{}
		_, _ = w.Write([]byte(`{"version":{"version":"1"}}`))
		return
	}
	if len(parts) < 4 {
		notFound()
		return
	}
	key := group + "/" + parts[3]
	vs, ok := f.versions[key]
	if !ok {
		notFound()
		return
	}
	rest := parts[4:]
	switch {
	case len(rest) == 0 && r.Method == http.MethodGet:
		_, _ = w.Write([]byte(`{}`))
	case len(rest) == 1 && rest[0] == "rules" && r.Method == http.MethodPost:
		var rule struct{ RuleType, Config string }
		_ = json.Unmarshal(body, &rule)
		f.rules[key][rule.RuleType] = rule.Config
		f.ruleCalls++
	case len(rest) == 2 && rest[0] == "rules":
		cfg, ok := f.rules[key][rest[1]]
		switch {
		case !ok:
			notFound()
		case r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]string{"ruleType": rest[1], "config": cfg})
		case r.Method == http.MethodPut:
			var rule struct{ Config string }
			_ = json.Unmarshal(body, &rule)
			f.rules[key][rest[1]] = rule.Config
			f.ruleCalls++
		}
	case len(rest) == 2 && rest[0] == "versions" && rest[1] == "branch=latest":
		_ = json.NewEncoder(w).Encode(map[string]string{"version": itoa(len(vs))})
	case len(rest) == 3 && rest[2] == "content":
		_, _ = w.Write([]byte(vs[len(vs)-1]))
	case len(rest) == 1 && rest[0] == "versions" && r.Method == http.MethodPost:
		var req struct {
			Content struct {
				Content string `json:"content"`
			} `json:"content"`
			Labels map[string]any `json:"labels"`
		}
		_ = json.Unmarshal(body, &req)
		issues, err := compat.Backward([]byte(vs[len(vs)-1]), []byte(req.Content.Content))
		if err != nil || len(issues) > 0 {
			w.WriteHeader(http.StatusConflict)
			causes := []map[string]string{}
			for _, i := range issues {
				causes = append(causes, map[string]string{"description": i.Message, "context": i.Path})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"detail": "incompatible", "causes": causes})
			return
		}
		if r.URL.Query().Get("dryRun") == "true" {
			return
		}
		f.versions[key] = append(vs, req.Content.Content)
		f.labels[key] = append(f.labels[key], req.Labels)
		_ = json.NewEncoder(w).Encode(map[string]string{"version": itoa(len(vs) + 1)})
	default:
		notFound()
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

const v1 = `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","additionalProperties":false,
  "properties":{"a":{"type":"string"}},"required":["a"]}`
const v2 = `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","additionalProperties":false,
  "properties":{"a":{"type":"string"},"b":{"type":"string"}},"required":["a"]}`
const breaking = `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","additionalProperties":false,
  "properties":{"a":{"type":"integer"}},"required":["a"]}`

func TestRegisterLifecycle(t *testing.T) {
	f := newFake()
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/apis/registry/v3"}
	ctx := context.Background()
	labels := map[string]string{"contracts-version": "v0.1.0"}

	step := func(content string, want Outcome, wantVersion string) {
		t.Helper()
		got, v, err := c.Register(ctx, "hotl.run", "RunStarted", []byte(content), labels)
		if err != nil {
			t.Fatal(err)
		}
		if got != want || v != wantVersion {
			t.Fatalf("Register = %s %s, want %s %s", got, v, want, wantVersion)
		}
	}
	step(v1, Created, "1")
	if f.rules["hotl.run/RunStarted"]["COMPATIBILITY"] != "BACKWARD" || f.rules["hotl.run/RunStarted"]["VALIDITY"] != "FULL" {
		t.Fatalf("rules after create = %v", f.rules["hotl.run/RunStarted"])
	}
	step(v1, Unchanged, "1")
	// Whitespace and key order do not count as a change.
	step(" "+strings.ReplaceAll(v1, `"type":"object",`, `"type": "object",`), Unchanged, "1")
	step(v2, NewVersion, "2")
	if f.labels["hotl.run/RunStarted"][1]["contracts-version"] != "v0.1.0" {
		t.Errorf("labels = %v", f.labels["hotl.run/RunStarted"])
	}

	// A rule someone loosened by hand is put back.
	f.rules["hotl.run/RunStarted"]["COMPATIBILITY"] = "NONE"
	calls := f.ruleCalls
	step(v2, Unchanged, "2")
	if f.rules["hotl.run/RunStarted"]["COMPATIBILITY"] != "BACKWARD" || f.ruleCalls != calls+1 {
		t.Errorf("rule not restored: %v", f.rules["hotl.run/RunStarted"])
	}

	_, _, err := c.Register(ctx, "hotl.run", "RunStarted", []byte(breaking), labels)
	var rv *RuleViolation
	if !errors.As(err, &rv) || len(rv.Causes) == 0 {
		t.Fatalf("want a RuleViolation with causes, got %v", err)
	}
	if len(f.versions["hotl.run/RunStarted"]) != 2 {
		t.Error("a refused version was stored")
	}
}

func TestCheck(t *testing.T) {
	f := newFake()
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/apis/registry/v3"}
	ctx := context.Background()

	if err := c.Check(ctx, "hotl.run", "New", []byte(v1)); err != nil {
		t.Errorf("unregistered schema: %v", err)
	}
	if _, _, err := c.Register(ctx, "hotl.run", "RunStarted", []byte(v1), nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Check(ctx, "hotl.run", "RunStarted", []byte(v2)); err != nil {
		t.Errorf("compatible change: %v", err)
	}
	var rv *RuleViolation
	if err := c.Check(ctx, "hotl.run", "RunStarted", []byte(breaking)); !errors.As(err, &rv) {
		t.Errorf("breaking change: want RuleViolation, got %v", err)
	}
	if len(f.versions["hotl.run/RunStarted"]) != 1 {
		t.Error("Check stored a version")
	}
}

// TestRegistryAcceptsSchemas asks the real registry whether every schema in the working tree would be accepted as
// the next version of its artifact. It runs only when HOTL_REGISTRY_URL is set; make check-registry sets it for the
// k3d-hotl registry.
func TestRegistryAcceptsSchemas(t *testing.T) {
	base := os.Getenv("HOTL_REGISTRY_URL")
	if base == "" {
		t.Skip("HOTL_REGISTRY_URL not set")
	}
	fsys := os.DirFS("../..")
	arts, err := schema.Artifacts(fsys)
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{BaseURL: base}
	for _, a := range arts {
		content, err := schema.Bundle(fsys, a.Path)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Check(context.Background(), a.Group, a.ID, content); err != nil {
			t.Errorf("%s: %v", a.Path, err)
		}
	}
}
