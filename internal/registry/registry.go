// Package registry registers the contracts schemas in Apicurio Registry 3 through its v3 REST API.
package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
)

// Rules every artifact gets, set explicitly per artifact so they hold whatever the registry's global rules say.
var Rules = map[string]string{
	"COMPATIBILITY": "BACKWARD", // a new version must read everything the previous one could
	"VALIDITY":      "FULL",     // a new version must be a valid JSON Schema
}

// Client talks to one registry.
type Client struct {
	BaseURL string // for example http://hotl-registry.localhost:8480/apis/registry/v3
	HTTP    *http.Client
}

// Outcome of registering one schema.
type Outcome string

const (
	Created    Outcome = "created"     // new artifact, first version
	NewVersion Outcome = "new version" // content changed and passed the rules
	Unchanged  Outcome = "unchanged"   // latest version already has this content
)

// RuleViolation is the registry refusing a version, with its reasons.
type RuleViolation struct {
	Group, ID string
	Causes    []string
}

func (e *RuleViolation) Error() string {
	return fmt.Sprintf("%s/%s: registry refused the new version: %s", e.Group, e.ID, strings.Join(e.Causes, "; "))
}

// Register makes content the latest version of the artifact, creating it if needed, and makes sure the artifact
// carries Rules. labels are attached to a version it creates.
func (c *Client) Register(ctx context.Context, group, id string, content []byte, labels map[string]string) (Outcome, string, error) {
	exists, err := c.exists(ctx, group, id)
	if err != nil {
		return "", "", err
	}
	if !exists {
		body := map[string]any{
			"artifactId":   id,
			"artifactType": "JSON",
			"firstVersion": map[string]any{"content": jsonContent(content), "labels": labels},
		}
		var resp struct {
			Version struct {
				Version string `json:"version"`
			} `json:"version"`
		}
		if err := c.do(ctx, http.MethodPost, c.path("groups", group, "artifacts"), body, &resp); err != nil {
			return "", "", err
		}
		if err := c.ensureRules(ctx, group, id); err != nil {
			return "", "", err
		}
		return Created, resp.Version.Version, nil
	}

	if err := c.ensureRules(ctx, group, id); err != nil {
		return "", "", err
	}
	latest, version, err := c.latest(ctx, group, id)
	if err != nil {
		return "", "", err
	}
	if sameJSON(latest, content) {
		return Unchanged, version, nil
	}
	var resp struct {
		Version string `json:"version"`
	}
	body := map[string]any{"content": jsonContent(content), "labels": labels}
	if err := c.do(ctx, http.MethodPost, c.path("groups", group, "artifacts", id, "versions"), body, &resp); err != nil {
		return "", "", violation(group, id, err)
	}
	return NewVersion, resp.Version, nil
}

// Check asks the registry whether content would be accepted as the next version, without registering it. A schema
// that is not registered yet is always accepted.
func (c *Client) Check(ctx context.Context, group, id string, content []byte) error {
	exists, err := c.exists(ctx, group, id)
	if err != nil || !exists {
		return err
	}
	body := map[string]any{"content": jsonContent(content)}
	err = c.do(ctx, http.MethodPost, c.path("groups", group, "artifacts", id, "versions")+"?dryRun=true", body, nil)
	return violation(group, id, err)
}

func (c *Client) exists(ctx context.Context, group, id string) (bool, error) {
	err := c.do(ctx, http.MethodGet, c.path("groups", group, "artifacts", id), nil, nil)
	if se, ok := err.(*statusError); ok && se.code == http.StatusNotFound {
		return false, nil
	}
	return err == nil, err
}

func (c *Client) latest(ctx context.Context, group, id string) ([]byte, string, error) {
	var meta struct {
		Version string `json:"version"`
	}
	base := c.path("groups", group, "artifacts", id, "versions", "branch=latest")
	if err := c.do(ctx, http.MethodGet, base, nil, &meta); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/content", nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", &statusError{code: resp.StatusCode, body: b}
	}
	return b, meta.Version, nil
}

func (c *Client) ensureRules(ctx context.Context, group, id string) error {
	for ruleType, config := range Rules {
		var current struct {
			Config string `json:"config"`
		}
		rulePath := c.path("groups", group, "artifacts", id, "rules", ruleType)
		err := c.do(ctx, http.MethodGet, rulePath, nil, &current)
		switch se, ok := err.(*statusError); {
		case ok && se.code == http.StatusNotFound:
			body := map[string]string{"ruleType": ruleType, "config": config}
			if err := c.do(ctx, http.MethodPost, c.path("groups", group, "artifacts", id, "rules"), body, nil); err != nil {
				return fmt.Errorf("%s/%s: add %s rule: %w", group, id, ruleType, err)
			}
		case err != nil:
			return err
		case current.Config != config:
			body := map[string]string{"ruleType": ruleType, "config": config}
			if err := c.do(ctx, http.MethodPut, rulePath, body, nil); err != nil {
				return fmt.Errorf("%s/%s: set %s rule: %w", group, id, ruleType, err)
			}
		}
	}
	return nil
}

func (c *Client) path(segments ...string) string {
	esc := make([]string, len(segments))
	for i, s := range segments {
		esc[i] = url.PathEscape(s)
	}
	return strings.TrimSuffix(c.BaseURL, "/") + "/" + strings.Join(esc, "/")
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

type statusError struct {
	code int
	body []byte
}

func (e *statusError) Error() string {
	return fmt.Sprintf("registry answered %d: %s", e.code, strings.TrimSpace(string(e.body)))
}

func (c *Client) do(ctx context.Context, method, u string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &statusError{code: resp.StatusCode, body: b}
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

// violation turns a 4xx answer that lists causes into a *RuleViolation.
func violation(group, id string, err error) error {
	se, ok := err.(*statusError)
	if !ok || se.code < 400 || se.code > 499 {
		return err
	}
	var problem struct {
		Detail string `json:"detail"`
		Causes []struct {
			Description string `json:"description"`
			Context     string `json:"context"`
		} `json:"causes"`
	}
	if json.Unmarshal(se.body, &problem) != nil || (len(problem.Causes) == 0 && problem.Detail == "") {
		return err
	}
	v := &RuleViolation{Group: group, ID: id}
	for _, c := range problem.Causes {
		v.Causes = append(v.Causes, strings.TrimSpace(c.Description+" at "+c.Context))
	}
	if len(v.Causes) == 0 {
		v.Causes = []string{problem.Detail}
	}
	return v
}

func jsonContent(b []byte) map[string]string {
	return map[string]string{"content": string(b), "contentType": "application/json"}
}

func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
