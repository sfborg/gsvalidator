package presenter

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/sfborg/gsvalidator/domain"
	"github.com/sfborg/gsvalidator/usecase"
)

// SQLiteFormatter writes validation results to a SQLite database validation_results table
type SQLiteFormatter struct {
	db        *sql.DB
	batchSize int
	clear     bool
	prefix    string // "__gs_" for new databases, "" for legacy
	prefixSet bool   // Whether prefix has been detected
}

// NewSQLiteFormatter creates a new SQLite formatter
func NewSQLiteFormatter(db *sql.DB, batchSize int, clear bool) *SQLiteFormatter {
	if batchSize <= 0 {
		batchSize = 1000 // Default batch size
	}
	return &SQLiteFormatter{
		db:        db,
		batchSize: batchSize,
		clear:     clear,
	}
}

// Format writes validation report to SQLite validation_results table
func (f *SQLiteFormatter) Format(ctx context.Context, report *usecase.ValidationReport) error {
	// Ensure validation_results table exists
	if err := f.ensureTable(ctx); err != nil {
		return fmt.Errorf("failed to ensure validation_results table: %w", err)
	}

	// Clear old results if requested
	if f.clear {
		if err := f.clearOldResults(ctx, report.TableName); err != nil {
			return fmt.Errorf("failed to clear old results: %w", err)
		}
	}

	// Insert failures only
	failures := make([]*domain.Result, 0)
	for _, result := range report.Results {
		if !result.Passed {
			failures = append(failures, result)
		}
	}

	if len(failures) == 0 {
		return nil // No failures to insert
	}

	// Insert in batches
	if err := f.insertBatch(ctx, failures); err != nil {
		return fmt.Errorf("failed to insert validation results: %w", err)
	}

	return nil
}

// detectPrefix detects if database uses new __gs_ prefix
func (f *SQLiteFormatter) detectPrefix(ctx context.Context) {
	if f.prefixSet {
		return
	}
	var tableName string
	err := f.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name='__gs_validation_rules'").Scan(&tableName)
	if err == nil {
		f.prefix = "__gs_"
	} else {
		f.prefix = ""
	}
	f.prefixSet = true
}

// ensureTable creates validation_results table if it doesn't exist
func (f *SQLiteFormatter) ensureTable(ctx context.Context) error {
	// Detect if database uses new __gs_ prefix by checking for __gs_validation_rules
	f.detectPrefix(ctx)

	schema := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %svalidation_results (
		result_id TEXT PRIMARY KEY,
		table_name TEXT NOT NULL,
		record_id TEXT NOT NULL,
		field_name TEXT NOT NULL,
		rule_id TEXT NOT NULL,
		rule_name TEXT NOT NULL,
		validator_type TEXT NOT NULL,
		validation_type TEXT NOT NULL,
		validation_message TEXT NOT NULL,
		actual_value TEXT,
		expected_value TEXT,
		is_resolved BOOLEAN DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		resolved_at DATETIME,
		FOREIGN KEY (rule_id) REFERENCES %svalidation_rules(rule_id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_validation_results_record
		ON %svalidation_results(table_name, record_id);

	CREATE INDEX IF NOT EXISTS idx_validation_results_unresolved
		ON %svalidation_results(is_resolved) WHERE is_resolved = 0;

	CREATE INDEX IF NOT EXISTS idx_validation_results_field
		ON %svalidation_results(table_name, field_name);

	CREATE INDEX IF NOT EXISTS idx_validation_results_validator
		ON %svalidation_results(validator_type);

	CREATE INDEX IF NOT EXISTS idx_validation_results_type
		ON %svalidation_results(validation_type);

	CREATE INDEX IF NOT EXISTS idx_validation_results_rule
		ON %svalidation_results(rule_id);
	`, f.prefix, f.prefix, f.prefix, f.prefix, f.prefix, f.prefix, f.prefix, f.prefix)

	_, err := f.db.ExecContext(ctx, schema)
	return err
}

// clearOldResults deletes old validation results for a table
func (f *SQLiteFormatter) clearOldResults(ctx context.Context, tableName string) error {
	f.detectPrefix(ctx)
	query := fmt.Sprintf(`DELETE FROM %svalidation_results WHERE table_name = ?`, f.prefix)
	_, err := f.db.ExecContext(ctx, query, tableName)
	return err
}

// insertBatch inserts validation results in batches for performance
func (f *SQLiteFormatter) insertBatch(ctx context.Context, results []*domain.Result) error {
	f.detectPrefix(ctx)
	// Prepare insert statement
	query := fmt.Sprintf(`
	INSERT INTO %svalidation_results (
		result_id,
		table_name,
		record_id,
		field_name,
		rule_id,
		rule_name,
		validator_type,
		validation_type,
		validation_message,
		actual_value,
		expected_value,
		is_resolved,
		created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)
	`, f.prefix)

	stmt, err := f.db.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	// Process in batches
	for i := 0; i < len(results); i += f.batchSize {
		end := i + f.batchSize
		if end > len(results) {
			end = len(results)
		}
		batch := results[i:end]

		// Start transaction for batch
		tx, err := f.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		txStmt := tx.StmtContext(ctx, stmt)

		// Insert batch
		for _, result := range batch {
			resultID := f.generateResultID(result)
			actualValue := f.formatValue(result.ActualValue)
			expectedValue := f.formatValue(result.ExpectedValue)

			_, err := txStmt.ExecContext(ctx,
				resultID,
				result.TableName,
				result.RecordID,
				result.FieldName,
				result.RuleID,
				result.RuleName,
				result.ValidatorType,
				result.ValidationType,
				result.Message,
				actualValue,
				expectedValue,
				result.Timestamp.Format(time.RFC3339),
			)
			if err != nil {
				tx.Rollback()
				return err
			}
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

// generateResultID creates a unique ID for a validation result
// Format: <table>_<record>_<rule>_<timestamp>
func (f *SQLiteFormatter) generateResultID(result *domain.Result) string {
	return fmt.Sprintf("%s_%s_%s_%d",
		result.TableName,
		result.RecordID,
		result.RuleID,
		result.Timestamp.Unix(),
	)
}

// formatValue converts interface{} to string for storage
func (f *SQLiteFormatter) formatValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}

// CountResults returns the number of validation results in the database
func (f *SQLiteFormatter) CountResults(ctx context.Context, tableName string) (int, error) {
	f.detectPrefix(ctx)
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %svalidation_results WHERE table_name = ?`, f.prefix)
	var count int
	err := f.db.QueryRowContext(ctx, query, tableName).Scan(&count)
	return count, err
}
