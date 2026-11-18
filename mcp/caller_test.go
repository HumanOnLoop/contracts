package mcp

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCallerToken(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	c := Caller{Run: "RUN-0060-1", Task: "T-1", Role: "implementer", Org: "humanonloop", Project: "hotl", Expires: now.Add(time.Hour)}
	tok, err := SignCaller(key, c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyCaller(key, tok, now)
	if err != nil || got != c {
		t.Fatalf("verify: %+v %v", got, err)
	}
	payload, sig, _ := strings.Cut(tok, ".")
	forged := strings.Replace(payload, "R", "S", 1) + "." + sig
	for name, bad := range map[string]func() (Caller, error){
		"expired":   func() (Caller, error) { return VerifyCaller(key, tok, now.Add(2*time.Hour)) },
		"other key": func() (Caller, error) { return VerifyCaller([]byte(strings.Repeat("x", 32)), tok, now) },
		"forged":    func() (Caller, error) { return VerifyCaller(key, forged, now) },
		"garbage":   func() (Caller, error) { return VerifyCaller(key, "nope", now) },
	} {
		if _, err := bad(); !errors.Is(err, ErrBadCaller) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := SignCaller([]byte("short"), c); err == nil {
		t.Error("a short key was accepted")
	}
}
