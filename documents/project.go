package documents

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const (
	organizationSchema = "documents/organization.schema.json"
	projectSchema      = "documents/project.schema.json"
)

// ParseOrganization reads and validates an organization JSON document.
func ParseOrganization(raw []byte) (Organization, error) {
	var o Organization
	raw = bytes.TrimPrefix(raw, []byte("\uFEFF"))
	if err := schemaValidateJSON(organizationSchema, raw); err != nil {
		return o, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		return o, fmt.Errorf("organization: %w", err)
	}
	return o, nil
}

// ValidateOrganization checks an Organization struct against its schema.
func ValidateOrganization(o Organization) error { return validate(organizationSchema, o) }

// ParseProject reads and validates a project JSON document.
func ParseProject(raw []byte) (Project, error) {
	var p Project
	raw = bytes.TrimPrefix(raw, []byte("\uFEFF"))
	if err := schemaValidateJSON(projectSchema, raw); err != nil {
		return p, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, fmt.Errorf("project: %w", err)
	}
	return p, nil
}

// ValidateProject checks a Project struct against its schema.
func ValidateProject(p Project) error { return validate(projectSchema, p) }
