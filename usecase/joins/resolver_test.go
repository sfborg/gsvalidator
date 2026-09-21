package joins

import (
	"strings"
	"testing"

	"github.com/sfborg/gsvalidator/domain"
)

func TestBuildRelationQuery_SingleStep(t *testing.T) {
	rel := domain.Relation{
		TargetTable: "name",
		Cardinality: "one",
		Join: []domain.JoinStep{
			{From: "taxon.col__name_id", To: "name.col__id"},
		},
	}
	got, args, err := buildRelationQuery(rel, "taxon", "col__id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT "name".* FROM "taxon"` +
		` JOIN "name" ON "taxon"."col__name_id" = "name"."col__id"` +
		` WHERE "taxon"."col__id" = ?`
	if got != want {
		t.Errorf("single-step SQL mismatch\n got: %s\nwant: %s", got, want)
	}
	if len(args) != 0 {
		t.Errorf("expected no filter args, got %v", args)
	}
}

func TestBuildRelationQuery_MultiStepWithAlias(t *testing.T) {
	// The canonical "parent's owning name" traversal: from a
	// source taxon, walk to its parent taxon (self-join,
	// aliased), then walk to the parent's owning name row.
	rel := domain.Relation{
		TargetTable: "name",
		Cardinality: "one",
		Join: []domain.JoinStep{
			{From: "taxon.col__parent_id", To: "taxon.col__id", Alias: "parent"},
			{From: "parent.col__name_id", To: "name.col__id"},
		},
	}
	got, _, err := buildRelationQuery(rel, "taxon", "col__id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT "name".* FROM "taxon"` +
		` JOIN "taxon" AS "parent" ON "taxon"."col__parent_id" = "parent"."col__id"` +
		` JOIN "name" ON "parent"."col__name_id" = "name"."col__id"` +
		` WHERE "taxon"."col__id" = ?`
	if got != want {
		t.Errorf("multi-step SQL mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildRelationQuery_CustomPKColumn(t *testing.T) {
	// Non-sfga consumers may name their PK differently; verify
	// the WHERE clause honors what the caller supplies.
	rel := domain.Relation{
		TargetTable: "name",
		Cardinality: "one",
		Join: []domain.JoinStep{
			{From: "taxon.name_uuid", To: "name.uuid"},
		},
	}
	got, _, err := buildRelationQuery(rel, "taxon", "uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT "name".* FROM "taxon"` +
		` JOIN "name" ON "taxon"."name_uuid" = "name"."uuid"` +
		` WHERE "taxon"."uuid" = ?`
	if got != want {
		t.Errorf("custom-pk SQL mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildRelationQuery_StepWithWhereFilter(t *testing.T) {
	// A join step that narrows the joined rows to a constant
	// match. The Where predicate contributes an "AND alias.col
	// = ?" to the ON clause and a bound argument in step order,
	// ahead of the WHERE-clause sourceID argument.
	rel := domain.Relation{
		TargetTable: "name_relation",
		Cardinality: "one",
		Join: []domain.JoinStep{
			{
				From: "name.col__id",
				To:   "name_relation.col__name_id",
				Where: []domain.JoinFilter{
					{Column: "col__type_id", Equals: "BASIONYM"},
				},
			},
		},
	}
	got, args, err := buildRelationQuery(rel, "name", "col__id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT "name_relation".* FROM "name"` +
		` JOIN "name_relation" ON "name"."col__id" = "name_relation"."col__name_id"` +
		` AND "name_relation"."col__type_id" = ?` +
		` WHERE "name"."col__id" = ?`
	if got != want {
		t.Errorf("filtered SQL mismatch\n got: %s\nwant: %s", got, want)
	}
	if len(args) != 1 || args[0] != "BASIONYM" {
		t.Errorf("filter args = %v, want [BASIONYM]", args)
	}
}

func TestBuildRelationQuery_TargetAliasForSelfJoin(t *testing.T) {
	// name → name via name_relation. The final step aliases the
	// joined name row so it doesn't collide with the source's
	// "name" identifier. TargetAlias tells the SELECT to project
	// the aliased row.
	rel := domain.Relation{
		TargetTable: "name",
		TargetAlias: "basionym_name",
		Cardinality: "one",
		Join: []domain.JoinStep{
			{
				From: "name.col__id",
				To:   "name_relation.col__name_id",
				Where: []domain.JoinFilter{
					{Column: "col__type_id", Equals: "BASIONYM"},
				},
			},
			{
				From:  "name_relation.col__related_name_id",
				To:    "name.col__id",
				Alias: "basionym_name",
			},
		},
	}
	got, args, err := buildRelationQuery(rel, "name", "col__id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT "basionym_name".* FROM "name"` +
		` JOIN "name_relation" ON "name"."col__id" = "name_relation"."col__name_id"` +
		` AND "name_relation"."col__type_id" = ?` +
		` JOIN "name" AS "basionym_name" ON "name_relation"."col__related_name_id" = "basionym_name"."col__id"` +
		` WHERE "name"."col__id" = ?`
	if got != want {
		t.Errorf("self-join SQL mismatch\n got: %s\nwant: %s", got, want)
	}
	if len(args) != 1 || args[0] != "BASIONYM" {
		t.Errorf("filter args = %v, want [BASIONYM]", args)
	}
}

func TestBuildRelationQuery_RejectsUnsafeWhereColumn(t *testing.T) {
	rel := domain.Relation{
		TargetTable: "name_relation",
		Join: []domain.JoinStep{
			{
				From: "name.col__id",
				To:   "name_relation.col__name_id",
				Where: []domain.JoinFilter{
					{Column: `col__type_id"; DROP TABLE users --`, Equals: "BASIONYM"},
				},
			},
		},
	}
	_, _, err := buildRelationQuery(rel, "name", "col__id")
	if err == nil {
		t.Fatal("expected error on unsafe where column, got nil")
	}
}

func TestBuildRelationQuery_RejectsMalformedRef(t *testing.T) {
	rel := domain.Relation{
		TargetTable: "name",
		Join: []domain.JoinStep{
			{From: "taxon_col__name_id", To: "name.col__id"}, // missing dot
		},
	}
	_, _, err := buildRelationQuery(rel, "taxon", "col__id")
	if err == nil {
		t.Fatal("expected error on malformed from ref, got nil")
	}
	if !strings.Contains(err.Error(), "table.column form") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBuildRelationQuery_RejectsUnsafeIdentifier(t *testing.T) {
	rel := domain.Relation{
		TargetTable: `name"; DROP TABLE users --`,
		Join: []domain.JoinStep{
			{From: "taxon.col__name_id", To: "name.col__id"},
		},
	}
	_, _, err := buildRelationQuery(rel, "taxon", "col__id")
	if err == nil {
		t.Fatal("expected error on unsafe target table, got nil")
	}
}

func TestBuildRelationQuery_RejectsUnsafePKColumn(t *testing.T) {
	rel := domain.Relation{
		TargetTable: "name",
		Join: []domain.JoinStep{
			{From: "taxon.col__name_id", To: "name.col__id"},
		},
	}
	_, _, err := buildRelationQuery(rel, "taxon", `col__id"; DROP TABLE users --`)
	if err == nil {
		t.Fatal("expected error on unsafe pk column, got nil")
	}
}

func TestBuildRelationQuery_RejectsEmptyPKColumn(t *testing.T) {
	rel := domain.Relation{
		TargetTable: "name",
		Join: []domain.JoinStep{
			{From: "taxon.col__name_id", To: "name.col__id"},
		},
	}
	_, _, err := buildRelationQuery(rel, "taxon", "")
	if err == nil {
		t.Fatal("expected error on empty pk column, got nil")
	}
}

func TestBuildRelationQuery_RejectsEmpty(t *testing.T) {
	_, _, err := buildRelationQuery(domain.Relation{TargetTable: "name"}, "taxon", "col__id")
	if err == nil {
		t.Fatal("expected error on empty join list, got nil")
	}
}
