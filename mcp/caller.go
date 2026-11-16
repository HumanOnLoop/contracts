package mcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// HeaderCaller carries a caller token: who an agent Job is, signed by the agent runtime with a key it shares with the
// MCP server (ADR 0017). The server takes the run, task and role only from a valid token; without one a call is
// answered, but credits no precedent.
const HeaderCaller = "Hotl-Caller"

// Caller is who calls the MCP server: an agent Job's run, task and role.
type Caller struct {
	Run     string    `json:"run"`
	Task    string    `json:"task,omitempty"`
	Role    string    `json:"role"`
	Expires time.Time `json:"exp"`
}

// ErrBadCaller is a caller token that is malformed, forged or expired.
var ErrBadCaller = errors.New("invalid caller token")

// SignCaller makes a caller token: the caller as JSON and its HMAC-SHA256 under key, both base64url.
func SignCaller(key []byte, c Caller) (string, error) {
	if len(key) < 32 {
		return "", errors.New("a caller key has at least 32 bytes")
	}
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(b)
	m := hmac.New(sha256.New, key)
	m.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}

// VerifyCaller checks a caller token's signature and expiry and returns the caller.
func VerifyCaller(key []byte, token string, now time.Time) (Caller, error) {
	payload, sig, ok := strings.Cut(token, ".")
	if !ok || len(key) < 32 {
		return Caller{}, ErrBadCaller
	}
	want := hmac.New(sha256.New, key)
	want.Write([]byte(payload))
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, want.Sum(nil)) {
		return Caller{}, ErrBadCaller
	}
	b, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Caller{}, ErrBadCaller
	}
	var c Caller
	if err := json.Unmarshal(b, &c); err != nil || c.Run == "" || c.Role == "" || !now.Before(c.Expires) {
		return Caller{}, ErrBadCaller
	}
	return c, nil
}
