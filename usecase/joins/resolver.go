package joins

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/sfborg/gsvalidator/domain"
)

// PrimaryKeyProvider tells the resolver which column holds the
// primary key for a given table. Consumers usually pass their
// SchemaMapper — the same method signature is intentional so it
// satisfies this interface without adapters.
type PrimaryKeyProvider interface {
	GetPrimaryKeyField(tableName string) string
}

// RelationResolver executes named relations declared in a
// bundle's Relations block. Given a source table +
// primary-key id + relation name, it walks the declared join
// chain and returns the target row(s).
//
// Constrained by design: each join step is a plain "left column
// equals right column" join. No arbitrary WHERE clauses, no
// subqueries, no functions. This keeps rule-author-supplied
// relations bounded to safe compositions.
//
// The resolver is stateless beyond its relation map and PK
// provider. Consumers construct one at bundle-load time and pass
// it to validators that need cross-table access.
type RelationResolver struct {
	relations  map[string]domain.Relation
	pkProvider PrimaryKeyProvider
}

// NewRelationResolver builds a resolver over a relations map and
// a PK provider (typically the consumer's SchemaMapper). Nil
// relations map is fine — the resolver just fails every lookup
// with an "unknown relation" error. A nil pkProvider is a
// programmer error: the resolver panics rather than emit SQL
// with an unquoted empty column name.
func NewRelationResolver(relations map[string]domain.Relation, pkProvider PrimaryKeyProvider) *RelationResolver {
	if pkProvider == nil {
		panic("joins.NewRelationResolver: pkProvider must not be nil")
	}
	if relations == nil {
		relations = map[string]domain.Relation{}
	}
	return &RelationResolver{relations: relations, pkProvider: pkProvider}
}

// Has reports whether a relation with the given name is
// declared. Cheap — used by Requires-block checks that verify
// the resolver knows every relation a bundle needs.
func (r *RelationResolver) Has(name string) bool {
	_, ok := r.relations[name]
	return ok
}

// Names returns every declared relation name. Used by
// consumers building an availableRelations map for
// CheckRequires.
func (r *RelationResolver) Names() []string {
	out := make([]string, 0, len(r.relations))
	for name := range r.relations {
		out = append(out, name)
	}
	return out
}

// LookupRelated satisfies domain.ConditionResolver. Delegates to
// ResolveOne so relation-scoped rule conditions can call through
// the same code path validators use for join traversal.
func (r *RelationResolver) LookupRelated(ctx context.Context, db *sql.DB, sourceTable, sourceID, relation string) (map[string]interface{}, error) {
	return r.ResolveOne(ctx, db, sourceTable, sourceID, relation)
}

// ResolveOne executes a one-cardinality relation from
// (sourceTable, sourceID) and returns the single target row.
// Returns (nil, nil) when the join yields no row — root-taxon
// asking for parent, orphan record — so callers can treat that
// as "no data" without an error path.
//
// Errors on: unknown relation name, cardinality mismatch (the
// relation was declared "many"), malformed join spec, or SQL
// failure.
func (r *RelationResolver) ResolveOne(ctx context.Context, db *sql.DB, sourceTable, sourceID, relationName string) (map[string]interface{}, error) {
	rel, ok := r.relations[relationName]
	if !ok {
		return nil, fmt.Errorf("relation %q not declared", relationName)
	}
	if rel.Cardinality == "many" {
		return nil, fmt.Errorf("relation %q is many-cardinality; use ResolveMany", relationName)
	}
	pkCol := r.pkProvider.GetPrimaryKeyField(sourceTable)
	query, filterArgs, err := buildRelationQuery(rel, sourceTable, pkCol)
	if err != nil {
		return nil, fmt.Errorf("relation %q: %w", relationName, err)
	}
	query += " LIMIT 1"
	args := append(filterArgs, sourceID)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("relation %q: query: %w", relationName, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	return scanRowToMap(rows)
}

// ResolveMany executes a many-cardinality relation and returns
// every target row. Empty slice + nil error when the relation
// yields no rows.
func (r *RelationResolver) ResolveMany(ctx context.Context, db *sql.DB, sourceTable, sourceID, relationName string) ([]map[string]interface{}, error) {
	rel, ok := r.relations[relationName]
	if !ok {
		return nil, fmt.Errorf("relation %q not declared", relationName)
	}
	pkCol := r.pkProvider.GetPrimaryKeyField(sourceTable)
	query, filterArgs, err := buildRelationQuery(rel, sourceTable, pkCol)
	if err != nil {
		return nil, fmt.Errorf("relation %q: %w", relationName, err)
	}
	args := append(filterArgs, sourceID)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("relation %q: query: %w", relationName, err)
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		m, err := scanRowToMap(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// buildRelationQuery renders a Relation into parameterized SQL.
// Supports arbitrary-length join chains.
//
// Semantic per join step:
//
//   - from: "<in-scope-table-or-alias>.<column>" — references the
//     source table (for the first step) or an alias declared by a
//     previous step
//   - to:   "<real-table>.<column>" — the newly-joined table, named
//     by its real (unaliased) name
//   - alias: optional rename for the just-joined table, letting
//     later steps reference it
//   - where: optional list of `<alias>.<column> = ?` filters that
//     narrow the joined rows to a constant match
//
// Generated shape:
//
//	SELECT <target>.* FROM <source>
//	JOIN <step1.to.table> [AS <step1.alias>] ON <step1.from> = <step1_alias>.<step1.to.col> [AND <step1_alias>.<col> = ?]
//	JOIN <step2.to.table> [AS <step2.alias>] ON <step2.from> = <step2_alias>.<step2.to.col> [AND <step2_alias>.<col> = ?]
//	...
//	WHERE <source>.<sourcePKCol> = ?
//
// The first bound parameter is the sourceID; any additional
// parameters are the JoinFilter Equals values in step order.
// When Relation.TargetAlias is set, the SELECT projects that
// alias instead of the target table's real name — required for
// self-joins where source and target share a table name.
// Identifier quoting via safeIdent (allow-list of alnum +
// underscore) keeps column/table/alias names safe, including the
// PK column name.
func buildRelationQuery(rel domain.Relation, sourceTable, sourcePKCol string) (string, []interface{}, error) {
	if rel.TargetTable == "" {
		return "", nil, fmt.Errorf("target_table required")
	}
	if len(rel.Join) == 0 {
		return "", nil, fmt.Errorf("at least one join step required")
	}
	if sourcePKCol == "" {
		return "", nil, fmt.Errorf("source primary-key column empty for table %q", sourceTable)
	}
	if err := safeIdent(rel.TargetTable); err != nil {
		return "", nil, err
	}
	if err := safeIdent(sourceTable); err != nil {
		return "", nil, err
	}
	if err := safeIdent(sourcePKCol); err != nil {
		return "", nil, err
	}
	if rel.TargetAlias != "" {
		if err := safeIdent(rel.TargetAlias); err != nil {
			return "", nil, err
		}
	}
	for i, step := range rel.Join {
		if err := safeJoinStep(step); err != nil {
			return "", nil, fmt.Errorf("join step %d: %w", i, err)
		}
	}

	selectRef := rel.TargetTable
	if rel.TargetAlias != "" {
		selectRef = rel.TargetAlias
	}
	var sb strings.Builder
	var filterArgs []interface{}
	sb.WriteString("SELECT ")
	sb.WriteString(quoteIdent(selectRef))
	sb.WriteString(".* FROM ")
	sb.WriteString(quoteIdent(sourceTable))
	for _, step := range rel.Join {
		fromTable, fromCol, _ := splitStepRef(step.From)
		toTable, toCol, _ := splitStepRef(step.To)
		alias := step.Alias
		joinRef := toTable
		if alias != "" {
			joinRef = alias
		}
		sb.WriteString(" JOIN ")
		sb.WriteString(quoteIdent(toTable))
		if alias != "" {
			sb.WriteString(" AS ")
			sb.WriteString(quoteIdent(alias))
		}
		sb.WriteString(" ON ")
		sb.WriteString(quoteIdent(fromTable))
		sb.WriteString(".")
		sb.WriteString(quoteIdent(fromCol))
		sb.WriteString(" = ")
		sb.WriteString(quoteIdent(joinRef))
		sb.WriteString(".")
		sb.WriteString(quoteIdent(toCol))
		for _, f := range step.Where {
			sb.WriteString(" AND ")
			sb.WriteString(quoteIdent(joinRef))
			sb.WriteString(".")
			sb.WriteString(quoteIdent(f.Column))
			sb.WriteString(" = ?")
			filterArgs = append(filterArgs, f.Equals)
		}
	}
	sb.WriteString(" WHERE ")
	sb.WriteString(quoteIdent(sourceTable))
	sb.WriteString(".")
	sb.WriteString(quoteIdent(sourcePKCol))
	sb.WriteString(" = ?")
	return sb.String(), filterArgs, nil
}

// splitStepRef parses "table.column" into its parts.
func splitStepRef(s string) (string, string, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("join step reference %q not in table.column form", s)
	}
	return parts[0], parts[1], nil
}

// safeIdent rejects identifiers that would break identifier
// quoting or enable SQL injection. Conservative allow-list:
// letters, digits, underscore.
func safeIdent(s string) error {
	if s == "" {
		return fmt.Errorf("identifier empty")
	}
	for _, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return fmt.Errorf("identifier %q contains unsafe character %q", s, r)
	}
	return nil
}

// safeJoinStep validates the identifiers named by a JoinStep.
func safeJoinStep(step domain.JoinStep) error {
	fromTable, fromCol, err := splitStepRef(step.From)
	if err != nil {
		return err
	}
	toTable, toCol, err := splitStepRef(step.To)
	if err != nil {
		return err
	}
	for _, part := range []string{fromTable, fromCol, toTable, toCol} {
		if err := safeIdent(part); err != nil {
			return err
		}
	}
	if step.Alias != "" {
		if err := safeIdent(step.Alias); err != nil {
			return err
		}
	}
	for _, f := range step.Where {
		if err := safeIdent(f.Column); err != nil {
			return err
		}
	}
	return nil
}

// quoteIdent wraps an identifier in double quotes. Callers must
// ensure the identifier passed safeIdent first.
func quoteIdent(s string) string {
	return `"` + s + `"`
}

// scanRowToMap converts a *sql.Rows current row into a
// map[column]value. Handles the common []byte → string coercion
// SQLite emits.
func scanRowToMap(rows *sql.Rows) (map[string]interface{}, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	values := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	out := make(map[string]interface{}, len(cols))
	for i, name := range cols {
		v := values[i]
		if b, ok := v.([]byte); ok {
			out[name] = string(b)
		} else {
			out[name] = v
		}
	}
	return out, nil
}
