package validator

import (
	"strings"
	"testing"
)

func TestBuildRankCodeLookup_SelfNoJoin(t *testing.T) {
	got, err := buildRankCodeLookup("taxon", "col__id", nil,
		"col__rank_id", "col__code_id", false, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT t."col__rank_id", t."col__code_id" FROM "taxon" t WHERE t."col__id" = ?`
	if got != want {
		t.Errorf("SQL mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildRankCodeLookup_SelfWithJoin(t *testing.T) {
	j := &ancestorJoin{fromColumn: "col__name_id", toTable: "name", toColumn: "col__id"}
	got, err := buildRankCodeLookup("taxon", "col__id", j,
		"col__rank_id", "col__code_id", false, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT j."col__rank_id", j."col__code_id" FROM "taxon" t JOIN "name" j ON j."col__id" = t."col__name_id" WHERE t."col__id" = ?`
	if got != want {
		t.Errorf("SQL mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildRankCodeLookup_ParentWithJoin(t *testing.T) {
	j := &ancestorJoin{fromColumn: "col__name_id", toTable: "name", toColumn: "col__id"}
	got, err := buildRankCodeLookup("taxon", "col__id", j,
		"col__rank_id", "col__code_id", true, "col__parent_id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `JOIN "taxon" p ON p."col__id" = t."col__parent_id"`) {
		t.Errorf("expected parent hop, got: %s", got)
	}
	if !strings.Contains(got, `JOIN "name" j ON j."col__id" = p."col__name_id"`) {
		t.Errorf("expected join from parent alias, got: %s", got)
	}
	if !strings.Contains(got, `SELECT j."col__rank_id", j."col__code_id"`) {
		t.Errorf("expected select via join alias, got: %s", got)
	}
}

func TestBuildRankCodeLookup_RejectsUnsafeIdentifiers(t *testing.T) {
	cases := []struct {
		name, tbl, pk, rank, code, pcol string
		parent                          bool
	}{
		{"table", `tx"; DROP`, "col__id", "col__rank_id", "col__code_id", "", false},
		{"pk", "taxon", `col__id"; --`, "col__rank_id", "col__code_id", "", false},
		{"rank", "taxon", "col__id", `col__rank_id"; --`, "col__code_id", "", false},
		{"code", "taxon", "col__id", "col__rank_id", `col__code_id"; --`, "", false},
		{"parent col", "taxon", "col__id", "col__rank_id", "col__code_id", `col__parent_id"; --`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := buildRankCodeLookup(c.tbl, c.pk, nil, c.rank, c.code, c.parent, c.pcol)
			if err == nil {
				t.Fatalf("expected error on unsafe %s", c.name)
			}
		})
	}
}

func TestParseParentRankHigherParams_MinimumFields(t *testing.T) {
	p, err := parseParentRankHigherParams(map[string]interface{}{
		"parent_column": "col__parent_id",
		"rank_column":   "col__rank_id",
		"code_column":   "col__code_id",
		"rank_order": map[string]interface{}{
			"ZOOLOGICAL": []interface{}{"KINGDOM", "PHYLUM", "SPECIES"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.rankOrder["ZOOLOGICAL"]) != 3 {
		t.Errorf("unexpected rank_order: %+v", p.rankOrder)
	}
}

func TestParseParentRankHigherParams_RejectsBadShapes(t *testing.T) {
	base := func() map[string]interface{} {
		return map[string]interface{}{
			"parent_column": "col__parent_id",
			"rank_column":   "col__rank_id",
			"code_column":   "col__code_id",
			"rank_order": map[string]interface{}{
				"ZOOLOGICAL": []interface{}{"KINGDOM", "SPECIES"},
			},
		}
	}
	cases := map[string]func(map[string]interface{}){
		"missing parent_column": func(p map[string]interface{}) { delete(p, "parent_column") },
		"missing rank_column":   func(p map[string]interface{}) { delete(p, "rank_column") },
		"missing code_column":   func(p map[string]interface{}) { delete(p, "code_column") },
		"missing rank_order":    func(p map[string]interface{}) { delete(p, "rank_order") },
		"empty rank_order":      func(p map[string]interface{}) { p["rank_order"] = map[string]interface{}{} },
		"empty rank list": func(p map[string]interface{}) {
			p["rank_order"] = map[string]interface{}{"ZOOLOGICAL": []interface{}{}}
		},
		"non-string rank in list": func(p map[string]interface{}) {
			p["rank_order"] = map[string]interface{}{"ZOOLOGICAL": []interface{}{"KINGDOM", 42}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := base()
			mutate(p)
			if _, err := parseParentRankHigherParams(p); err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}

func TestIndexOfString(t *testing.T) {
	list := []string{"A", "B", "C"}
	if indexOfString(list, "A") != 0 {
		t.Errorf("A → %d, want 0", indexOfString(list, "A"))
	}
	if indexOfString(list, "C") != 2 {
		t.Errorf("C → %d, want 2", indexOfString(list, "C"))
	}
	if indexOfString(list, "Z") != -1 {
		t.Errorf("Z → %d, want -1", indexOfString(list, "Z"))
	}
}
