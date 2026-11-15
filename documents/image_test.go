package documents

import (
	"strings"
	"testing"
)

func TestParseToolchainImage(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for ref, ok := range map[string]bool{
		"sha256:" + digest:                      true,
		"ghcr.io/acme/java-21@sha256:" + digest: true,
		"hotl/agent-python:dev":                 false,
		"ghcr.io/acme/java:21":                  false,
		"sha256:" + digest[:10]:                 false,
	} {
		raw := `{"name":"java","ref":"` + ref + `","languages":["java"],"description":"JDK 21 and Maven"}`
		img, err := ParseToolchainImage([]byte(raw))
		if (err == nil) != ok {
			t.Errorf("%s: %v", ref, err)
		}
		if ok && (img.Name != "java" || img.Number() != 0) {
			t.Errorf("%s: %+v", ref, img)
		}
	}
	if _, err := ParseToolchainImage([]byte(`{"name":"Java!","ref":"sha256:` + digest + `","languages":["java"],"description":"x"}`)); err == nil {
		t.Error("a name with capitals and punctuation was accepted")
	}
}
