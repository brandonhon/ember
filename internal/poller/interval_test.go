package poller

import (
	"testing"
	"time"
)

func TestAdaptiveInterval(t *testing.T) {
	cases := []struct {
		name string
		in   IntervalInputs
		want time.Duration
	}{
		{
			name: "many new -> halve",
			in:   IntervalInputs{NewArticles: 5, Current: time.Hour},
			want: 30 * time.Minute,
		},
		{
			name: "no new -> grow",
			in:   IntervalInputs{NewArticles: 0, Current: time.Hour},
			want: 90 * time.Minute,
		},
		{
			name: "1 new -> hold",
			in:   IntervalInputs{NewArticles: 1, Current: time.Hour},
			want: time.Hour,
		},
		{
			name: "error -> double (and again)",
			in:   IntervalInputs{HadError: true, ErrorCount: 2, Current: 30 * time.Minute},
			want: 2 * time.Hour, // 30m << 2 = 2h
		},
		{
			name: "clamped to MinInterval",
			in:   IntervalInputs{NewArticles: 10, Current: time.Minute},
			want: MinInterval,
		},
		{
			name: "clamped to MaxInterval",
			in:   IntervalInputs{HadError: true, ErrorCount: 10, Current: 24 * time.Hour},
			want: MaxInterval,
		},
		{
			name: "zero current -> default 30m, no new -> grow",
			in:   IntervalInputs{NewArticles: 0, Current: 0},
			want: 45 * time.Minute,
		},
		{
			name: "error count zero clamped to 1",
			in:   IntervalInputs{HadError: true, ErrorCount: 0, Current: time.Hour},
			want: 2 * time.Hour,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AdaptiveInterval(tc.in)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
