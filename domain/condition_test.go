package domain

import "testing"

func TestResolveConditionValue_LiteralPassThrough(t *testing.T) {
	rec := map[string]interface{}{"a": "x"}
	cases := []interface{}{
		"literal",
		42,
		3.14,
		[]interface{}{"a", "b"},
		nil,
	}
	for _, c := range cases {
		got := resolveConditionValue(c, rec)
		if got != nil && c != nil {
			if !equal(got, c) {
				t.Errorf("literal %v mangled: got %v", c, got)
			}
		}
	}
}

func TestResolveConditionValue_FieldSigil(t *testing.T) {
	rec := map[string]interface{}{"a": "x", "b": 42}
	got := resolveConditionValue(map[string]interface{}{"field": "a"}, rec)
	if got != "x" {
		t.Errorf("got %v, want %q", got, "x")
	}
	got = resolveConditionValue(map[string]interface{}{"field": "b"}, rec)
	if got != 42 {
		t.Errorf("got %v, want %d", got, 42)
	}
}

func TestResolveConditionValue_MissingFieldReturnsNil(t *testing.T) {
	rec := map[string]interface{}{"a": "x"}
	got := resolveConditionValue(map[string]interface{}{"field": "missing"}, rec)
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestResolveConditionValue_MapWithoutFieldKeyPassesThrough(t *testing.T) {
	rec := map[string]interface{}{"a": "x"}
	in := map[string]interface{}{"other": "value"}
	got := resolveConditionValue(in, rec)
	// Should return the map itself unchanged.
	m, ok := got.(map[string]interface{})
	if !ok || m["other"] != "value" {
		t.Errorf("got %v, want original map", got)
	}
}

func TestResolveConditionValue_FieldWithSiblingKeysPassesThrough(t *testing.T) {
	// A map with "field" plus other keys is a literal, not a sigil.
	rec := map[string]interface{}{"a": "x"}
	in := map[string]interface{}{"field": "a", "flavor": "extra"}
	got := resolveConditionValue(in, rec)
	m, ok := got.(map[string]interface{})
	if !ok || m["field"] != "a" || m["flavor"] != "extra" {
		t.Errorf("got %v, want original map (sigil requires single-key map)", got)
	}
}

func TestResolveConditionValue_EmptyFieldPassesThrough(t *testing.T) {
	rec := map[string]interface{}{"a": "x"}
	in := map[string]interface{}{"field": ""}
	got := resolveConditionValue(in, rec)
	m, ok := got.(map[string]interface{})
	if !ok {
		t.Errorf("empty field name should treat map as literal; got %v", got)
	}
	if m["field"] != "" {
		t.Errorf("map unexpectedly mutated: %v", got)
	}
}

func TestCondition_EqualsWithSigilFiresOnMatch(t *testing.T) {
	// The classic self-reference case: name_id equals related_name_id
	// on the same row = self-referenced relation.
	c := &Condition{
		Field:    "name_id",
		Operator: "equals",
		Value:    map[string]interface{}{"field": "related_name_id"},
	}
	vctx := &ValidationContext{
		Record: map[string]interface{}{
			"name_id":         "abc",
			"related_name_id": "abc",
		},
	}
	ok, err := c.Eval(vctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !ok {
		t.Errorf("expected condition to fire when two fields match")
	}
}

func TestCondition_EqualsWithSigilSkipsOnDiffer(t *testing.T) {
	c := &Condition{
		Field:    "name_id",
		Operator: "equals",
		Value:    map[string]interface{}{"field": "related_name_id"},
	}
	vctx := &ValidationContext{
		Record: map[string]interface{}{
			"name_id":         "abc",
			"related_name_id": "xyz",
		},
	}
	ok, err := c.Eval(vctx)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if ok {
		t.Errorf("expected condition to skip when values differ")
	}
}

func equal(a, b interface{}) bool {
	as, aok := a.([]interface{})
	bs, bok := b.([]interface{})
	if aok && bok {
		if len(as) != len(bs) {
			return false
		}
		for i := range as {
			if as[i] != bs[i] {
				return false
			}
		}
		return true
	}
	return a == b
}
