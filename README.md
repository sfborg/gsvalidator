# gsvalidator

Rule-driven validation for SQLite databases.

Rules are declared in JSON: the table and field to check, the validator
to apply, and whether a failure is an error or a warning. gsvalidator
runs the rules against a database and reports each failing record. It
does not assume a particular schema: named relations let rules follow
joins between tables, and a mapper interface connects rule table and
field names to the physical schema.

## Validators

| Validator | Checks that |
|---|---|
| `presence`, `absence` | a field is set, or is empty |
| `regex` | a value matches a pattern |
| `length` | a string's length is within bounds |
| `range` | a numeric or date value is within bounds |
| `check_digit` | an identifier's check digit is valid (ORCID, ISSN, ISBN-10, ISBN-13, Luhn) |
| `foreign_key_exists` | a foreign key resolves to an existing row through a declared relation |
| `related_field_equals`, `related_field_not_equals`, `related_field_in_set` | a field on a record reached through a relation equals, differs from, or is one of a set of values |
| `count_across` | the number of matching rows in another table is within bounds |
| `ancestor_exists`, `ancestor_field_check`, `parent_rank_higher`, `no_self_cycle` | along a self-referencing parent chain: an ancestor matches, a field compares against an ancestor's, the parent ranks higher, or the chain has no cycle |

## Rule bundles

A bundle is a single JSON document holding a schema's relations and its
rules. `enforcement` is `hard` or `soft`; `severity` is `error`, `warn`,
`info` or `debug`.

```json
{
  "name": "example",
  "version": "1.0.0",
  "relations": {
    "book:publisher": {
      "target_table": "publisher",
      "cardinality": "one",
      "join": [{ "from": "book.publisher_id", "to": "publisher.id" }]
    }
  },
  "rules": [
    {
      "rule_id": "book_title_presence",
      "table_name": "book",
      "field_name": "title",
      "validator_type": "presence",
      "enforcement": "hard",
      "severity": "error",
      "error_message": "Title is required",
      "is_active": true
    },
    {
      "rule_id": "book_year_format",
      "table_name": "book",
      "field_name": "published_year",
      "validator_type": "regex",
      "enforcement": "soft",
      "severity": "warn",
      "parameters": { "pattern": "^[0-9]{4}$" },
      "warning_message": "Year should have four digits",
      "is_active": true
    }
  ]
}
```

## Usage

```go
loader := repository.NewJSONBundleLoader("rules.json") // or NewBytesBundleLoader for //go:embed
pkg, err := loader.LoadPackage(ctx)
if err != nil {
	return err
}
resolver := joins.NewRelationResolver(pkg.Relations, mapper)

registry := validator.NewRegistry()
registry.Register(&validator.PresenceValidator{})
registry.Register(validator.NewRegexValidator())
registry.Register(validator.NewForeignKeyExistsValidator(resolver))

uc := usecase.NewValidateTableUseCase(db, loader, mapper, registry)
report, err := uc.Execute(ctx, "book")
if err != nil {
	return err
}
fmt.Println(report.HardErrors, report.SoftWarnings)
```

`mapper` is your implementation of `usecase.SchemaMapper`: it maps rule
table and field names to the physical schema, names each table's primary
key, and loads records. Register the validators your rules use;
relation-aware validators take the resolver, and parent-chain validators
take the mapper.

`presenter.NewSQLiteFormatter` writes a report's results to a
`validation_results` table.

## Install

```
go get github.com/sfborg/gsvalidator
```

Requires Go 1.25 or later.

## License

MIT. See [LICENSE](LICENSE).
