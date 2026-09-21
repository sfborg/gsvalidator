package validator

import "github.com/sfborg/gsvalidator/domain"

// Validator defines the interface that all validators must implement.
// This is the core abstraction for validation logic in the use case layer.
type Validator interface {
	// Name returns the validator type identifier (must match rule.ValidatorType)
	Name() string

	// Validate executes the validation logic and returns a result
	Validate(ctx *domain.ValidationContext, rule *domain.Rule) (*domain.Result, error)

	// CanAutoFix returns true if this validator supports automatic fixing
	CanAutoFix() bool

	// AutoFix attempts to fix the validation error (only called if CanAutoFix returns true)
	AutoFix(ctx *domain.ValidationContext, rule *domain.Rule, result *domain.Result) error
}

// NeighborRef points to a specific record whose validation results
// may be affected by a mutation to the currently-validating record.
// Consumers use it to propagate re-sync after a write.
type NeighborRef struct {
	TableName string
	RecordID  string
}

// NeighborhoodProvider is an optional interface for validators whose
// output depends on rows other than the one being validated (typically
// aggregates). When implemented, callers query the validator for the
// records whose results may have changed and re-run their validation.
//
// Return an empty slice when the rule isn't currently applicable
// (e.g. self fields empty and the rule skips) — callers treat that
// as "no propagation needed" without an error path.
//
// Only "new-side" neighbors are discoverable this way — records that
// currently match the record being validated. Records that USED to
// match (before the mutation changed the record's fields) are not
// returned; callers wanting full pre/post coverage need a
// pre-mutation hook to snapshot the old neighborhood.
type NeighborhoodProvider interface {
	Neighborhood(ctx *domain.ValidationContext, rule *domain.Rule) ([]NeighborRef, error)
}

// Registry manages all registered validators.
type Registry struct {
	validators map[string]Validator
}

// NewRegistry creates a new validator registry.
func NewRegistry() *Registry {
	return &Registry{
		validators: make(map[string]Validator),
	}
}

// Register adds a validator to the registry.
func (r *Registry) Register(validator Validator) {
	r.validators[validator.Name()] = validator
}

// Get retrieves a validator by name.
func (r *Registry) Get(name string) (Validator, bool) {
	v, exists := r.validators[name]
	return v, exists
}

// Has checks if a validator is registered.
func (r *Registry) Has(name string) bool {
	_, exists := r.validators[name]
	return exists
}

// All returns all registered validators.
func (r *Registry) All() map[string]Validator {
	return r.validators
}
