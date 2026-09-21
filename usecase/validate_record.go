package usecase

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/sfborg/gsvalidator/domain"
	"github.com/sfborg/gsvalidator/usecase/validator"
)

// ValidateRecordUseCase validates a single record against all
// applicable rules.
type ValidateRecordUseCase struct {
	db                *sql.DB
	ruleLoader        RuleLoader
	schemaMapper      SchemaMapper
	validatorRegistry *validator.Registry

	// relationResolver, when set, is exposed via ValidationContext
	// so rule conditions and validators can traverse named
	// relations. Optional — rules without relation-scoped features
	// don't need it.
	relationResolver domain.ConditionResolver
}

// NewValidateRecordUseCase creates a new validate record use case.
func NewValidateRecordUseCase(db *sql.DB, ruleLoader RuleLoader, schemaMapper SchemaMapper, validatorRegistry *validator.Registry) *ValidateRecordUseCase {
	return &ValidateRecordUseCase{
		db:                db,
		ruleLoader:        ruleLoader,
		schemaMapper:      schemaMapper,
		validatorRegistry: validatorRegistry,
	}
}

// SetRelationResolver installs a resolver so rule conditions and
// validators that traverse named relations (from the bundle's
// Relations catalog) can evaluate. Call once at construction
// time; the resolver stays attached for the life of the use case.
func (uc *ValidateRecordUseCase) SetRelationResolver(r domain.ConditionResolver) {
	uc.relationResolver = r
}

// Neighborhoods returns the set of records whose validation results
// may have changed due to a mutation on (tableName, recordID). It
// walks the same rules Execute would run and, for each rule whose
// validator implements validator.NeighborhoodProvider, collects the
// neighboring records. Results are deduplicated (a record shows up
// once even if multiple rules point at it).
//
// Consumers call this alongside Execute on the write path to
// propagate re-validation to related rows — aggregate rules like
// count uniqueness only see one side of the change when the record
// being validated is R; the "other side" needs to re-run to see
// that R now (or no longer) matches.
func (uc *ValidateRecordUseCase) Neighborhoods(ctx context.Context, tableName, recordID string) ([]validator.NeighborRef, error) {
	record, err := uc.schemaMapper.LoadRecord(ctx, uc.db, tableName, recordID)
	if err != nil {
		return nil, fmt.Errorf("neighborhoods: load record %s from %s: %w", recordID, tableName, err)
	}
	return uc.NeighborhoodsFromRecord(ctx, tableName, recordID, record)
}

// NeighborhoodsFromRecord is like Neighborhoods but uses the
// caller-supplied record snapshot rather than loading fresh. Lets
// write paths capture a record's pre-mutation state and, after the
// mutation, use that snapshot to find the "old-side" neighbors —
// records that used to match the record's aggregate predicates and
// now don't.
//
// The record map must contain every field the rules reference.
// Missing keys are treated as nil (same semantic as an absent
// column). A nil record is passed straight through to the
// underlying providers, which typically means "no matches" for
// aggregate rules.
func (uc *ValidateRecordUseCase) NeighborhoodsFromRecord(ctx context.Context, tableName, recordID string, record map[string]interface{}) ([]validator.NeighborRef, error) {
	rules, err := uc.ruleLoader.LoadRulesForTable(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("neighborhoods: load rules for %s: %w", tableName, err)
	}
	vctx := &domain.ValidationContext{
		Ctx:              ctx,
		DB:               uc.db,
		Record:           record,
		TableName:        tableName,
		RecordID:         recordID,
		SystemContext:    domain.NewSystemContext(),
		RelatedRecords:   make(map[string][]map[string]interface{}),
		CodeContext:      make(map[string]interface{}),
		RelationResolver: uc.relationResolver,
	}
	seen := make(map[[2]string]bool)
	var out []validator.NeighborRef
	for _, rule := range rules {
		if !rule.IsActive {
			continue
		}
		v, exists := uc.validatorRegistry.Get(rule.ValidatorType)
		if !exists {
			continue
		}
		np, ok := v.(validator.NeighborhoodProvider)
		if !ok {
			continue
		}
		neighbors, err := np.Neighborhood(vctx, rule)
		if err != nil {
			return nil, fmt.Errorf("neighborhoods: rule %s: %w", rule.ID, err)
		}
		for _, n := range neighbors {
			key := [2]string{n.TableName, n.RecordID}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, n)
		}
	}
	return out, nil
}

// Execute validates a single record and returns all validation results.
func (uc *ValidateRecordUseCase) Execute(ctx context.Context, tableName, recordID string) ([]*domain.Result, error) {
	// Load record from database
	record, err := uc.schemaMapper.LoadRecord(ctx, uc.db, tableName, recordID)
	if err != nil {
		return nil, fmt.Errorf("failed to load record %s from %s: %w", recordID, tableName, err)
	}

	// Load rules for this table
	rules, err := uc.ruleLoader.LoadRulesForTable(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to load rules for table %s: %w", tableName, err)
	}

	// Create validation context
	validationCtx := &domain.ValidationContext{
		Ctx:              ctx,
		DB:               uc.db,
		Record:           record,
		TableName:        tableName,
		RecordID:         recordID,
		SystemContext:    domain.NewSystemContext(),
		RelatedRecords:   make(map[string][]map[string]interface{}),
		CodeContext:      make(map[string]interface{}),
		RelationResolver: uc.relationResolver,
	}

	// Validate record against all applicable rules
	var results []*domain.Result

	for _, rule := range rules {
		// Skip inactive rules
		if !rule.IsActive {
			continue
		}

		// Check if rule applies to this record (based on
		// conditions). EvalConditions honors relation-scoped
		// conditions by dispatching through the ValidationContext's
		// resolver; ordinary conditions still evaluate against the
		// record directly.
		applies, err := rule.EvalConditions(validationCtx)
		if err != nil {
			return nil, fmt.Errorf("rule %s: eval conditions: %w", rule.ID, err)
		}
		if !applies {
			continue
		}

		// Get validator for this rule
		v, exists := uc.validatorRegistry.Get(rule.ValidatorType)
		if !exists {
			return nil, fmt.Errorf("%w: %s", domain.ErrValidatorNotFound, rule.ValidatorType)
		}

		// Execute validation
		result, err := v.Validate(validationCtx, rule)
		if err != nil {
			return nil, fmt.Errorf("validation failed for rule %s: %w", rule.ID, err)
		}

		results = append(results, result)
	}

	return results, nil
}
