package poller

import (
	"testing"
	"time"
)

func TestAdaptiveInterval(t *testing.T) {
	cases := []struct {
		name         string
		in           IntervalInputs
		minIv, maxIv time.Duration // zero → defaults (5m, 6h)
		want         time.Duration
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
			name: "clamped to floor (default 5m)",
			in:   IntervalInputs{NewArticles: 10, Current: time.Minute},
			want: 5 * time.Minute,
		},
		{
			name: "clamped to MaxInterval",
			in:   IntervalInputs{HadError: true, ErrorCount: 10, Current: 24 * time.Hour},
			want: MaxInterval,
		},
		{
			name:  "zero current -> starts at floor, no new -> grow",
			in:    IntervalInputs{NewArticles: 0, Current: 0},
			want:  45 * time.Minute, // floor 30m * 1.5
			minIv: 30 * time.Minute,
		},
		{
			name: "error count zero clamped to 1",
			in:   IntervalInputs{HadError: true, ErrorCount: 0, Current: time.Hour},
			want: 2 * time.Hour,
		},
		{
			name:  "configurable floor: many new clamps to 30m, not 5m",
			in:    IntervalInputs{NewArticles: 10, Current: 40 * time.Minute},
			minIv: 30 * time.Minute,
			want:  30 * time.Minute,
		},
		{
			name:  "floor above MaxInterval raises the ceiling to the floor",
			in:    IntervalInputs{NewArticles: 0, Current: 8 * time.Hour},
			minIv: 8 * time.Hour,
			maxIv: 6 * time.Hour, // < floor; AdaptiveInterval lifts it to 8h
			want:  8 * time.Hour,
		},
		{
			name:  "zero floor falls back to 30m default",
			in:    IntervalInputs{NewArticles: 10, Current: time.Minute},
			minIv: -1, // treated as unset → fallbackMinInterval (30m)
			want:  30 * time.Minute,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			min, max := tc.minIv, tc.maxIv
			if min == 0 {
				min = 5 * time.Minute
			}
			if max == 0 {
				max = 6 * time.Hour
			}
			got := AdaptiveInterval(tc.in, min, max)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
