package validator

import (
	"strings"
	"testing"
)

func TestBuildCountQuery_SingleMatch(t *testing.T) {
	p := &countParams{
		table:   "name",
		matches: []matchPair{{target: "col__scientific_name", self: "col__scientific_name"}},
	}
	got, err := buildCountQuery(p, false, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT COUNT(*) FROM "name" WHERE "col__scientific_name" = ?`
	if got != want {
		t.Errorf("single-match SQL mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildCountQuery_MultiMatch(t *testing.T) {
	p := &countParams{
		table: "name",
		matches: []matchPair{
			{target: "col__scientific_name", self: "col__scientific_name"},
			{target: "col__authorship", self: "col__authorship"},
		},
	}
	got, err := buildCountQuery(p, false, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT COUNT(*) FROM "name" WHERE "col__scientific_name" = ? AND "col__authorship" = ?`
	if got != want {
		t.Errorf("multi-match SQL mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildCountQuery_ExcludeSelf(t *testing.T) {
	p := &countParams{
		table:   "name",
		matches: []matchPair{{target: "gn__canonical_simple", self: "gn__canonical_simple"}},
	}
	got, err := buildCountQuery(p, true, "col__id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT COUNT(*) FROM "name" WHERE "gn__canonical_simple" = ? AND "col__id" != ?`
	if got != want {
		t.Errorf("exclude_self SQL mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildCountQuery_ExcludeSelfRequiresPKColumn(t *testing.T) {
	p := &countParams{
		table:   "name",
		matches: []matchPair{{target: "col__scientific_name", self: "col__scientific_name"}},
	}
	_, err := buildCountQuery(p, true, "")
	if err == nil {
		t.Fatal("expected error when exclude_self set but pk column empty")
	}
	if !strings.Contains(err.Error(), "primary-key") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildCountQuery_RejectsUnsafeTable(t *testing.T) {
	p := &countParams{
		table:   `name"; DROP TABLE users --`,
		matches: []matchPair{{target: "col__id", self: "col__id"}},
	}
	_, err := buildCountQuery(p, false, "")
	if err == nil {
		t.Fatal("expected error on unsafe table identifier")
	}
}

func TestBuildCountQuery_RejectsUnsafeTargetColumn(t *testing.T) {
	p := &countParams{
		table:   "name",
		matches: []matchPair{{target: `col__id; SELECT *`, self: "col__id"}},
	}
	_, err := buildCountQuery(p, false, "")
	if err == nil {
		t.Fatal("expected error on unsafe match target column")
	}
}

func TestBuildCountQuery_RejectsUnsafePKColumn(t *testing.T) {
	p := &countParams{
		table:   "name",
		matches: []matchPair{{target: "col__id", self: "col__id"}},
	}
	_, err := buildCountQuery(p, true, `col__id"; DROP`)
	if err == nil {
		t.Fatal("expected error on unsafe pk column")
	}
}

func TestParseCountParams_MinimumFields(t *testing.T) {
	p := map[string]interface{}{
		"table": "name",
		"match": []interface{}{
			map[string]interface{}{"target": "col__id", "self": "col__id"},
		},
		"expected": map[string]interface{}{"max": float64(0)},
	}
	got, err := parseCountParams(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.table != "name" || len(got.matches) != 1 {
		t.Errorf("unexpected parse result: %+v", got)
	}
	if !got.expected.hasMax || got.expected.max != 0 {
		t.Errorf("expected max=0, got %+v", got.expected)
	}
}

func TestParseCountParams_ExpectedRequiresBound(t *testing.T) {
	p := map[string]interface{}{
		"table":    "name",
		"match":    []interface{}{map[string]interface{}{"target": "x", "self": "y"}},
		"expected": map[string]interface{}{},
	}
	_, err := parseCountParams(p)
	if err == nil {
		t.Fatal("expected error when neither min nor max set")
	}
}

func TestParseCountParams_MatchMustNotBeEmpty(t *testing.T) {
	p := map[string]interface{}{
		"table":    "name",
		"match":    []interface{}{},
		"expected": map[string]interface{}{"min": float64(1)},
	}
	_, err := parseCountParams(p)
	if err == nil {
		t.Fatal("expected error on empty match list")
	}
}

func TestBuildNeighborhoodQuery_SameTableExcludesSelf(t *testing.T) {
	p := &countParams{
		table:   "name",
		matches: []matchPair{{target: "gn__canonical_simple", self: "gn__canonical_simple"}},
	}
	got, err := buildNeighborhoodQuery(p, true, "col__id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT "col__id" FROM "name" WHERE "gn__canonical_simple" = ? AND "col__id" != ?`
	if got != want {
		t.Errorf("neighborhood SQL mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildNeighborhoodQuery_CrossTableNoExclude(t *testing.T) {
	p := &countParams{
		table: "type_material",
		matches: []matchPair{
			{target: "col__name_id", self: "col__id"},
		},
	}
	got, err := buildNeighborhoodQuery(p, false, "col__id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT "col__id" FROM "type_material" WHERE "col__name_id" = ?`
	if got != want {
		t.Errorf("cross-table neighborhood SQL mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildNeighborhoodQuery_RejectsUnsafePKColumn(t *testing.T) {
	p := &countParams{
		table:   "name",
		matches: []matchPair{{target: "col__id", self: "col__id"}},
	}
	_, err := buildNeighborhoodQuery(p, true, `col__id"; DROP`)
	if err == nil {
		t.Fatal("expected error on unsafe pk column")
	}
}

func TestFormatExpected(t *testing.T) {
	cases := []struct {
		b    countBounds
		want string
	}{
		{countBounds{min: 0, max: 0, hasMin: true, hasMax: true}, "exactly 0"},
		{countBounds{min: 1, max: 1, hasMin: true, hasMax: true}, "exactly 1"},
		{countBounds{min: 1, max: 5, hasMin: true, hasMax: true}, "between 1 and 5"},
		{countBounds{min: 1, hasMin: true}, "at least 1"},
		{countBounds{max: 3, hasMax: true}, "at most 3"},
	}
	for _, c := range cases {
		if got := formatExpected(c.b); got != c.want {
			t.Errorf("formatExpected(%+v) = %q, want %q", c.b, got, c.want)
		}
	}
}
