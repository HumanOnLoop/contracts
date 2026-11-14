package agents

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/HumanOnLoop/contracts/documents"
	"github.com/HumanOnLoop/contracts/events"
)

// InlinePrefix starts the URI of every inline Markdown document.
const InlinePrefix = "data:text/markdown;charset=utf-8;base64,"

// Inline returns a reference that carries the Markdown itself: a data: URI and its SHA-256. Nothing stores
// documents yet, so briefs and plans travel this way until a document store exists.
func Inline(md []byte) events.DocumentRef {
	sum := sha256.Sum256(md)
	return events.DocumentRef{URI: InlinePrefix + base64.StdEncoding.EncodeToString(md), Sha256: hex.EncodeToString(sum[:])}
}

// DecodeInline returns the Markdown of an inline document, after checking it against its SHA-256.
func DecodeInline(ref events.DocumentRef) ([]byte, error) {
	enc, ok := strings.CutPrefix(ref.URI, InlinePrefix)
	if !ok {
		return nil, fmt.Errorf("%s is not an inline document", ref.URI)
	}
	md, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, err
	}
	if sum := sha256.Sum256(md); hex.EncodeToString(sum[:]) != ref.Sha256 {
		return nil, fmt.Errorf("inline document does not match its sha256")
	}
	return md, nil
}

// RenderBrief writes a review brief's Markdown, per the review brief template, and checks its front matter
// against the review brief schema.
func RenderBrief(req BriefRequest) ([]byte, error) {
	var b strings.Builder
	blocks := make([]string, len(req.Blocks))
	for i, t := range req.Blocks {
		blocks[i] = string(t)
	}
	fmt.Fprintf(&b, "---\nid: %s\nrun: %s\nintent: %s v%d\ncategory: %s\nto: %s\nraised_by: %s\nurgency: %s\n",
		yamlString(string(req.ID)), yamlString(string(req.Run.RunID)), req.Run.IntentID, req.Run.IntentVersion,
		req.Category, yamlString(req.To), req.RaisedBy, req.Urgency)
	fmt.Fprintf(&b, "deadline: %s\ndefault: %s\nblocks: [%s]\n---\n\n", req.Deadline.UTC().Format("2006-01-02T15:04"),
		req.Default, strings.Join(blocks, ", "))
	fmt.Fprintf(&b, "## Question\n%s\n\n## Context\n%s\n\n## Options\n", req.Question, req.Context)
	for _, o := range req.Options {
		rec := ""
		if o.Recommended {
			rec = " (recommended)"
		}
		fmt.Fprintf(&b, "### %s: %s%s\n%s Impact: %s\n\n", o.ID, o.Name, rec, o.Text, o.Impact)
	}
	precedents := req.Precedents
	if precedents == "" {
		precedents = "None found."
	}
	fmt.Fprintf(&b, "## Recommendation\n%s\n\n## Precedents\n%s\n\n## Answer\n", req.Recommendation, precedents)
	md := []byte(b.String())
	if _, _, err := documents.ParseReviewBrief(md); err != nil {
		return nil, fmt.Errorf("brief %s does not match the review brief template: %w", req.ID, err)
	}
	return md, nil
}

// yamlString quotes s for YAML when it needs it (names such as "Developer, Orders team" contain a comma).
func yamlString(s string) string {
	if strings.ContainsAny(s, ",:#[]{}&*!|>'\"%@`") || strings.TrimSpace(s) != s {
		return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
	}
	return s
}
