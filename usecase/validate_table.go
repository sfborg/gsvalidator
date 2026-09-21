package usecase

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/sfborg/gsvalidator/domain"
	"github.com/sfborg/gsvalidator/usecase/validator"
)

// ValidationReport contains validation results for an entire table or database.
type ValidationReport struct {
	DatabasePath string
	TableName    string
	TotalRecords int
	TotalRules   int
	HardErrors   int
	SoftWarnings int
	PassedCount  int
	FailedCount  int
	Results      []*domain.Result
}

// ValidateTableUseCase validates all records in a table.
type ValidateTableUseCase struct {
	db                *sql.DB
	ruleLoader        RuleLoader
	schemaMapper      SchemaMapper
	validatorRegistry *validator.Registry
}

// NewValidateTableUseCase creates a new validate table use case.
func NewValidateTableUseCase(db *sql.DB, ruleLoader RuleLoader, schemaMapper SchemaMapper, validatorRegistry *validator.Registry) *ValidateTableUseCase {
	return &ValidateTableUseCase{
		db:                db,
		ruleLoader:        ruleLoader,
		schemaMapper:      schemaMapper,
		validatorRegistry: validatorRegistry,
	}
}

// Execute validates all records in a table and returns a report.
func (uc *ValidateTableUseCase) Execute(ctx context.Context, tableName string) (*ValidationReport, error) {
	// Load rules for this table
	rules, err := uc.ruleLoader.LoadRulesForTable(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to load rules for table %s: %w", tableName, err)
	}

	// Load all records from table
	records, err := uc.schemaMapper.LoadAllRecords(ctx, uc.db, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to load records from %s: %w", tableName, err)
	}

	// Create report
	report := &ValidationReport{
		TableName:    tableName,
		TotalRecords: len(records),
		TotalRules:   len(rules),
		Results:      make([]*domain.Result, 0),
	}

	// Validate each record
	validateRecordUC := NewValidateRecordUseCase(uc.db, uc.ruleLoader, uc.schemaMapper, uc.validatorRegistry)

	for i, record := range records {
		// Get record ID
		recordID, ok := record["id"]
		if !ok {
			continue // Skip records without ID
		}
		recordIDStr := fmt.Sprintf("%v", recordID)

		// Progress reporting every 100 records
		if (i+1)%100 == 0 || i == 0 {
			fmt.Fprintf(os.Stderr, "Progress: %d/%d records (%.1f%%)\n", i+1, len(records), float64(i+1)/float64(len(records))*100)
		}

		// Validate record
		results, err := validateRecordUC.Execute(ctx, tableName, recordIDStr)
		if err != nil {
			return nil, fmt.Errorf("failed to validate record %s: %w", recordIDStr, err)
		}

		// Aggregate results
		for _, result := range results {
			report.Results = append(report.Results, result)

			if result.Passed {
				report.PassedCount++
			} else {
				report.FailedCount++
				if result.IsHardFailure() {
					report.HardErrors++
				} else {
					report.SoftWarnings++
				}
			}
		}
	}

	return report, nil
}
