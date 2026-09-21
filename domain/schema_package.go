package domain

import (
	"fmt"
	"time"
)

// SchemaPackage is a portable JSON bundle carrying (optionally) a
// schema definition, a named-relation catalog, and one or more
// validation rulesets.
//
// Consumers load bundles at startup: a schema-defining package
// creates tables (via a consumer-provided DDL applier), a
// ruleset-only package layers rules on top of an already-present
// schema. The Requires block declares what the bundle needs at
// load time; consumers verify against the live database before
// activating rules.
//
// Fields unknown to the loader are silently ignored, so a bundle
// can carry application-specific metadata that gsvalidator doesn't
// reason about.
type SchemaPackage struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Domain      string    `json:"domain,omitempty"`
	Description string    `json:"description,omitempty"`
	Author      string    `json:"author,omitempty"`
	License     string    `json:"license,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitempty"`

	// SchemaFingerprint carries a SHA256 hash of the normalized
	// schema for content-addressable discovery of matching
	// packages (e.g. re-import of a plain SQLite that lost its
	// metadata). Not used for load-time compatibility gating —
	// Requires does that instead. See CompatSchema in this file.
	SchemaFingerprint string `json:"schema_fingerprint,omitempty"`

	// Tables is populated on schema-defining packages. Consumers
	// apply the DDL to create tables + insert InitialData rows.
	// Ruleset-only packages leave this empty.
	Tables []PackageTable `json:"tables,omitempty"`

	// Relations is the named-join catalog rules may reference.
	// Constrained shape: each relation is a bounded join chain
	// with no arbitrary SQL.
	Relations map[string]Relation `json:"relations,omitempty"`

	// Rules are the validation rules the bundle contributes. May
	// be empty on a schema-only package.
	Rules []*Rule `json:"rules,omitempty"`

	// Requires declares the schema features (tables, columns,
	// relations) this bundle needs at load time. Consumers walk
	// the live database and verify each item resolves; missing
	// items are per-item load-time errors. Additive on the archive
	// side — extra tables/columns/relations are fine.
	Requires *Requires `json:"requires,omitempty"`
}

// PackageTable wraps a Table definition with optional initial-data
// rows (used to seed lookup tables at package install time).
type PackageTable struct {
	Table       Table                    `json:"table"`
	InitialData []map[string]interface{} `json:"initial_data,omitempty"`
}

// GetTableByName finds a table by name within the package. Returns
// nil when no table matches.
func (sp *SchemaPackage) GetTableByName(name string) *PackageTable {
	for i, pt := range sp.Tables {
		if pt.Table.Name == name {
			return &sp.Tables[i]
		}
	}
	return nil
}

// Validate checks that the package has the minimum fields to be
// usable: name and version. Deeper checks (rule integrity,
// relation resolvability, requires satisfaction) happen at load
// time against a live database.
func (sp *SchemaPackage) Validate() error {
	if sp.Name == "" {
		return fmt.Errorf("schema package: name is required")
	}
	if sp.Version == "" {
		return fmt.Errorf("schema package: version is required")
	}
	return nil
}
