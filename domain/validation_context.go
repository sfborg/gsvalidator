package domain

import (
	"context"
	"database/sql"
	"time"
)

// ValidationContext provides access to the database and related records during validation.
// This is passed to validators to give them necessary context for complex validations.
type ValidationContext struct {
	Ctx            context.Context
	DB             *sql.DB
	Record         map[string]interface{}
	TableName      string
	RecordID       string
	SystemContext  SystemContext
	RelatedRecords map[string][]map[string]interface{} // Cache of related records
	CodeContext    map[string]interface{}              // Free-form per-record context supplied by the consumer

	// RelationResolver resolves named relations from the bundle's
	// Relations catalog. Populated by the caller (ValidateRecordUseCase
	// sets it from an application-supplied resolver). Nil is
	// acceptable for records / rules that don't use relations;
	// relation-scoped conditions or validators error cleanly when
	// this is nil.
	RelationResolver ConditionResolver
}

// SystemContext provides system-level context for validation.
type SystemContext struct {
	CurrentDate time.Time
	CurrentYear int
}

// NewSystemContext creates a new system context with current date/time.
func NewSystemContext() SystemContext {
	now := time.Now()
	return SystemContext{
		CurrentDate: now,
		CurrentYear: now.Year(),
	}
}

// GetFieldValue safely retrieves a field value from the record.
func (v *ValidationContext) GetFieldValue(fieldName string) (interface{}, bool) {
	value, exists := v.Record[fieldName]
	return value, exists
}

// GetFieldString safely retrieves a field value as string from the record.
func (v *ValidationContext) GetFieldString(fieldName string) (string, bool) {
	value, exists := v.Record[fieldName]
	if !exists {
		return "", false
	}

	if strVal, ok := value.(string); ok {
		return strVal, true
	}

	return "", false
}

// SetRelatedRecords caches related records for a given foreign key.
func (v *ValidationContext) SetRelatedRecords(foreignKey string, records []map[string]interface{}) {
	if v.RelatedRecords == nil {
		v.RelatedRecords = make(map[string][]map[string]interface{})
	}
	v.RelatedRecords[foreignKey] = records
}

// GetRelatedRecords retrieves cached related records for a given foreign key.
func (v *ValidationContext) GetRelatedRecords(foreignKey string) ([]map[string]interface{}, bool) {
	if v.RelatedRecords == nil {
		return nil, false
	}
	records, exists := v.RelatedRecords[foreignKey]
	return records, exists
}
