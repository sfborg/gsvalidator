package usecase

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/sfborg/gsvalidator/domain"
)

// RequiresError enumerates every unmet requirement discovered by
// CheckRequires. Consumer applications surface these to the
// caller as a load-time failure so the bundle isn't applied
// against an incompatible schema.
type RequiresError struct {
	MissingTables    []string
	MissingColumns   []string // "table.column" strings
	TypeMismatches   []string // "table.column: expected <type>, got <type>"
	MissingRelations []string // relation names the bundle needs but the loaded catalog doesn't have
}

// Error implements the error interface. Concatenates the missing
// items into a readable multi-line message.
func (e *RequiresError) Error() string {
	var parts []string
	if len(e.MissingTables) > 0 {
		parts = append(parts, fmt.Sprintf("missing tables: %s", strings.Join(e.MissingTables, ", ")))
	}
	if len(e.MissingColumns) > 0 {
		parts = append(parts, fmt.Sprintf("missing columns: %s", strings.Join(e.MissingColumns, ", ")))
	}
	if len(e.TypeMismatches) > 0 {
		parts = append(parts, fmt.Sprintf("column type mismatches: %s", strings.Join(e.TypeMismatches, "; ")))
	}
	if len(e.MissingRelations) > 0 {
		parts = append(parts, fmt.Sprintf("missing relations: %s", strings.Join(e.MissingRelations, ", ")))
	}
	if len(parts) == 0 {
		return "requires check failed (unspecified)"
	}
	return "requires check failed: " + strings.Join(parts, "; ")
}

// HasIssues reports whether any requirement is unmet.
func (e *RequiresError) HasIssues() bool {
	return len(e.MissingTables) > 0 ||
		len(e.MissingColumns) > 0 ||
		len(e.TypeMismatches) > 0 ||
		len(e.MissingRelations) > 0
}

// CheckRequires verifies that the live database satisfies the
// bundle's Requires block. Returns nil when everything resolves
// (or when the bundle has no Requires). Returns a *RequiresError
// enumerating every unmet requirement otherwise.
//
// availableRelations is the set of relation names known to the
// consumer's relation catalog (the union of all loaded bundles'
// Relations blocks by the time this check runs). Pass nil / empty
// map when the bundle doesn't declare relation requirements.
//
// Type checks are loose: SQLite storage class comparison, so
// nullability / defaults / check constraints don't false-fail.
// Extra tables, columns, or relations in the live database or
// relation catalog are fine.
func CheckRequires(
	ctx context.Context,
	db *sql.DB,
	req *domain.Requires,
	availableRelations map[string]bool,
) error {
	if req.IsEmpty() {
		return nil
	}
	res := &RequiresError{}

	// Tables — one PRAGMA table_info call per required table
	// tells us both existence and column shape at once. Cache
	// the results so column checks below reuse the same lookup.
	tableCols := map[string]map[string]string{}
	tablesToCheck := map[string]bool{}
	for _, t := range req.Tables {
		tablesToCheck[t] = true
	}
	for tc := range parsedColumnTables(req.Columns) {
		tablesToCheck[tc] = true
	}
	for t := range tablesToCheck {
		cols, ok, err := introspectTable(ctx, db, t)
		if err != nil {
			return fmt.Errorf("requires check: introspect %s: %w", t, err)
		}
		if !ok {
			res.MissingTables = append(res.MissingTables, t)
			continue
		}
		tableCols[t] = cols
	}

	// Columns — parse the "table.column" key, look up the type
	// in the introspected column map from the pass above. Missing
	// table already surfaced above so the ok-check here just
	// skips to avoid double-reporting.
	for spec, expected := range req.Columns {
		tbl, col, ok := splitColumnSpec(spec)
		if !ok {
			res.MissingColumns = append(res.MissingColumns, spec+" (malformed)")
			continue
		}
		cols, ok := tableCols[tbl]
		if !ok {
			continue // missing table already reported
		}
		actual, ok := cols[col]
		if !ok {
			res.MissingColumns = append(res.MissingColumns, spec)
			continue
		}
		if !typesCompatible(expected, actual) {
			res.TypeMismatches = append(res.TypeMismatches,
				fmt.Sprintf("%s: expected %s, got %s", spec, expected, actual))
		}
	}

	// Relations — pure string lookup against the caller-supplied
	// set. Consumers assemble that set from the loaded bundles'
	// Relations blocks before invoking CheckRequires.
	for _, name := range req.Relations {
		if !availableRelations[name] {
			res.MissingRelations = append(res.MissingRelations, name)
		}
	}

	if res.HasIssues() {
		return res
	}
	return nil
}

// introspectTable reads the column list of a SQLite table via
// PRAGMA table_info. Returns (columns, true, nil) when the table
// exists, (nil, false, nil) when it doesn't, and (nil, false,
// error) on any other failure. Column type strings are lowercased
// for uniform comparison downstream.
func introspectTable(ctx context.Context, db *sql.DB, table string) (map[string]string, bool, error) {
	// PRAGMA can't be a parameterized query. Bounded input:
	// table names come from the ruleset author (implicitly
	// trusted) and are further filtered by parsedColumnTables /
	// req.Tables. Reject anything with a quote or semicolon as
	// defence-in-depth.
	if strings.ContainsAny(table, "'\";") {
		return nil, false, fmt.Errorf("unsafe table name: %q", table)
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%q)", table))
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	cols := map[string]string{}
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return nil, false, err
		}
		cols[name] = strings.ToLower(ctype)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(cols) == 0 {
		// PRAGMA on a missing table returns zero rows silently.
		return nil, false, nil
	}
	return cols, true, nil
}

// parsedColumnTables extracts the set of table names referenced
// by a Requires.Columns map so we can pre-introspect them in one
// pass.
func parsedColumnTables(columns map[string]string) map[string]bool {
	out := map[string]bool{}
	for spec := range columns {
		if tbl, _, ok := splitColumnSpec(spec); ok {
			out[tbl] = true
		}
	}
	return out
}

// splitColumnSpec parses "table.column". Rejects specs missing
// either half or containing more than one dot.
func splitColumnSpec(spec string) (table, column string, ok bool) {
	parts := strings.Split(spec, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// typesCompatible maps common type aliases onto SQLite storage
// classes before comparing. Loose: `int`, `integer`, `int64` all
// match `integer`; `text`, `varchar`, `string` all match `text`.
// SQLite's own type-affinity rules already collapse most of these
// at runtime, so this mirrors the actual on-disk semantics.
func typesCompatible(expected, actual string) bool {
	return normalizeType(expected) == normalizeType(actual)
}

func normalizeType(t string) string {
	lc := strings.ToLower(strings.TrimSpace(t))
	switch lc {
	case "text", "varchar", "string", "char", "clob":
		return "text"
	case "int", "integer", "int64", "int32", "bigint", "smallint":
		return "integer"
	case "real", "float", "double", "decimal", "numeric":
		return "real"
	case "blob", "bytes":
		return "blob"
	case "bool", "boolean":
		return "integer" // SQLite has no bool; stored as integer
	case "date", "datetime", "timestamp":
		return "text" // SQLite stores as ISO-8601 text
	case "":
		// PRAGMA table_info returns "" for columns declared with
		// no type. SQLite treats those as blob affinity.
		return "blob"
	}
	return lc
}
