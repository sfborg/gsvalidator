package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/sfborg/gsvalidator/domain"
)

// BundleLoader reads a rule bundle (SchemaPackage JSON)
// and exposes both the full package and a RuleLoader adapter over
// its rules. Consumers use LoadPackage to get schema + relations +
// requires; existing use cases keep working via the RuleLoader
// methods.
//
// Two constructors:
//
//   - NewJSONBundleLoader(path)  loads from a file path
//   - NewBytesBundleLoader(data) loads from an in-memory byte slice
//     (for //go:embed use)
//
// Both are stateful: the JSON is parsed once on the first Load
// call and cached for subsequent lookups.
type BundleLoader struct {
	source    bundleSource
	pkg       *domain.SchemaPackage
	ruleByID  map[string]*domain.Rule
	rulesLoad bool
}

// bundleSource abstracts where the bytes come from.
type bundleSource interface {
	read() ([]byte, error)
	describe() string
}

type fileSource struct{ path string }

func (f fileSource) read() ([]byte, error) { return os.ReadFile(f.path) }
func (f fileSource) describe() string      { return f.path }

type bytesSource struct{ data []byte }

func (b bytesSource) read() ([]byte, error) { return b.data, nil }
func (b bytesSource) describe() string      { return "<embedded>" }

// NewJSONBundleLoader loads a bundle from a JSON file on disk.
func NewJSONBundleLoader(path string) *BundleLoader {
	return &BundleLoader{source: fileSource{path: path}}
}

// NewBytesBundleLoader loads a bundle from an in-memory byte
// slice — the pattern for //go:embed'ed bundles shipped inside a
// consumer binary.
func NewBytesBundleLoader(data []byte) *BundleLoader {
	return &BundleLoader{source: bytesSource{data: data}}
}

// LoadPackage returns the full SchemaPackage. Parses the source
// on first call and caches it; subsequent calls return the same
// struct.
func (l *BundleLoader) LoadPackage(ctx context.Context) (*domain.SchemaPackage, error) {
	if l.pkg != nil {
		return l.pkg, nil
	}
	data, err := l.source.read()
	if err != nil {
		return nil, fmt.Errorf("bundle %s: read: %w", l.source.describe(), err)
	}
	var pkg domain.SchemaPackage
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("bundle %s: parse: %w", l.source.describe(), err)
	}
	if err := pkg.Validate(); err != nil {
		return nil, fmt.Errorf("bundle %s: %w", l.source.describe(), err)
	}
	l.pkg = &pkg
	l.ruleByID = make(map[string]*domain.Rule, len(pkg.Rules))
	for _, r := range pkg.Rules {
		l.ruleByID[r.ID] = r
	}
	l.rulesLoad = true
	return l.pkg, nil
}

// LoadRules satisfies the RuleLoader interface. Returns the
// bundle's rules; only active rules are surfaced so that
// consumers don't need to filter separately.
func (l *BundleLoader) LoadRules(ctx context.Context) ([]*domain.Rule, error) {
	pkg, err := l.LoadPackage(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Rule, 0, len(pkg.Rules))
	for _, r := range pkg.Rules {
		if r.IsActive {
			out = append(out, r)
		}
	}
	return out, nil
}

// LoadRuleByID satisfies the RuleLoader interface. Looks up an
// individual rule by ID. Returns ErrRuleNotFound when no rule
// matches.
func (l *BundleLoader) LoadRuleByID(ctx context.Context, ruleID string) (*domain.Rule, error) {
	if _, err := l.LoadPackage(ctx); err != nil {
		return nil, err
	}
	r, ok := l.ruleByID[ruleID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", domain.ErrRuleNotFound, ruleID)
	}
	return r, nil
}

// LoadRulesForTable satisfies the RuleLoader interface. Filters
// the bundle's rules to those whose TableName matches. Only
// active rules are surfaced.
func (l *BundleLoader) LoadRulesForTable(ctx context.Context, tableName string) ([]*domain.Rule, error) {
	pkg, err := l.LoadPackage(ctx)
	if err != nil {
		return nil, err
	}
	var out []*domain.Rule
	for _, r := range pkg.Rules {
		if r.IsActive && r.TableName == tableName {
			out = append(out, r)
		}
	}
	return out, nil
}
