package documents

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const toolchainImageSchema = "documents/toolchain-image.schema.json"

// ParseToolchainImage reads a toolchain image catalogue entry from JSON, validated against its schema (ADR 0013).
func ParseToolchainImage(raw []byte) (ToolchainImage, error) {
	var t ToolchainImage
	raw = bytes.TrimPrefix(raw, []byte("\uFEFF"))
	if err := schemaValidateJSON(toolchainImageSchema, raw); err != nil {
		return t, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return t, fmt.Errorf("toolchain image: %w", err)
	}
	return t, nil
}

// ValidateToolchainImage checks a catalogue entry built in code.
func ValidateToolchainImage(t ToolchainImage) error { return validate(toolchainImageSchema, t) }

// Number is the entry's version; 0 when it has none yet (a proposal).
func (t ToolchainImage) Number() int {
	if t.Version == nil {
		return 0
	}
	return *t.Version
}
