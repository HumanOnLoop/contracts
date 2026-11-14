// Package contracts holds HOTL's JSON Schemas: the event envelope, one schema per event in the catalogue, and the
// front matter of intent packages and review briefs. The schemas are the source of truth; the Go types in the
// events and documents packages are generated from them.
//
// Every schema's $id is BaseURI followed by its path in this repository, so Schemas can resolve each one.
package contracts

import "embed"

// BaseURI prefixes every schema's $id: the $id of events/run/RunStarted.schema.json is
// BaseURI + "events/run/RunStarted.schema.json".
const BaseURI = "https://github.com/osbe/contracts/"

// Schemas holds every schema file, at its path in this repository.
//
//go:embed events/*.schema.json events/*/*.schema.json documents/*.schema.json
var Schemas embed.FS
