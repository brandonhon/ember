package filters

import (
	"strings"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
)

// Validate (and therefore Apply, which calls it for every article x filter)
// must go through reCache. A bare regexp.Compile here re-parses each pattern
// on every ingested article; the cache exists precisely to avoid that.
func TestValidate_UsesRegexCache(t *testing.T) {
	pattern := "cache-probe-(alpha|beta)"
	m := Match{Field: FieldTitle, Op: OpMatches, Value: pattern}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	first, ok := reCache.Load(pattern)
	if !ok {
		t.Fatal("Validate did not populate reCache — it is compiling on every call")
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate (2nd): %v", err)
	}
	second, _ := reCache.Load(pattern)
	if first != second {
		t.Error("second Validate replaced the cached regexp instead of reusing it")
	}
	// An invalid pattern must still error and must not be cached.
	bad := Match{Field: FieldTitle, Op: OpMatches, Value: "unclosed(["}
	if err := bad.Validate(); err == nil {
		t.Error("invalid regex accepted")
	}
	if _, cached := reCache.Load("unclosed(["); cached {
		t.Error("invalid pattern was cached")
	}
}

func TestValidate_FieldOpCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name    string
		m       Match
		wantErr bool
	}{
		{"title contains", Match{Field: FieldTitle, Op: OpContains, Value: "x"}, false},
		{"title newer_than rejected", Match{Field: FieldTitle, Op: OpNewerThan, Value: "24h"}, true},
		{"feed_id equals int", Match{Field: FieldFeedID, Op: OpEquals, Value: "12"}, false},
		{"feed_id non-int", Match{Field: FieldFeedID, Op: OpEquals, Value: "abc"}, true},
		{"feed_id contains rejected", Match{Field: FieldFeedID, Op: OpContains, Value: "12"}, true},
		{"has_image true", Match{Field: FieldHasImage, Op: OpEquals, Value: "true"}, false},
		{"has_image bogus", Match{Field: FieldHasImage, Op: OpEquals, Value: "yes"}, true},
		{"published_at newer_than 7d", Match{Field: FieldPublishedAt, Op: OpNewerThan, Value: "7d"}, false},
		{"published_at bad duration", Match{Field: FieldPublishedAt, Op: OpNewerThan, Value: "soon"}, true},
		{"published_at equals rejected", Match{Field: FieldPublishedAt, Op: OpEquals, Value: "7d"}, true},
		{"unknown field", Match{Field: "nope", Op: OpEquals, Value: "x"}, true},
		{"empty value", Match{Field: FieldTitle, Op: OpContains, Value: ""}, true},
		{"overlong regex", Match{Field: FieldTitle, Op: OpMatches, Value: strings.Repeat("a", maxPatternLen+1)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.m.Validate(); (err != nil) != tc.wantErr {
				t.Errorf("Validate(%+v) err = %v, wantErr %v", tc.m, err, tc.wantErr)
			}
		})
	}
}

func TestValidateActionWithValue(t *testing.T) {
	for _, tc := range []struct {
		action, value string
		wantErr       bool
	}{
		{"mark_read", "", false},
		{"star", "", false},
		{"hide", "", false},
		{"tag", "news", false},
		{"tag", "   ", true},
		{"tag", "", true},
		{"add_to_board", "7", false},
		{"add_to_board", "not-a-number", true},
		{"add_to_board", "", true},
		{"explode", "", true},
	} {
		err := ValidateActionWithValue(tc.action, tc.value)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateActionWithValue(%q,%q) = %v, wantErr %v", tc.action, tc.value, err, tc.wantErr)
		}
	}
}

func TestMatches_AllFieldsAndOps(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := models.Article{
		FeedID: 7, Title: "Breaking News", ContentText: "body copy here",
		Author: "Jane Doe", URL: "https://ex.test/a", Tags: "tech,go",
		ImageURL: "https://ex.test/i.png", PublishedAt: now.Add(-2 * time.Hour).Unix(),
	}
	for _, tc := range []struct {
		name string
		m    Match
		want bool
	}{
		{"title contains ci", Match{Field: FieldTitle, Op: OpContains, Value: "breaking"}, true},
		{"title contains cs miss", Match{Field: FieldTitle, Op: OpContains, Value: "breaking", CaseSensitive: true}, false},
		{"title equals", Match{Field: FieldTitle, Op: OpEquals, Value: "breaking news"}, true},
		{"title starts_with", Match{Field: FieldTitle, Op: OpStartsWith, Value: "break"}, true},
		{"title regex ci", Match{Field: FieldTitle, Op: OpMatches, Value: "^break(ing)? ?news$"}, true},
		{"title regex cs miss", Match{Field: FieldTitle, Op: OpMatches, Value: "^breaking news$", CaseSensitive: true}, false},
		{"content contains", Match{Field: FieldContent, Op: OpContains, Value: "body"}, true},
		{"author contains", Match{Field: FieldAuthor, Op: OpContains, Value: "jane"}, true},
		{"url starts_with", Match{Field: FieldURL, Op: OpStartsWith, Value: "https://ex.test"}, true},
		{"tags contains", Match{Field: FieldTags, Op: OpContains, Value: "go"}, true},
		{"feed_id hit", Match{Field: FieldFeedID, Op: OpEquals, Value: "7"}, true},
		{"feed_id miss", Match{Field: FieldFeedID, Op: OpEquals, Value: "8"}, false},
		{"feed_id unparseable", Match{Field: FieldFeedID, Op: OpEquals, Value: "x"}, false},
		{"has_image true", Match{Field: FieldHasImage, Op: OpEquals, Value: "true"}, true},
		{"has_image false", Match{Field: FieldHasImage, Op: OpEquals, Value: "false"}, false},
		{"newer_than 24h", Match{Field: FieldPublishedAt, Op: OpNewerThan, Value: "24h"}, true},
		{"newer_than 1h", Match{Field: FieldPublishedAt, Op: OpNewerThan, Value: "1h"}, false},
		{"newer_than bad duration", Match{Field: FieldPublishedAt, Op: OpNewerThan, Value: "soon"}, false},
		{"unknown field", Match{Field: "nope", Op: OpContains, Value: "x"}, false},
		{"unknown op", Match{Field: FieldTitle, Op: "nope", Value: "x"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Matches(tc.m, a, now); got != tc.want {
				t.Errorf("Matches(%+v) = %v, want %v", tc.m, got, tc.want)
			}
		})
	}

	// An article with no published_at can never satisfy newer_than.
	noPub := a
	noPub.PublishedAt = 0
	if Matches(Match{Field: FieldPublishedAt, Op: OpNewerThan, Value: "24h"}, noPub, now) {
		t.Error("newer_than matched an article with no published_at")
	}
	// has_image=false must match when the image URL is blank/whitespace.
	noImg := a
	noImg.ImageURL = "   "
	if !Matches(Match{Field: FieldHasImage, Op: OpEquals, Value: "false"}, noImg, now) {
		t.Error("has_image=false should match a blank image URL")
	}
}

func rule(id int64, prio int, enabled bool, matchJSON, action, value string) models.Filter {
	return models.Filter{ID: id, Priority: prio, Enabled: enabled, MatchJSON: matchJSON, Action: action, ActionValue: value}
}

const titleMatch = `{"field":"title","op":"contains","value":"news"}`

// Apply must never let a bad rule break ingest, must dedupe additive actions,
// and must be deterministic regardless of input order.
func TestApply_SkipsBadRulesAndDedupes(t *testing.T) {
	a := models.Article{Title: "Breaking news", FeedID: 1}
	now := time.Now()

	out := Apply([]models.Filter{
		rule(1, 100, true, "not json at all", "star", ""),
		rule(2, 100, true, `{"field":"bogus","op":"contains","value":"x"}`, "star", ""),
		rule(3, 100, true, titleMatch, "explode", ""),
		rule(4, 100, true, titleMatch, "tag", "  "),
		rule(5, 100, true, titleMatch, "add_to_board", "nope"),
		rule(6, 100, true, titleMatch, "add_to_board", "-1"),
		rule(7, 100, false, titleMatch, "star", ""),
	}, a, now)
	if out.Any() {
		t.Errorf("bad/disabled rules produced an outcome: %+v", out)
	}

	out = Apply([]models.Filter{
		rule(1, 100, true, titleMatch, "tag", "dup"),
		rule(2, 100, true, titleMatch, "tag", "dup"),
		rule(3, 100, true, titleMatch, "tag", "other"),
		rule(4, 100, true, titleMatch, "add_to_board", "5"),
		rule(5, 100, true, titleMatch, "add_to_board", "5"),
	}, a, now)
	if len(out.Tags) != 2 || out.Tags[0] != "dup" || out.Tags[1] != "other" {
		t.Errorf("tags = %v, want [dup other]", out.Tags)
	}
	if len(out.BoardIDs) != 1 || out.BoardIDs[0] != 5 {
		t.Errorf("boards = %v, want [5]", out.BoardIDs)
	}

	// hide implies mark_read.
	out = Apply([]models.Filter{rule(1, 100, true, titleMatch, "hide", "")}, a, now)
	if !out.Hide || !out.MarkRead {
		t.Errorf("hide should also mark read: %+v", out)
	}
}

// Ordering is by priority then id, and the caller's slice order must not
// change the result — nor may Apply mutate the caller's slice.
func TestApply_DeterministicAndNonMutating(t *testing.T) {
	a := models.Article{Title: "Breaking news", FeedID: 1}
	now := time.Now()
	rules := []models.Filter{
		rule(3, 50, true, titleMatch, "tag", "third"),
		rule(1, 10, true, titleMatch, "tag", "first"),
		rule(2, 10, true, titleMatch, "tag", "second"),
	}
	before := append([]models.Filter(nil), rules...)

	out := Apply(rules, a, now)
	want := []string{"first", "second", "third"}
	for i, w := range want {
		if out.Tags[i] != w {
			t.Fatalf("tags = %v, want %v", out.Tags, want)
		}
	}
	for i := range rules {
		if rules[i].ID != before[i].ID {
			t.Fatalf("Apply reordered the caller's slice: %v", rules)
		}
	}
	// Reversed input, identical outcome.
	rev := []models.Filter{rules[2], rules[1], rules[0]}
	if got := Apply(rev, a, now); got.Tags[0] != "first" || got.Tags[2] != "third" {
		t.Errorf("reversed input changed the result: %v", got.Tags)
	}
}

func TestParseDuration_DaySuffix(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"24h", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"0d", 0, false},
		{"1.5d", 0, true},
		{"1d12h", 0, true},
		{"", 0, true},
		{"soon", 0, true},
	} {
		got, err := parseDuration(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseDuration(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if err == nil && got != tc.want {
			t.Errorf("parseDuration(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
