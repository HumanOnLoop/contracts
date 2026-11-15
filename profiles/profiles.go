// Package profiles reads repo profiles from the review service, which stores them and records their approval
// (ADR 0013). Intake, the agent runtime, the CI/CD adapter and the orchestrator share this client.
//
// The review service's API for profiles:
//
//	GET  /repos                              {"repos": [profile...]}: the approved profile of every attached repo
//	GET  /repos/{repo}/profile               the approved profile; 404 when the repo has none
//	GET  /repos/{repo}/profiles              {"profiles": [Version...]}: every version, proposals included
//	GET  /repos/{repo}/profiles/{version}    one version's profile, as stored (its stable URL)
//	POST /repos/{repo}/profiles              Proposal → 201 Proposed
//
// and for the toolchain image catalogue (since v0.9.0):
//
//	GET  /images                             {"images": [entry...]}: every approved catalogue entry
//	GET  /images/{name}                      the approved entry; 404 when there is none
//	GET  /images/{name}/versions             {"versions": [ImageVersion...]}
//	GET  /images/{name}/versions/{version}   one version's entry, as stored
//	POST /images/{name}/versions             ImageProposal → 201 Proposed
package profiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/HumanOnLoop/contracts/documents"
	"github.com/HumanOnLoop/contracts/events"
)

// ErrNotFound reports a repo with no approved profile: it is not attached to HOTL.
var ErrNotFound = errors.New("no approved profile")

// Status of a profile version.
type Status string

const (
	// StatusProposed: waiting for the architect's decision on its brief.
	StatusProposed Status = "proposed"
	// StatusApproved: the version in effect.
	StatusApproved Status = "approved"
	// StatusSuperseded: approved once, replaced by a later approved version.
	StatusSuperseded Status = "superseded"
	// StatusDeclined: the brief was answered with anything but approval.
	StatusDeclined Status = "declined"
	// StatusWithdrawn: its brief was withdrawn before a decision.
	StatusWithdrawn Status = "withdrawn"
)

// Version is one version of a repo's profile and where it stands.
type Version struct {
	Repo       events.Repo           `json:"repo"`
	Version    int                   `json:"version"`
	Status     Status                `json:"status"`
	Profile    documents.RepoProfile `json:"profile"`
	Document   events.DocumentRef    `json:"document"`
	Attachment string                `json:"attachment,omitempty"`
	// RunBriefID is the id the proposer gave the approval brief; BriefID the one the review service assigned.
	RunBriefID string        `json:"runBriefId,omitempty"`
	BriefID    string        `json:"briefId,omitempty"`
	DecidedBy  events.Person `json:"decidedBy,omitempty"`
	ProposedAt time.Time     `json:"proposedAt"`
	DecidedAt  *time.Time    `json:"decidedAt,omitempty"`
}

// Proposal proposes a new version of a repo's profile. It takes effect only when the brief named by RunBriefID is
// answered with ApproveOption.
type Proposal struct {
	Profile    documents.RepoProfile `json:"profile"`
	Attachment string                `json:"attachment"`
	// RunBriefID is the id the proposer gives the rule brief that asks for approval (the BriefRaised subject).
	RunBriefID string `json:"runBriefId"`
	// ApproveOption is the brief's option that approves the profile, for example A.
	ApproveOption string `json:"approveOption"`
}

// Proposed is the stored proposal: its version and its stable URL.
type Proposed struct {
	Version  int                `json:"version"`
	Document events.DocumentRef `json:"document"`
}

// Client reads and proposes profiles.
type Client struct {
	// URL is the review service's base URL, for example http://review-service.hotl.svc:8080.
	URL  string
	HTTP *http.Client
}

// New returns a client for the review service at base. A nil httpClient means http.DefaultClient with a 30s
// timeout.
func New(base string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{URL: strings.TrimRight(base, "/"), HTTP: httpClient}
}

// Get returns the repo's approved profile, or ErrNotFound.
func (c *Client) Get(ctx context.Context, repo events.Repo) (documents.RepoProfile, error) {
	var p documents.RepoProfile
	err := c.do(ctx, http.MethodGet, "/repos/"+url.PathEscape(string(repo))+"/profile", nil, &p)
	return p, err
}

// List returns the approved profile of every attached repo.
func (c *Client) List(ctx context.Context) ([]documents.RepoProfile, error) {
	var out struct {
		Repos []documents.RepoProfile `json:"repos"`
	}
	err := c.do(ctx, http.MethodGet, "/repos", nil, &out)
	return out.Repos, err
}

// Versions returns every version of the repo's profile, newest first.
func (c *Client) Versions(ctx context.Context, repo events.Repo) ([]Version, error) {
	var out struct {
		Profiles []Version `json:"profiles"`
	}
	err := c.do(ctx, http.MethodGet, "/repos/"+url.PathEscape(string(repo))+"/profiles", nil, &out)
	return out.Profiles, err
}

// Propose stores a proposal. Proposing the same profile for the same brief again returns the version stored the
// first time.
func (c *Client) Propose(ctx context.Context, p Proposal) (Proposed, error) {
	var out Proposed
	b, err := json.Marshal(p)
	if err != nil {
		return out, err
	}
	err = c.do(ctx, http.MethodPost, "/repos/"+url.PathEscape(p.Profile.Repo)+"/profiles", b, &out)
	return out, err
}

// HTTPError is an answer other than 2xx; 4xx answers will not change on a retry.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("review service: %d %s", e.Status, e.Body) }

// Permanent reports whether a retry cannot change the answer (4xx).
func (e *HTTPError) Permanent() bool { return e.Status >= 400 && e.Status < 500 }

func (c *Client) do(ctx context.Context, method, path string, body []byte, out any) error {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("review service %s %s: %w", method, path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode == http.StatusNotFound && method == http.MethodGet {
		return fmt.Errorf("%w: %s", ErrNotFound, strings.TrimSpace(string(raw)))
	}
	if res.StatusCode/100 != 2 {
		return &HTTPError{Status: res.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("review service %s %s: %w", method, path, err)
	}
	return nil
}

// ImageVersion is one version of a toolchain image catalogue entry and where it stands.
type ImageVersion struct {
	Name       string                   `json:"name"`
	Version    int                      `json:"version"`
	Status     Status                   `json:"status"`
	Image      documents.ToolchainImage `json:"image"`
	Document   events.DocumentRef       `json:"document"`
	Request    string                   `json:"request,omitempty"`
	RunBriefID string                   `json:"runBriefId,omitempty"`
	BriefID    string                   `json:"briefId,omitempty"`
	DecidedBy  events.Person            `json:"decidedBy,omitempty"`
	ProposedAt time.Time                `json:"proposedAt"`
	DecidedAt  *time.Time               `json:"decidedAt,omitempty"`
}

// ImageProposal proposes a new version of a catalogue entry; it takes effect only when the brief named by
// RunBriefID is answered with ApproveOption.
type ImageProposal struct {
	Image         documents.ToolchainImage `json:"image"`
	Request       string                   `json:"request"`
	RunBriefID    string                   `json:"runBriefId"`
	ApproveOption string                   `json:"approveOption"`
}

// GetImage returns the approved catalogue entry of that name, or ErrNotFound.
func (c *Client) GetImage(ctx context.Context, name string) (documents.ToolchainImage, error) {
	var t documents.ToolchainImage
	err := c.do(ctx, http.MethodGet, "/images/"+url.PathEscape(name), nil, &t)
	return t, err
}

// ListImages returns every approved catalogue entry.
func (c *Client) ListImages(ctx context.Context) ([]documents.ToolchainImage, error) {
	var out struct {
		Images []documents.ToolchainImage `json:"images"`
	}
	err := c.do(ctx, http.MethodGet, "/images", nil, &out)
	return out.Images, err
}

// ProposeImage stores a proposed catalogue entry version.
func (c *Client) ProposeImage(ctx context.Context, p ImageProposal) (Proposed, error) {
	var out Proposed
	b, err := json.Marshal(p)
	if err != nil {
		return out, err
	}
	err = c.do(ctx, http.MethodPost, "/images/"+url.PathEscape(p.Image.Name)+"/versions", b, &out)
	return out, err
}
