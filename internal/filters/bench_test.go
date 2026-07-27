package filters

import (
	"fmt"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
)

func benchRules(n int, op Op, value string) []models.Filter {
	out := make([]models.Filter, 0, n)
	for i := range n {
		out = append(out, models.Filter{
			ID: int64(i + 1), Enabled: true, Priority: 100,
			MatchJSON: fmt.Sprintf(`{"field":"title","op":%q,"value":%q}`, op, value+fmt.Sprint(i)),
			Action:    string(ActionStar),
		})
	}
	return out
}

func BenchmarkApply_RegexRules(b *testing.B) {
	rules := benchRules(10, OpMatches, "break(ing|down)-")
	a := models.Article{Title: "Breaking-9 news about things", FeedID: 1}
	now := time.Now()
	b.ReportAllocs()
	for b.Loop() {
		_ = Apply(rules, a, now)
	}
}

func BenchmarkApply_ContainsRules(b *testing.B) {
	rules := benchRules(10, OpContains, "news")
	a := models.Article{Title: "Breaking news about things", FeedID: 1}
	now := time.Now()
	b.ReportAllocs()
	for b.Loop() {
		_ = Apply(rules, a, now)
	}
}
