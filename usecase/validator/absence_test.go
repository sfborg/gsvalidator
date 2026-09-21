package validator

import (
	"testing"

	"github.com/sfborg/gsvalidator/domain"
)

func TestAbsenceValidator_MissingFieldPasses(t *testing.T) {
	v := &AbsenceValidator{}
	rule := &domain.Rule{FieldName: "col__missing"}
	ctx := &domain.ValidationContext{Record: map[string]interface{}{}}
	res, err := v.Validate(ctx, rule)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Passed {
		t.Errorf("missing field should pass; got failure %q", res.Message)
	}
}

func TestAbsenceValidator_NilValuePasses(t *testing.T) {
	v := &AbsenceValidator{}
	rule := &domain.Rule{FieldName: "col__nil"}
	ctx := &domain.ValidationContext{Record: map[string]interface{}{"col__nil": nil}}
	res, err := v.Validate(ctx, rule)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Passed {
		t.Errorf("nil value should pass; got failure %q", res.Message)
	}
}

func TestAbsenceValidator_EmptyStringPasses(t *testing.T) {
	v := &AbsenceValidator{}
	rule := &domain.Rule{FieldName: "col__empty"}
	ctx := &domain.ValidationContext{Record: map[string]interface{}{"col__empty": ""}}
	res, err := v.Validate(ctx, rule)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Passed {
		t.Errorf("empty string should pass; got failure %q", res.Message)
	}
}

func TestAbsenceValidator_NonEmptyStringFails(t *testing.T) {
	v := &AbsenceValidator{}
	rule := &domain.Rule{FieldName: "col__present", ErrorMessage: "field must be empty"}
	ctx := &domain.ValidationContext{Record: map[string]interface{}{"col__present": "value"}}
	res, err := v.Validate(ctx, rule)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Passed {
		t.Errorf("non-empty string should fail; got pass")
	}
	if res.ActualValue != "value" {
		t.Errorf("ActualValue = %v, want %q", res.ActualValue, "value")
	}
}

func TestAbsenceValidator_NonZeroNumberFails(t *testing.T) {
	v := &AbsenceValidator{}
	rule := &domain.Rule{FieldName: "col__count"}
	ctx := &domain.ValidationContext{Record: map[string]interface{}{"col__count": int64(1)}}
	res, err := v.Validate(ctx, rule)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Passed {
		t.Errorf("non-zero number should fail; got pass")
	}
}
