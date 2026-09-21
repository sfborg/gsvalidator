package validator

import (
	"strings"
	"testing"
)

func TestBuildSelfValueQuery_NoJoin(t *testing.T) {
	got, err := buildSelfValueQuery("taxon", "col__id", nil, "col__scrutinizer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT t."col__scrutinizer" FROM "taxon" t WHERE t."col__id" = ?`
	if got != want {
		t.Errorf("SQL mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildSelfValueQuery_WithJoin(t *testing.T) {
	j := &ancestorJoin{fromColumn: "col__name_id", toTable: "name", toColumn: "col__id"}
	got, err := buildSelfValueQuery("taxon", "col__id", j, "col__authorship")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT sj."col__authorship" FROM "taxon" t JOIN "name" sj ON sj."col__id" = t."col__name_id" WHERE t."col__id" = ?`
	if got != want {
		t.Errorf("SQL mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildAncestorValueQuery_BothJoinsPresent(t *testing.T) {
	// A rule that checks the authorship-year assertion between self
	// and a specific-rank ancestor, both reached through the same
	// name join.
	p := &ancestorFieldCheckParams{
		parentColumn: "col__parent_id",
		stopWhen: ancestorMatch{
			join:     &ancestorJoin{fromColumn: "col__name_id", toTable: "name", toColumn: "col__id"},
			field:    "col__rank_id",
			operator: "equals",
			value:    "GENUS",
		},
		assert: ancestorAssert{
			ancestorJoin:  &ancestorJoin{fromColumn: "col__name_id", toTable: "name", toColumn: "col__id"},
			ancestorField: "col__published_in_year",
			selfField:     "col__published_in_year",
			operator:      "greater_than_or_equals",
		},
		maxDepth: 100,
	}
	got, args, err := buildAncestorValueQuery(p, "taxon", "col__id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `SELECT aj."col__published_in_year"`) {
		t.Errorf("expected SELECT via aj alias, got: %s", got)
	}
	if !strings.Contains(got, `JOIN "name" sj ON sj."col__id" = a."col__name_id"`) {
		t.Errorf("expected stop-when join alias sj, got: %s", got)
	}
	if !strings.Contains(got, `JOIN "name" aj ON aj."col__id" = a."col__name_id"`) {
		t.Errorf("expected ancestor-side join alias aj, got: %s", got)
	}
	if !strings.Contains(got, `sj."col__rank_id" = ?`) {
		t.Errorf("expected stop-when predicate against sj, got: %s", got)
	}
	if !strings.Contains(got, `ORDER BY c.depth LIMIT 1`) {
		t.Errorf("expected ordered LIMIT 1, got: %s", got)
	}
	if len(args) != 1 || args[0] != "GENUS" {
		t.Errorf("args = %v, want [GENUS]", args)
	}
}

func TestCompareFieldValues(t *testing.T) {
	cases := []struct {
		name string
		a, b interface{}
		op   string
		want bool
		err  bool
	}{
		{"equal strings", "abc", "abc", "equals", true, false},
		{"unequal strings", "abc", "def", "equals", false, false},
		{"not_equals opposite", "abc", "def", "not_equals", true, false},
		{"less_than", "1", "2", "less_than", true, false},
		{"less_than_or_equals equal", 5, 5, "less_than_or_equals", true, false},
		{"greater_than_or_equals equal", 5, 5, "greater_than_or_equals", true, false},
		{"greater_than", int64(10), int64(3), "greater_than", true, false},
		{"non-numeric numeric compare", "abc", "def", "less_than", false, true},
		{"nil equals nil", nil, nil, "equals", true, false},
		{"nil vs non-nil", nil, "x", "equals", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := compareFieldValues(c.a, c.b, c.op)
			if c.err {
				if err == nil {
					t.Errorf("expected error, got none (result=%v)", got)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestParseAncestorFieldCheckParams_MinimumFields(t *testing.T) {
	p, err := parseAncestorFieldCheckParams(map[string]interface{}{
		"parent_column": "col__parent_id",
		"stop_when": map[string]interface{}{
			"field":    "col__rank_id",
			"operator": "equals",
			"value":    "GENUS",
		},
		"assert": map[string]interface{}{
			"self_field":     "col__year",
			"ancestor_field": "col__year",
			"operator":       "greater_than_or_equals",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.parentColumn != "col__parent_id" || p.assert.operator != "greater_than_or_equals" {
		t.Errorf("unexpected params: %+v", p)
	}
	if p.requireAncestor {
		t.Errorf("require_ancestor default should be false")
	}
	if p.maxDepth != 100 {
		t.Errorf("max_depth default should be 100, got %d", p.maxDepth)
	}
}

func TestParseAncestorFieldCheckParams_RejectsMissingFields(t *testing.T) {
	base := func() map[string]interface{} {
		return map[string]interface{}{
			"parent_column": "col__parent_id",
			"stop_when": map[string]interface{}{
				"field": "col__rank_id", "operator": "equals", "value": "X",
			},
			"assert": map[string]interface{}{
				"self_field": "x", "ancestor_field": "y", "operator": "equals",
			},
		}
	}
	cases := map[string]func(map[string]interface{}){
		"no parent_column": func(p map[string]interface{}) { delete(p, "parent_column") },
		"no stop_when":     func(p map[string]interface{}) { delete(p, "stop_when") },
		"no assert":        func(p map[string]interface{}) { delete(p, "assert") },
		"no self_field":    func(p map[string]interface{}) { delete(p["assert"].(map[string]interface{}), "self_field") },
		"no ancestor_field": func(p map[string]interface{}) {
			delete(p["assert"].(map[string]interface{}), "ancestor_field")
		},
		"no assert op": func(p map[string]interface{}) {
			delete(p["assert"].(map[string]interface{}), "operator")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := base()
			mutate(p)
			if _, err := parseAncestorFieldCheckParams(p); err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}
