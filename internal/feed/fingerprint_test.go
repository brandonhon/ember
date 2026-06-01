package feed

import "testing"

func TestTitleFingerprint(t *testing.T) {
	cases := []struct {
		name  string
		title string
		want  string
	}{
		{"empty", "", ""},
		{"plain", "Hello world today", "hello world today"},
		{"strip punctuation", "Hello, world!", "hello world"},
		{"strip stopwords", "The Best Hack of 2026", "best hack 2026"},
		{"reorder-insensitive only via tokens being kept", "Best hack of 2026", "best hack 2026"},
		{"case insensitive", "OPENAI launches GPT-7", "openai launches gpt 7"},
		{"unicode letters preserved", "Café opens in Paris", "café opens paris"},
		{"too short → empty", "Re:", ""},
		{"too short stopwords-only", "the and of", ""},
		{"borderline length passes at 8 chars", "ab cd ef", "ab cd ef"},
		{"slashes collapse to space", "AI/ML round-up", "ai ml round up"},
		{"6-char tokens-only rejected", "AI ML", ""},
		{"long enough alphanumeric", "Apple Q3 earnings beat", "apple q3 earnings beat"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TitleFingerprint(tc.title)
			if got != tc.want {
				t.Errorf("TitleFingerprint(%q) = %q, want %q", tc.title, got, tc.want)
			}
		})
	}
}

func TestTitleFingerprint_CollapsesSyndication(t *testing.T) {
	// The whole point: same article, different outlet wording (article vs
	// "the" + article, em-dash vs colon, etc.) should NOT cluster unless
	// the meaningful tokens match. We only want exact-token equivalence
	// to cluster; near-matches stay distinct.
	a := TitleFingerprint("OpenAI launches GPT-7")
	b := TitleFingerprint("OpenAI launches GPT-7")
	c := TitleFingerprint("OpenAI Launches GPT-7!")
	d := TitleFingerprint("openai-launches-gpt-7")
	e := TitleFingerprint("OpenAI launches GPT-7 — sources")
	if a != b || a != c || a != d {
		t.Errorf("expected punctuation/case variants to share fingerprint; got %q / %q / %q / %q", a, b, c, d)
	}
	if a == e {
		t.Errorf("expected fingerprint to diverge when title has extra meaningful tokens; got %q for both", a)
	}
}
