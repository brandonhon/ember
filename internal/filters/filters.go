// Package filters evaluates user-defined filter rules against newly-ingested
// articles and applies their actions. The Match shape is intentionally narrow:
// a single field/op/value triple. AnyOf / AllOf grouping can be added later
// without breaking the wire format (an envelope around the same primitive).
package filters

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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
	FieldTitle       Field = "title"
	FieldContent     Field = "content"
	FieldAuthor      Field = "author"
	FieldURL         Field = "url"
	FieldFeedID      Field = "feed_id"      // numeric: equals
	FieldTags        Field = "tags"         // string: contains
	FieldPublishedAt Field = "published_at" // duration: newer_than
	FieldHasImage    Field = "has_image"    // bool: equals "true"/"false"
)

// Op is the comparison operator.
type Op string

const (
	OpContains   Op = "contains"
	OpEquals     Op = "equals"
	OpStartsWith Op = "starts_with"
	OpMatches    Op = "matches" // regex
	// OpNewerThan compares a duration value ("24h", "7d") against the
	// published_at field. Only valid with FieldPublishedAt.
	OpNewerThan Op = "newer_than"
)

// Action is what to do when a filter matches.
type Action string

const (
	ActionMarkRead Action = "mark_read"
	ActionStar     Action = "star"
	ActionHide     Action = "hide" // implemented as mark_read for now
	// ActionTag attaches a tag (from action_value) to the article.
	ActionTag Action = "tag"
	// ActionAddToBoard adds the article to a board (board id, decimal,
	// in action_value).
	ActionAddToBoard Action = "add_to_board"
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

// Validate returns an error if the match is malformed. Enforces
// field-op compatibility: numeric / bool / duration fields don't accept
// the string ops, and vice versa.
func (m Match) Validate() error {
	switch m.Field {
	case FieldTitle, FieldContent, FieldAuthor, FieldURL, FieldTags:
		// string fields — any string op
		switch m.Op {
		case OpContains, OpEquals, OpStartsWith, OpMatches:
		default:
			return fmt.Errorf("filters: op %q not supported on field %q", m.Op, m.Field)
		}
	case FieldFeedID:
		if m.Op != OpEquals {
			return fmt.Errorf("filters: feed_id only supports equals")
		}
		if _, err := strconv.ParseInt(m.Value, 10, 64); err != nil {
			return fmt.Errorf("filters: feed_id value must be int: %w", err)
		}
	case FieldHasImage:
		if m.Op != OpEquals {
			return fmt.Errorf("filters: has_image only supports equals")
		}
		if m.Value != "true" && m.Value != "false" {
			return fmt.Errorf("filters: has_image value must be \"true\" or \"false\"")
		}
	case FieldPublishedAt:
		if m.Op != OpNewerThan {
			return fmt.Errorf("filters: published_at only supports newer_than")
		}
		if _, err := parseDuration(m.Value); err != nil {
			return fmt.Errorf("filters: published_at value must be a duration (e.g. 24h, 7d): %w", err)
		}
	default:
		return fmt.Errorf("filters: invalid field %q", m.Field)
	}
	if m.Value == "" {
		return fmt.Errorf("filters: value required")
	}
	if m.Op == OpMatches {
		// Bound the pattern length: each distinct pattern is compiled and
		// cached forever (reCache), so unbounded patterns are a memory-DoS
		// vector. RE2 rules out catastrophic backtracking; this caps per-entry
		// size. 512 is far beyond any legitimate filter.
		if len(m.Value) > maxPatternLen {
			return fmt.Errorf("filters: regex too long (max %d chars)", maxPatternLen)
		}
		if _, err := regexp.Compile(m.Value); err != nil {
			return fmt.Errorf("filters: invalid regex %q: %w", m.Value, err)
		}
	}
	return nil
}

// maxPatternLen caps a single match regex; see Match.Validate.
const maxPatternLen = 512

// parseDuration extends time.ParseDuration with a day unit ("7d"), which
// the stdlib doesn't support but our docs and UI advertise for the
// published_at/newer_than filter. A bare "Nd" is converted to N*24h; any
// other input falls through to time.ParseDuration unchanged.
func parseDuration(s string) (time.Duration, error) {
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(n)
		if err == nil {
			return time.Duration(days) * 24 * time.Hour, nil
		}
		// Not a plain integer day count (e.g. "1.5d", "1d12h") — let
		// time.ParseDuration produce the canonical error message.
	}
	return time.ParseDuration(s)
}

// ValidateAction returns an error for unknown action strings. Some
// actions require action_value; callers that have access to the value
// should use ValidateActionWithValue to also enforce that constraint.
func ValidateAction(a string) error {
	switch Action(a) {
	case ActionMarkRead, ActionStar, ActionHide, ActionTag, ActionAddToBoard:
		return nil
	}
	return fmt.Errorf("filters: invalid action %q", a)
}

// ValidateActionWithValue is the action-validator that knows about the
// payload (action_value). Used at the API layer where the value is in
// hand; the engine's Apply silently skips bad payloads.
func ValidateActionWithValue(a, value string) error {
	if err := ValidateAction(a); err != nil {
		return err
	}
	switch Action(a) {
	case ActionTag:
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("filters: tag action requires a tag name in action_value")
		}
	case ActionAddToBoard:
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return fmt.Errorf("filters: add_to_board action_value must be a board id: %w", err)
		}
	}
	return nil
}

// Matches returns true if the article satisfies the match. now is the
// reference clock for relative-date ops (FieldPublishedAt + OpNewerThan);
// callers in the poller hot path pass time.Now(); tests inject a fixed
// value.
func Matches(m Match, a models.Article, now time.Time) bool {
	// Numeric / boolean / duration fields branch first — they don't share
	// the string-comparison plumbing below.
	switch m.Field {
	case FieldFeedID:
		want, err := strconv.ParseInt(m.Value, 10, 64)
		if err != nil {
			return false
		}
		return a.FeedID == want
	case FieldHasImage:
		want := m.Value == "true"
		got := strings.TrimSpace(a.ImageURL) != ""
		return got == want
	case FieldPublishedAt:
		// OpNewerThan only — Validate enforces.
		d, err := parseDuration(m.Value)
		if err != nil {
			return false
		}
		if a.PublishedAt == 0 {
			return false
		}
		return now.Add(-d).Unix() <= a.PublishedAt
	}

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
	case FieldTags:
		return a.Tags
	}
	return ""
}

// Outcome is what the engine wants applied for a given article+user pair.
// Tags and BoardIDs are additive: every matching rule contributes — an
// article can pick up multiple tags or land on multiple boards in one
// pass. The boolean fields are sticky: once true, they stay true.
type Outcome struct {
	MarkRead bool
	Star     bool
	Hide     bool
	// Tags carries every distinct tag name from matched ActionTag rules.
	Tags []string
	// BoardIDs carries every distinct board id from matched ActionAddToBoard
	// rules. The poller resolves each id against the user's actual boards
	// — cross-user safety.
	BoardIDs []int64
}

// Any returns true if any action would be applied.
func (o Outcome) Any() bool {
	return o.MarkRead || o.Star || o.Hide || len(o.Tags) > 0 || len(o.BoardIDs) > 0
}

// Apply runs all enabled filters against the article in priority order
// (lower priority numbers first; ties broken by filter id ascending) and
// returns the combined outcome. Bad match_json, unknown action, or bad
// action_value is silently skipped — filters are never allowed to break
// ingest. now is the reference clock for relative-date matches.
func Apply(rules []models.Filter, a models.Article, now time.Time) Outcome {
	// Sort by priority (asc), then id (asc) so the engine is deterministic
	// regardless of how the caller ordered rules. Higher-priority rules
	// see "earlier" state and can short-circuit downstream by setting
	// MarkRead/Hide (though the boolean fields are additive, not
	// preemptive — we keep semantics simple for v1).
	sorted := make([]models.Filter, len(rules))
	copy(sorted, rules)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority < sorted[j].Priority
		}
		return sorted[i].ID < sorted[j].ID
	})

	var out Outcome
	tagsSeen := map[string]struct{}{}
	boardsSeen := map[int64]struct{}{}
	for _, f := range sorted {
		if !f.Enabled {
			continue
		}
		m, err := ParseMatch(f.MatchJSON)
		if err != nil {
			continue
		}
		if !Matches(m, a, now) {
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
		case ActionTag:
			tag := strings.TrimSpace(f.ActionValue)
			if tag == "" {
				continue
			}
			if _, dup := tagsSeen[tag]; dup {
				continue
			}
			tagsSeen[tag] = struct{}{}
			out.Tags = append(out.Tags, tag)
		case ActionAddToBoard:
			id, perr := strconv.ParseInt(f.ActionValue, 10, 64)
			if perr != nil || id <= 0 {
				continue
			}
			if _, dup := boardsSeen[id]; dup {
				continue
			}
			boardsSeen[id] = struct{}{}
			out.BoardIDs = append(out.BoardIDs, id)
		}
	}
	return out
}
