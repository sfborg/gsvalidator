package validator

import (
	"strings"
	"testing"
)

func TestBuildCycleQuery_ShapesQuery(t *testing.T) {
	got, err := buildCycleQuery("taxon", "col__parent_id", "col__id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `WITH RECURSIVE chain(id) AS (` +
		`SELECT "col__parent_id" FROM "taxon" WHERE "col__id" = ? ` +
		`UNION SELECT "taxon"."col__parent_id" FROM "taxon" JOIN chain ON "taxon"."col__id" = chain.id ` +
		`WHERE "taxon"."col__parent_id" IS NOT NULL AND "taxon"."col__parent_id" != '') ` +
		`SELECT COUNT(*) FROM chain WHERE id = ?`
	if got != want {
		t.Errorf("cycle SQL mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildCycleQuery_RejectsUnsafeIdentifiers(t *testing.T) {
	cases := []struct {
		name            string
		table, pcl, pkc string
	}{
		{"table", `taxon"; DROP`, "col__parent_id", "col__id"},
		{"parent col", "taxon", `col__parent_id"; DROP`, "col__id"},
		{"pk col", "taxon", "col__parent_id", `col__id"; DROP`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := buildCycleQuery(c.table, c.pcl, c.pkc)
			if err == nil {
				t.Fatalf("expected error on unsafe %s", c.name)
			}
			if !strings.Contains(err.Error(), "unsafe") {
				t.Errorf("unexpected error message: %v", err)
			}
		})
	}
}
