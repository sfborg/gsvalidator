package domain

// Table is a table definition in a SchemaPackage. Enough shape for
// gsvalidator to reason about a schema (name, columns, primary key)
// without carrying the app-specific metadata (timestamps, UI
// display hints, standards-body tags) that consumer applications
// track internally. JSON tags match the shipping bundle format;
// unknown fields on incoming JSON are ignored.
type Table struct {
	ID          string  `json:"id,omitempty"`
	Name        string  `json:"name"`
	DisplayName string  `json:"display_name,omitempty"`
	Description string  `json:"description,omitempty"`
	IsSystem    bool    `json:"is_system,omitempty"`
	IsLookup    bool    `json:"is_lookup,omitempty"`
	Fields      []Field `json:"fields"`
}

// GetField finds a field by name. Returns nil when no field matches.
func (t *Table) GetField(name string) *Field {
	for i := range t.Fields {
		if t.Fields[i].Name == name {
			return &t.Fields[i]
		}
	}
	return nil
}
