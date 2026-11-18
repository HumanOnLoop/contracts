package documents

import "testing"

func TestParseOrganizationAndProject(t *testing.T) {
	orgJSON := []byte(`{"org":"humanonloop","githubOrg":"HumanOnLoop","description":"HOTL org"}`)
	org, err := ParseOrganization(orgJSON)
	if err != nil {
		t.Fatalf("ParseOrganization: %v", err)
	}
	if org.Org != "humanonloop" || org.GithubOrg == nil || *org.GithubOrg != "HumanOnLoop" {
		t.Errorf("unexpected org: %+v", org)
	}

	projJSON := []byte(`{
		"org": "humanonloop",
		"project": "java-k8s",
		"description": "Java K8s services",
		"egress": {"allowHosts": ["repo.maven.apache.org"]}
	}`)
	proj, err := ParseProject(projJSON)
	if err != nil {
		t.Fatalf("ParseProject: %v", err)
	}
	if proj.Org != "humanonloop" || proj.Project != "java-k8s" || proj.Egress == nil || len(proj.Egress.AllowHosts) != 1 {
		t.Errorf("unexpected project: %+v", proj)
	}
	if _, err := ParseProject([]byte(`{"org":"humanonloop"}`)); err == nil {
		t.Error("expected missing project to fail")
	}
}
