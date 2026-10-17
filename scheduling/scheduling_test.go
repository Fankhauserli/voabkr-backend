package scheduling

import (
	"math"
	"testing"
	"time"
)

// now is fixed so an expected moment is a literal rather than a second reading
// of the clock.
var now = time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC)

// A request's ease becomes a Grade only when it numbers one of the four.
func TestParseGradeAdmitsOnlyTheFour(t *testing.T) {
	tests := []struct {
		value int32
		want  Grade
		ok    bool
	}{
		{1, Again, true},
		{2, Hard, true},
		{3, Good, true},
		{4, Easy, true},
		{-1, 0, false},
		{0, 0, false},
		{5, 0, false},
	}

	for _, tt := range tests {
		grade, ok := ParseGrade(tt.value)
		if ok != tt.ok || grade != tt.want {
			t.Errorf("ParseGrade(%d) = (%v, %v), want (%v, %v)", tt.value, grade, ok, tt.want, tt.ok)
		}
	}
}

// A stored Review state crossed with each of the four Grades reproduces the
// schedule this backend computed before the scheduler was extracted. The
// expected values are the old handler's, worked out by hand: a first answer
// graduates, Again and Hard relearn in minutes, Good and Easy count days, an
// Interval always makes progress, and the Efactor never falls below 1.3.
func TestNextReproducesTheLegacySchedule(t *testing.T) {
	const efactorTolerance = 1e-9

	tests := []struct {
		name  string
		state State
		grade Grade
		want  Schedule
	}{
		// A Review row the schema has just created: Interval 1 is the first
		// graduation, not one day in.
		{
			"first answer marked Again",
			State{Interval: 1, Efactor: 2.5, Repetitions: 1},
			Again,
			Schedule{Interval: 0, Efactor: 2.18, Repetitions: 2, NextReviewAt: now.Add(time.Minute)},
		},
		{
			"first answer marked Hard",
			State{Interval: 1, Efactor: 2.5, Repetitions: 1},
			Hard,
			Schedule{Interval: 1, Efactor: 2.36, Repetitions: 2, NextReviewAt: now.AddDate(0, 0, 1)},
		},
		{
			"first answer marked Good",
			State{Interval: 1, Efactor: 2.5, Repetitions: 1},
			Good,
			Schedule{Interval: 6, Efactor: 2.5, Repetitions: 2, NextReviewAt: now.AddDate(0, 0, 6)},
		},
		{
			"first answer marked Easy",
			State{Interval: 1, Efactor: 2.5, Repetitions: 1},
			Easy,
			Schedule{Interval: 4, Efactor: 2.6, Repetitions: 2, NextReviewAt: now.AddDate(0, 0, 4)},
		},

		// A Card in relearning: its Interval stays 0 and the short steps keep it
		// there.
		{
			"relearning marked Again",
			State{Interval: 0, Efactor: 2.5, Repetitions: 2},
			Again,
			Schedule{Interval: 0, Efactor: 2.18, Repetitions: 3, NextReviewAt: now.Add(time.Minute)},
		},
		{
			"relearning marked Hard",
			State{Interval: 0, Efactor: 2.5, Repetitions: 2},
			Hard,
			Schedule{Interval: 0, Efactor: 2.36, Repetitions: 3, NextReviewAt: now.Add(6 * time.Minute)},
		},
		{
			"relearning marked Good",
			State{Interval: 0, Efactor: 2.5, Repetitions: 2},
			Good,
			Schedule{Interval: 0, Efactor: 2.5, Repetitions: 3, NextReviewAt: now.Add(10 * time.Minute)},
		},
		{
			"relearning marked Easy",
			State{Interval: 0, Efactor: 2.5, Repetitions: 2},
			Easy,
			Schedule{Interval: 4, Efactor: 2.6, Repetitions: 3, NextReviewAt: now.AddDate(0, 0, 4)},
		},

		// A graduated Card: days now, scaled by the Grade.
		{
			"graduated marked Again",
			State{Interval: 6, Efactor: 2.5, Repetitions: 3},
			Again,
			Schedule{Interval: 0, Efactor: 2.18, Repetitions: 4, NextReviewAt: now.Add(time.Minute)},
		},
		{
			"graduated marked Hard",
			State{Interval: 6, Efactor: 2.5, Repetitions: 3},
			Hard,
			Schedule{Interval: 7, Efactor: 2.36, Repetitions: 4, NextReviewAt: now.AddDate(0, 0, 7)},
		},
		{
			"graduated marked Good",
			State{Interval: 6, Efactor: 2.5, Repetitions: 3},
			Good,
			Schedule{Interval: 15, Efactor: 2.5, Repetitions: 4, NextReviewAt: now.AddDate(0, 0, 15)},
		},
		{
			"graduated marked Easy",
			State{Interval: 6, Efactor: 2.5, Repetitions: 3},
			Easy,
			Schedule{Interval: 20, Efactor: 2.6, Repetitions: 4, NextReviewAt: now.AddDate(0, 0, 20)},
		},

		// An Interval the Efactor can no longer grow: Good still moves it by a
		// day rather than leaving it where it was.
		{
			"low interval at the Efactor floor marked Good",
			State{Interval: 2, Efactor: 1.3, Repetitions: 5},
			Good,
			Schedule{Interval: 3, Efactor: 1.3, Repetitions: 6, NextReviewAt: now.AddDate(0, 0, 3)},
		},

		// Every Grade at the Efactor floor keeps the Efactor at 1.3.
		{
			"Efactor floor marked Again",
			State{Interval: 10, Efactor: 1.3, Repetitions: 4},
			Again,
			Schedule{Interval: 0, Efactor: 1.3, Repetitions: 5, NextReviewAt: now.Add(time.Minute)},
		},
		{
			"Efactor floor marked Hard",
			State{Interval: 10, Efactor: 1.3, Repetitions: 4},
			Hard,
			Schedule{Interval: 12, Efactor: 1.3, Repetitions: 5, NextReviewAt: now.AddDate(0, 0, 12)},
		},
		{
			"Efactor floor marked Good",
			State{Interval: 10, Efactor: 1.3, Repetitions: 4},
			Good,
			Schedule{Interval: 13, Efactor: 1.3, Repetitions: 5, NextReviewAt: now.AddDate(0, 0, 13)},
		},
		{
			"Efactor floor marked Easy",
			State{Interval: 10, Efactor: 1.3, Repetitions: 4},
			Easy,
			Schedule{Interval: 18, Efactor: 1.4, Repetitions: 5, NextReviewAt: now.AddDate(0, 0, 18)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Next(tt.state, tt.grade, now)

			if got.Interval != tt.want.Interval {
				t.Errorf("Interval = %d, want %d", got.Interval, tt.want.Interval)
			}
			if math.Abs(got.Efactor-tt.want.Efactor) > efactorTolerance {
				t.Errorf("Efactor = %g, want %g", got.Efactor, tt.want.Efactor)
			}
			if got.Repetitions != tt.want.Repetitions {
				t.Errorf("Repetitions = %d, want %d", got.Repetitions, tt.want.Repetitions)
			}
			if !got.NextReviewAt.Equal(tt.want.NextReviewAt) {
				t.Errorf("NextReviewAt = %s, want %s", got.NextReviewAt, tt.want.NextReviewAt)
			}
		})
	}
}
