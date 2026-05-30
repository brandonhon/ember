// Package filters evaluates user-defined filter rules against newly-ingested
// articles and applies their actions. The Match shape is intentionally narrow:
// a single field/op/value triple. AnyOf / AllOf grouping can be added later
// without breaking the wire format (an envelope around the same primitive).
package filters

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/brandonhon/ember/internal/models"
)

// reCache memoizes compiled regexps keyed on the final pattern string
// (including any "(?i)" prefix). Matches runs per-article-per-filter in the
// poller hot path; without this the same user pattern is recompiled on every
// article. Patterns are validated at write time and the working set is small
// and stable, so no eviction is needed.
var reCache sync.Map // pattern string -> *regexp.Regexp

func cachedRegexp(pattern string) (*regexp.Regexp, error) {
	if v, ok := reCache.Load(pattern); ok {
		return v.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	reCache.Store(pattern, re)
	return re, nil
}

// Field is the name of the article field the match runs against.
type Field string

const (
	FieldTitle   Field = "title"
	FieldContent Field = "content"
	FieldAuthor  Field = "author"
	FieldURL     Field = "url"
)

// Op is the comparison operator.
type Op string

const (
	OpContains   Op = "contains"
	OpEquals     Op = "equals"
	OpStartsWith Op = "starts_with"
	OpMatches    Op = "matches" // regex
)

// Action is what to do when a filter matches.
type Action string

const (
	ActionMarkRead Action = "mark_read"
	ActionStar     Action = "star"
	ActionHide     Action = "hide" // implemented as mark_read for now
)

// Match is the JSON shape stored in filters.match_json.
type Match struct {
	Field         Field  `json:"field"`
	Op            Op     `json:"op"`
	Value         string `json:"value"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
}

// ParseMatch parses the stored JSON.
func ParseMatch(s string) (Match, error) {
	var m Match
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return Match{}, fmt.Errorf("filters: parse match: %w", err)
	}
	if err := m.Validate(); err != nil {
		return Match{}, err
	}
	return m, nil
}

// Validate returns an error if the match is malformed.
func (m Match) Validate() error {
	switch m.Field {
	case FieldTitle, FieldContent, FieldAuthor, FieldURL:
	default:
		return fmt.Errorf("filters: invalid field %q", m.Field)
	}
	switch m.Op {
	case OpContains, OpEquals, OpStartsWith, OpMatches:
	default:
		return fmt.Errorf("filters: invalid op %q", m.Op)
	}
	if m.Value == "" {
		return fmt.Errorf("filters: value required")
	}
	if m.Op == OpMatches {
		if _, err := regexp.Compile(m.Value); err != nil {
			return fmt.Errorf("filters: invalid regex %q: %w", m.Value, err)
		}
	}
	return nil
}

// ValidateAction returns an error for unknown action strings.
func ValidateAction(a string) error {
	switch Action(a) {
	case ActionMarkRead, ActionStar, ActionHide:
		return nil
	}
	return fmt.Errorf("filters: invalid action %q", a)
}

// Matches returns true if the article satisfies the match.
func Matches(m Match, a models.Article) bool {
	subject := fieldValue(m.Field, a)
	value := m.Value
	if !m.CaseSensitive {
		subject = strings.ToLower(subject)
		value = strings.ToLower(value)
	}
	switch m.Op {
	case OpContains:
		return strings.Contains(subject, value)
	case OpEquals:
		return subject == value
	case OpStartsWith:
		return strings.HasPrefix(subject, value)
	case OpMatches:
		// Validate guarantees this compiles. The "(?i)" prefix applies the
		// case-insensitive flag; compiled forms are cached (see reCache).
		pattern := m.Value
		if !m.CaseSensitive {
			pattern = "(?i)" + pattern
		}
		re, err := cachedRegexp(pattern)
		if err != nil {
			return false
		}
		return re.MatchString(subject)
	}
	return false
}

func fieldValue(f Field, a models.Article) string {
	switch f {
	case FieldTitle:
		return a.Title
	case FieldContent:
		return a.ContentText
	case FieldAuthor:
		return a.Author
	case FieldURL:
		return a.URL
	}
	return ""
}

// Outcome is what the engine wants applied for a given article+user pair.
type Outcome struct {
	MarkRead bool
	Star     bool
	Hide     bool
}

// Any returns true if any action would be applied.
func (o Outcome) Any() bool { return o.MarkRead || o.Star || o.Hide }

// Apply runs all enabled filters against the article and returns the combined
// outcome. Bad match_json or unknown action is silently skipped — filters are
// not allowed to break ingest.
func Apply(filters []models.Filter, a models.Article) Outcome {
	var out Outcome
	for _, f := range filters {
		if !f.Enabled {
			continue
		}
		m, err := ParseMatch(f.MatchJSON)
		if err != nil {
			continue
		}
		if !Matches(m, a) {
			continue
		}
		switch Action(f.Action) {
		case ActionMarkRead:
			out.MarkRead = true
		case ActionStar:
			out.Star = true
		case ActionHide:
			out.Hide = true
			out.MarkRead = true // hide also reads (until is_hidden column exists)
		}
	}
	return out
}
