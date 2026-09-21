package domain

// Requires declares the schema features a bundle needs at load
// time — tables, columns (with expected type), and named
// relations. Consumers verify each item against the live database
// before activating the bundle's rules; missing items are
// per-item errors. Additive-compatible on the archive side: extra
// tables, columns, or relations don't fail the check.
//
// Type checks on columns are loose (matched against SQLite
// storage classes). This avoids false-fails on unrelated schema
// tweaks (nullability, default value, CHECK constraints) that
// don't affect whether the ruleset works.
type Requires struct {
	Tables    []string          `json:"tables,omitempty"`
	Columns   map[string]string `json:"columns,omitempty"`   // "table.column" -> "text|integer|real|blob|numeric"
	Relations []string          `json:"relations,omitempty"` // named relations that must resolve
}

// IsEmpty reports whether the Requires block declares no
// requirements. Empty is valid — a schema-defining package that
// depends on nothing existing beforehand has no Requires block
// (or an explicitly empty one).
func (r *Requires) IsEmpty() bool {
	if r == nil {
		return true
	}
	return len(r.Tables) == 0 && len(r.Columns) == 0 && len(r.Relations) == 0
}
