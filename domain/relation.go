package domain

// Relation is a named cross-table join declared in a
// SchemaPackage's Relations block. Rules reference relations by
// name (via validator parameters like `related_field_equals`'s
// `relation` key); the consumer's SchemaMapper resolves the join
// against the live database using the declared join chain.
//
// Constrained shape on purpose: each Join step is a bounded
// {from, to, alias?} tuple, no arbitrary WHERE clauses, no
// subqueries, no functions. This keeps rule-author-supplied
// relations bounded to safe compositions that a SQL translator
// can verify against a schema.
//
// Cardinality signals whether a rule using this relation should
// expect exactly one target row (`one`) or many (`many`). A
// `related_field_equals` mechanism needs `one`; a
// `duplicate_detection` mechanism might use `many`.
//
// TargetAlias is only needed when the target and source tables
// share a name (a self-join whose final step aliases the
// joined-in copy). When set, the SELECT projects the aliased row
// instead of the source. Blank in the common case where target
// and source are distinct tables.
type Relation struct {
	TargetTable string     `json:"target_table"`
	TargetAlias string     `json:"target_alias,omitempty"`
	Cardinality string     `json:"cardinality,omitempty"` // "one" (default) or "many"
	Join        []JoinStep `json:"join"`
	Description string     `json:"description,omitempty"`
}

// JoinStep is one edge of a relation's join chain. From/To are
// "<table_or_alias>.<column>" strings; Alias renames the joined
// table for later steps in the chain. Consumers walking a
// multi-step join thread aliases through the SQL they build.
//
// Where narrows the joined rows to those satisfying additional
// equality predicates on the just-joined table (or its alias).
// Each JoinFilter contributes one `AND <alias>.<column> = ?`
// clause to the ON clause of the step, with the constant passed
// as a bound parameter. Only equality against a constant is
// supported today — the constrained shape keeps rule-supplied
// joins safe by construction.
type JoinStep struct {
	From  string       `json:"from"`
	To    string       `json:"to"`
	Alias string       `json:"alias,omitempty"`
	Where []JoinFilter `json:"where,omitempty"`
}

// JoinFilter constrains a JoinStep to rows whose Column equals
// the given constant. Column names a field on the table that the
// step just joined in (identified by its alias if set, otherwise
// by its unaliased table name).
type JoinFilter struct {
	Column string      `json:"column"`
	Equals interface{} `json:"equals"`
}
