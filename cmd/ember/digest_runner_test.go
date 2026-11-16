package main

import (
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
)

// dueDigest must fire once per scheduled occurrence no matter where the
// 5-minute ticks land relative to midnight. It used to require a tick between
// today's scheduled time and the next UTC midnight, so a 23:59 schedule ticked
// at 23:57 and 00:02 never fired, and downtime spanning midnight skipped the
// day entirely.
func TestDueDigest(t *testing.T) {
	at := func(day, hour, minute int) time.Time {
		return time.Date(2026, 11, day, hour, minute, 0, 0, time.UTC)
	}
	cases := []struct {
		name       string
		hour, min  int
		lastSent   time.Time // zero = never sent
		now        time.Time
		wantFiring bool
	}{
		{"before today's time, sent yesterday", 8, 0, at(14, 8, 1), at(15, 7, 59), false},
		{"at today's time, sent yesterday", 8, 0, at(14, 8, 1), at(15, 8, 0), true},
		{"after today's time, already sent today", 8, 0, at(15, 8, 2), at(15, 9, 0), false},
		// A digest that has never been sent waits for its first scheduled time
		// rather than firing the moment it's enabled (and again at 08:00).
		{"never sent, before today's time", 8, 0, time.Time{}, at(15, 7, 0), false},
		{"never sent, after today's time", 8, 0, time.Time{}, at(15, 8, 3), true},
		// 23:59 schedule, ticks at 23:57 then 00:02: the first tick is early,
		// the second lands after midnight and must still catch the 23:59 run.
		{"23:59 tick before", 23, 59, at(14, 0, 2), at(14, 23, 57), false},
		{"23:59 tick after midnight", 23, 59, at(14, 0, 2), at(15, 0, 2), true},
		{"23:59 after-midnight catch-up already sent", 23, 59, at(15, 0, 2), at(15, 0, 7), false},
		// Down from before the 08:00 run until the next morning before 08:00:
		// yesterday's occurrence was never sent and is still owed.
		{"downtime across midnight", 8, 0, at(13, 8, 1), at(15, 3, 0), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := models.UserDigest{Enabled: true, HourUTC: tc.hour, MinuteUTC: tc.min}
			if !tc.lastSent.IsZero() {
				d.LastSentAt = tc.lastSent.Unix()
			}
			if got := dueDigest(d, tc.now); got != tc.wantFiring {
				t.Errorf("dueDigest(%02d:%02d, last=%v, now=%v) = %v, want %v",
					tc.hour, tc.min, tc.lastSent, tc.now, got, tc.wantFiring)
			}
		})
	}
	if dueDigest(models.UserDigest{Enabled: false}, at(15, 12, 0)) {
		t.Error("disabled digest reported due")
	}
}
