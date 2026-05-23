package filters

import (
	"testing"

	"github.com/brandonhon/ember/internal/models"
)

func art(title, content, author, url string) models.Article {
	return models.Article{Title: title, ContentText: content, Author: author, URL: url}
}

func TestMatch_AllOps(t *testing.T) {
	a := art("Rust 1.80 announced", "Borrow checker improvements", "Alice", "https://blog.test/rust")
	cases := []struct {
		name string
		m    Match
		want bool
	}{
		{"contains hit", Match{Field: FieldTitle, Op: OpContains, Value: "rust"}, true},
		{"contains miss", Match{Field: FieldTitle, Op: OpContains, Value: "python"}, false},
		{"equals hit", Match{Field: FieldAuthor, Op: OpEquals, Value: "alice"}, true},
		{"equals case-sensitive miss", Match{Field: FieldAuthor, Op: OpEquals, Value: "alice", CaseSensitive: true}, false},
		{"starts_with hit", Match{Field: FieldTitle, Op: OpStartsWith, Value: "rust"}, true},
		{"starts_with miss", Match{Field: FieldTitle, Op: OpStartsWith, Value: "announced"}, false},
		{"matches regex hit", Match{Field: FieldContent, Op: OpMatches, Value: `borrow\s+checker`}, true},
		{"matches regex miss", Match{Field: FieldContent, Op: OpMatches, Value: `garbage\s+collector`}, false},
		{"url field", Match{Field: FieldURL, Op: OpContains, Value: "blog.test"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Matches(c.m, a); got != c.want {
				t.Errorf("Matches(%+v) = %v, want %v", c.m, got, c.want)
			}
		})
	}
}

func TestMatch_Validate(t *testing.T) {
	valid := []Match{
		{Field: FieldTitle, Op: OpContains, Value: "x"},
		{Field: FieldContent, Op: OpMatches, Value: `\w+`},
	}
	for _, m := range valid {
		if err := m.Validate(); err != nil {
			t.Errorf("expected valid %+v: %v", m, err)
		}
	}

	invalid := []Match{
		{Field: "bogus", Op: OpContains, Value: "x"},
		{Field: FieldTitle, Op: "explode", Value: "x"},
		{Field: FieldTitle, Op: OpContains, Value: ""},
		{Field: FieldTitle, Op: OpMatches, Value: "[unclosed"}, // bad regex
	}
	for _, m := range invalid {
		if err := m.Validate(); err == nil {
			t.Errorf("expected error for %+v", m)
		}
	}
}

func TestParseMatch(t *testing.T) {
	m, err := ParseMatch(`{"field":"title","op":"contains","value":"foo"}`)
	if err != nil {
		t.Fatal(err)
	}
	if m.Field != FieldTitle || m.Op != OpContains || m.Value != "foo" {
		t.Errorf("parsed %+v", m)
	}
	if _, err := ParseMatch(`{not json`); err == nil {
		t.Error("expected parse error")
	}
	if _, err := ParseMatch(`{"field":"x","op":"contains","value":"y"}`); err == nil {
		t.Error("expected validation error")
	}
}

func TestValidateAction(t *testing.T) {
	for _, a := range []string{"mark_read", "star", "hide"} {
		if err := ValidateAction(a); err != nil {
			t.Errorf("expected %q valid: %v", a, err)
		}
	}
	if err := ValidateAction("delete_all_files"); err == nil {
		t.Error("expected invalid action error")
	}
}

func TestApply(t *testing.T) {
	a := art("Crypto pump and dump", "scam", "spammer", "https://x.test")
	filters := []models.Filter{
		{Name: "hide crypto", Action: "hide",
			MatchJSON: `{"field":"title","op":"contains","value":"crypto"}`,
			Enabled:   true},
		{Name: "star alice posts", Action: "star",
			MatchJSON: `{"field":"author","op":"equals","value":"alice"}`,
			Enabled:   true},
		{Name: "disabled", Action: "star",
			MatchJSON: `{"field":"title","op":"contains","value":"crypto"}`,
			Enabled:   false},
		{Name: "bad json", Action: "mark_read",
			MatchJSON: `not json`, Enabled: true},
	}

	out := Apply(filters, a)
	if !out.Hide {
		t.Errorf("expected Hide=true")
	}
	if !out.MarkRead {
		t.Errorf("Hide should also imply MarkRead")
	}
	if out.Star {
		t.Errorf("Star should be false (different author)")
	}
}

func TestApply_NoMatches(t *testing.T) {
	a := art("Ordinary article", "normal content", "alice", "https://x.test")
	filters := []models.Filter{
		{Action: "mark_read",
			MatchJSON: `{"field":"title","op":"contains","value":"crypto"}`,
			Enabled:   true},
	}
	out := Apply(filters, a)
	if out.Any() {
		t.Errorf("expected no actions, got %+v", out)
	}
}

func TestMatches_RegexCaseInsensitive(t *testing.T) {
	a := art("HelLo WoRld", "", "", "")
	m := Match{Field: FieldTitle, Op: OpMatches, Value: "hello.+world"}
	if !Matches(m, a) {
		t.Error("expected case-insensitive regex match")
	}
}

func TestMatches_RegexCaseSensitive(t *testing.T) {
	a := art("HelLo WoRld", "", "", "")
	m := Match{Field: FieldTitle, Op: OpMatches, Value: "hello.+world", CaseSensitive: true}
	if Matches(m, a) {
		t.Error("expected no match (case sensitive)")
	}
}
