package usecase

import (
	"context"
	"database/sql"

	"github.com/sfborg/gsvalidator/domain"
)

// RuleLoader loads validation rules from a consumer-supplied source
// (bundled JSON, a database table, an embedded byte slice, …).
type RuleLoader interface {
	LoadRules(ctx context.Context) ([]*domain.Rule, error)
	LoadRuleByID(ctx context.Context, ruleID string) (*domain.Rule, error)
	LoadRulesForTable(ctx context.Context, tableName string) ([]*domain.Rule, error)
}

// ResultWriter writes validation results to a consumer-supplied
// sink (a database table, a JSON file, a stream, …).
type ResultWriter interface {
	WriteResults(ctx context.Context, results []*domain.Result) error
	WriteResult(ctx context.Context, result *domain.Result) error
}

// SchemaMapper abstracts database schema differences between
// systems. Implementations are supplied by the consuming
// application.
type SchemaMapper interface {
	// GetTableName resolves logical table name to physical table name
	GetTableName(logicalName string) string

	// GetFieldName resolves logical field name to physical field name
	GetFieldName(tableName, logicalName string) string

	// GetPrimaryKeyField returns the primary-key column for the given
	// logical table.
	GetPrimaryKeyField(tableName string) string

	// LoadRecord loads a record by table name and ID
	LoadRecord(ctx context.Context, db *sql.DB, tableName, id string) (map[string]interface{}, error)

	// LoadRelatedRecords loads related records via foreign key
	LoadRelatedRecords(ctx context.Context, db *sql.DB, tableName, foreignKey, id string) ([]map[string]interface{}, error)

	// LoadAllRecords loads all records from a table
	LoadAllRecords(ctx context.Context, db *sql.DB, tableName string) ([]map[string]interface{}, error)
}
