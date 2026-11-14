package documents

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

// The fixtures are the worked examples from docs/design, copied verbatim.
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWorkedIntentPackage(t *testing.T) {
	ip, body, err := ParseIntentPackage([]byte(fixture(t, "intent-package-INT-0042.md")))
	if err != nil {
		t.Fatal(err)
	}
	if ip.ID != "INT-0042" || ip.Title != "Export orders as CSV" || ip.PO != "Product owner, Orders" ||
		ip.Developer != "Developer, Orders team" || ip.Autonomy != IntentPackageAutonomyL3 ||
		ip.Priority != IntentPackagePriorityHigh {
		t.Errorf("parsed %+v", ip)
	}
	if strings.Join(ip.Repos, ",") != "orders-service,gateway" {
		t.Errorf("repos = %v", ip.Repos)
	}
	if ip.Due == nil || *ip.Due != "2026-12-15" {
		t.Errorf("due = %v", ip.Due)
	}
	if ip.Source == nil || *ip.Source != "https://tracker.example.com/ORD-311" {
		t.Errorf("source = %v", ip.Source)
	}
	if !bytes.HasPrefix(bytes.TrimSpace(body), []byte("## Outcome")) {
		t.Errorf("body starts %q", body[:min(40, len(body))])
	}
	if err := ValidateIntentPackage(ip); err != nil {
		t.Errorf("parsed package does not validate: %v", err)
	}
}

func TestWorkedReviewBrief(t *testing.T) {
	rb, body, err := ParseReviewBrief([]byte(fixture(t, "review-brief-BRF-0107.md")))
	if err != nil {
		t.Fatal(err)
	}
	if rb.ID != "BRF-0107" || rb.Run != "RUN-0042-1" || rb.Intent != "INT-0042 v1" ||
		rb.Category != ReviewBriefCategoryImplementation || rb.To != "Developer, Orders team" ||
		rb.RaisedBy != ReviewBriefRaisedByImplementer || rb.Urgency != ReviewBriefUrgencyNormal ||
		rb.Deadline != "2026-11-16T15:00" || rb.Default != "A" {
		t.Errorf("parsed %+v", rb)
	}
	if strings.Join(rb.Blocks, ",") != "T-7,T-8" {
		t.Errorf("blocks = %v", rb.Blocks)
	}
	if !bytes.Contains(body, []byte("## Answer")) {
		t.Error("body lost the Answer section")
	}
	if err := ValidateReviewBrief(rb); err != nil {
		t.Errorf("parsed brief does not validate: %v", err)
	}
}

// replace changes one front matter line of a fixture; an empty to deletes the line.
func replace(t *testing.T, doc, from, to string) string {
	t.Helper()
	if !strings.Contains(doc, from+"\n") {
		t.Fatalf("fixture has no line %q", from)
	}
	if to == "" {
		return strings.Replace(doc, from+"\n", "", 1)
	}
	return strings.Replace(doc, from+"\n", to+"\n", 1)
}

func wantProblem(t *testing.T, err error, path string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %v", err)
	}
	for _, p := range ve.Problems {
		if p.Path == path {
			return
		}
	}
	t.Errorf("no problem at %q: %v", path, ve)
}

func TestIntentPackageChecks(t *testing.T) {
	doc := fixture(t, "intent-package-INT-0042.md")
	cases := []struct {
		name, from, to, path string
	}{
		{"title of 80 characters", "title: Export orders as CSV", "title: " + strings.Repeat("x", 80), "/title"},
		{"missing id", "id: INT-0042", "", ""},
		{"missing po", "po: Product owner, Orders", "", ""},
		{"no repos", "repos: [orders-service, gateway]", "repos: []", "/repos"},
		{"repo twice", "repos: [orders-service, gateway]", "repos: [gateway, gateway]", "/repos"},
		{"unknown autonomy", "autonomy: L3", "autonomy: L4", "/autonomy"},
		{"unknown priority", "priority: high", "priority: urgent", "/priority"},
		{"due not a date", "due: 2026-12-15", "due: next week", "/due"},
		{"source not a URL", "source: https://tracker.example.com/ORD-311", "source: ORD-311", "/source"},
		{"unknown field", "priority: high", "priority: high\nowner: someone", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseIntentPackage([]byte(replace(t, doc, tc.from, tc.to)))
			wantProblem(t, err, tc.path)
		})
	}

	t.Run("optional fields may be empty or absent", func(t *testing.T) {
		d := replace(t, doc, "due: 2026-12-15", "due:")
		d = replace(t, d, "source: https://tracker.example.com/ORD-311", "")
		ip, _, err := ParseIntentPackage([]byte(d))
		if err != nil {
			t.Fatal(err)
		}
		if ip.Due != nil || ip.Source != nil {
			t.Errorf("due %v, source %v", ip.Due, ip.Source)
		}
	})
	t.Run("title of 79 characters", func(t *testing.T) {
		if _, _, err := ParseIntentPackage([]byte(replace(t, doc, "title: Export orders as CSV",
			"title: "+strings.Repeat("x", 79)))); err != nil {
			t.Error(err)
		}
	})
}

func TestReviewBriefChecks(t *testing.T) {
	doc := fixture(t, "review-brief-BRF-0107.md")
	cases := []struct {
		name, from, to, path string
	}{
		{"scope brief with a default option", "category: implementation", "category: scope", "/default"},
		{"rule brief with a default option", "category: implementation", "category: rule", "/default"},
		{"budget brief with a default option", "category: implementation", "category: budget", "/default"},
		{"unknown category", "category: implementation", "category: design", "/category"},
		{"unknown agent role", "raised_by: implementer", "raised_by: product-owner", "/raised_by"},
		{"unknown urgency", "urgency: normal", "urgency: asap", "/urgency"},
		{"deadline with a time zone", "deadline: 2026-11-16T15:00", "deadline: 2026-11-16T15:00:00Z", "/deadline"},
		{"deadline without a time", "deadline: 2026-11-16T15:00", "deadline: 2026-11-16", "/deadline"},
		{"intent without a version", "intent: INT-0042 v1", "intent: INT-0042", "/intent"},
		{"default that is not an option id", "default: A", "default: maybe", "/default"},
		{"missing blocks", "blocks: [T-7, T-8]", "", ""},
		{"missing to", "to: Developer, Orders team", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseReviewBrief([]byte(replace(t, doc, tc.from, tc.to)))
			wantProblem(t, err, tc.path)
		})
	}

	for _, ok := range []struct {
		name  string
		edits [][2]string // from, to; applied in order
	}{
		{"scope brief that pauses", [][2]string{{"category: implementation", "category: scope"}, {"default: A", "default: pause"}}},
		{"implementation brief that pauses", [][2]string{{"default: A", "default: pause"}}},
		{"no blocked tasks", [][2]string{{"blocks: [T-7, T-8]", "blocks: []"}}},
	} {
		t.Run(ok.name, func(t *testing.T) {
			d := doc
			for _, e := range ok.edits {
				d = replace(t, d, e[0], e[1])
			}
			if _, _, err := ParseReviewBrief([]byte(d)); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestFrontMatter(t *testing.T) {
	cases := []struct {
		name, doc string
		wantErr   bool
		wantBody  string
	}{
		{"no front matter", "# Title\n", true, ""},
		{"unterminated", "---\nid: x\n", true, ""},
		{"key set twice", "---\nid: a\nid: b\n---\n", true, ""},
		{"empty key set twice", "---\ndue:\ndue:\n---\n", true, ""},
		{"not a mapping", "---\n- a\n---\n", true, ""},
		{"CRLF and BOM", "\uFEFF---\r\nid: x\r\n---\r\nbody\r\n", false, "body\n"},
		{"closing line at end of file", "---\nid: x\n---", false, ""},
		{"empty front matter", "---\n---\nbody", false, "body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, body, err := FrontMatter([]byte(tc.doc))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if !tc.wantErr && string(body) != tc.wantBody {
				t.Errorf("body = %q, want %q", body, tc.wantBody)
			}
		})
	}
	if _, _, err := FrontMatter([]byte("# no front matter")); !errors.Is(err, ErrNoFrontMatter) {
		t.Errorf("want ErrNoFrontMatter, got %v", err)
	}

	// Values are text, as written: no timestamps, numbers or booleans.
	fm, _, err := FrontMatter([]byte("---\ndue: 2026-12-15\ncount: 007\nflag: true\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if fm["due"] != "2026-12-15" || fm["count"] != "007" || fm["flag"] != "true" {
		t.Errorf("front matter = %#v", fm)
	}
}
