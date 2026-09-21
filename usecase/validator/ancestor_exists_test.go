package validator

import (
	"strings"
	"testing"
)

func TestBuildAncestorExistsQuery_EqualsMatch(t *testing.T) {
	p := &ancestorParams{
		parentColumn: "col__parent_id",
		match:        ancestorMatch{field: "col__rank_id", operator: "equals", value: "GENUS"},
		maxDepth:     100,
	}
	got, args, err := buildAncestorExistsQuery(p, "taxon", "col__id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `WITH RECURSIVE chain(id, depth) AS (` +
		`SELECT "col__parent_id", 1 FROM "taxon" WHERE "col__id" = ? ` +
		`UNION SELECT "taxon"."col__parent_id", chain.depth + 1 FROM "taxon" JOIN chain ON "taxon"."col__id" = chain.id WHERE chain.depth < ?) ` +
		`SELECT COUNT(*) FROM chain c JOIN "taxon" t ON t."col__id" = c.id WHERE t."col__rank_id" = ?`
	if got != want {
		t.Errorf("SQL mismatch\n got: %s\nwant: %s", got, want)
	}
	if len(args) != 1 || args[0] != "GENUS" {
		t.Errorf("match args = %v, want [GENUS]", args)
	}
}

func TestBuildAncestorExistsQuery_InMatch(t *testing.T) {
	p := &ancestorParams{
		parentColumn: "col__parent_id",
		match: ancestorMatch{
			field:    "col__rank_id",
			operator: "in",
			value:    []interface{}{"SPECIES", "SUBSPECIES"},
		},
		maxDepth: 100,
	}
	got, args, err := buildAncestorExistsQuery(p, "taxon", "col__id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `t."col__rank_id" IN (?,?)`) {
		t.Errorf("expected IN placeholder pair, got: %s", got)
	}
	if len(args) != 2 || args[0] != "SPECIES" || args[1] != "SUBSPECIES" {
		t.Errorf("match args = %v", args)
	}
}

func TestBuildAncestorExistsQuery_NotInMatch(t *testing.T) {
	p := &ancestorParams{
		parentColumn: "col__parent_id",
		match: ancestorMatch{
			field:    "col__rank_id",
			operator: "not_in",
			value:    []interface{}{"SPECIES"},
		},
		maxDepth: 50,
	}
	got, _, err := buildAncestorExistsQuery(p, "taxon", "col__id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `NOT IN`) {
		t.Errorf("expected NOT IN in query, got: %s", got)
	}
}

func TestBuildAncestorExistsQuery_WithJoin(t *testing.T) {
	p := &ancestorParams{
		parentColumn: "col__parent_id",
		match: ancestorMatch{
			join:     &ancestorJoin{fromColumn: "col__name_id", toTable: "name", toColumn: "col__id"},
			field:    "col__rank_id",
			operator: "in",
			value:    []interface{}{"GENUS", "SUBGENUS"},
		},
		maxDepth: 100,
	}
	got, args, err := buildAncestorExistsQuery(p, "taxon", "col__id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `JOIN "name" j ON j."col__id" = t."col__name_id"`) {
		t.Errorf("expected name join, got: %s", got)
	}
	if !strings.Contains(got, `j."col__rank_id" IN (?,?)`) {
		t.Errorf("expected match on joined alias, got: %s", got)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, want 2 values", args)
	}
}

func TestBuildAncestorExistsQuery_UnsupportedOperator(t *testing.T) {
	p := &ancestorParams{
		parentColumn: "col__parent_id",
		match:        ancestorMatch{field: "col__rank_id", operator: "greater_than", value: 5},
		maxDepth:     100,
	}
	_, _, err := buildAncestorExistsQuery(p, "taxon", "col__id")
	if err == nil {
		t.Fatal("expected error on unsupported operator")
	}
}

func TestBuildAncestorExistsQuery_RejectsUnsafeIdentifiers(t *testing.T) {
	cases := []struct {
		name             string
		tbl, pk, pc, fld string
	}{
		{"table", `taxon"; DROP`, "col__id", "col__parent_id", "col__rank_id"},
		{"pk", "taxon", `col__id"; --`, "col__parent_id", "col__rank_id"},
		{"parent", "taxon", "col__id", `col__parent_id"; --`, "col__rank_id"},
		{"field", "taxon", "col__id", "col__parent_id", `col__rank_id"; --`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &ancestorParams{
				parentColumn: c.pc,
				match:        ancestorMatch{field: c.fld, operator: "equals", value: "X"},
				maxDepth:     10,
			}
			_, _, err := buildAncestorExistsQuery(p, c.tbl, c.pk)
			if err == nil {
				t.Fatalf("expected error on unsafe %s", c.name)
			}
		})
	}
}

func TestParseAncestorParams_Defaults(t *testing.T) {
	p, err := parseAncestorParams(map[string]interface{}{
		"parent_column": "col__parent_id",
		"match": map[string]interface{}{
			"field":    "col__rank_id",
			"operator": "equals",
			"value":    "GENUS",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.maxDepth != 100 {
		t.Errorf("default max_depth = %d, want 100", p.maxDepth)
	}
}

func TestParseAncestorParams_RejectsMissingFields(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"no parent_column": {"match": map[string]interface{}{"field": "x", "operator": "equals", "value": "y"}},
		"no match":         {"parent_column": "col__parent_id"},
		"no match field":   {"parent_column": "col__parent_id", "match": map[string]interface{}{"operator": "equals", "value": "y"}},
		"no match op":      {"parent_column": "col__parent_id", "match": map[string]interface{}{"field": "x", "value": "y"}},
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseAncestorParams(params); err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}
