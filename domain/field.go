package domain

// FieldType classifies a Field's storage / semantics. Values match
// the shipping bundle format; type names are intentionally SQLite
// storage-class-aligned (text / integer / real / blob) plus
// higher-level flavors (number/date/reference) that consumers may
// treat specially.
type FieldType string

const (
	FieldTypeText      FieldType = "text"
	FieldTypeNumber    FieldType = "number"
	FieldTypeInteger   FieldType = "integer"
	FieldTypeFloat     FieldType = "float"
	FieldTypeBoolean   FieldType = "boolean"
	FieldTypeDate      FieldType = "date"
	FieldTypeDateTime  FieldType = "datetime"
	FieldTypeReference FieldType = "reference"
	FieldTypeJSON      FieldType = "json"
)

// Field is a column definition in a Table. Enough shape for
// gsvalidator to introspect a schema and reason about types /
// references; consumer-app UI hints (display styles, calculated
// field engines, etc.) are ignored by the bundle loader since
// they aren't in the JSON shape gsvalidator cares about.
type Field struct {
	ID                    string    `json:"id,omitempty"`
	Name                  string    `json:"name"`
	DisplayName           string    `json:"display_name,omitempty"`
	Type                  FieldType `json:"field_type"`
	IsRequired            bool      `json:"is_required,omitempty"`
	IsUnique              bool      `json:"is_unique,omitempty"`
	IsSystem              bool      `json:"is_system,omitempty"`
	DefaultValue          string    `json:"default_value,omitempty"`
	ReferenceTableID      string    `json:"reference_table_id,omitempty"`
	ReferenceDisplayField string    `json:"reference_display_field,omitempty"`
	Description           string    `json:"description,omitempty"`
	SortOrder             int       `json:"sort_order,omitempty"`
}
