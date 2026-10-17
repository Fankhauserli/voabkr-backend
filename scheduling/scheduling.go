// Package scheduling owns the Review schedule a Grade leads to. It is pure: a
// stored state, a Grade and the moment it was answered decide the next schedule,
// with no clock, database or caller involved.
package scheduling

import "time"

// Grade is the answer a learner gave, numbered as the wire numbers it.
type Grade int32

const (
	Again Grade = 1
	Hard  Grade = 2
	Good  Grade = 3
	Easy  Grade = 4
)

// ParseGrade returns the Grade numbered by value. A value outside the four is
// not a Grade: ok is false. This is how a request's ease becomes a Grade, so
// admission is not re-spelled at each route.
func ParseGrade(value int32) (Grade, bool) {
	switch Grade(value) {
	case Again, Hard, Good, Easy:
		return Grade(value), true
	default:
		return 0, false
	}
}

// State is what a stored Review remembers between answers: its Interval in days
// (0 while relearning), its Efactor, and how many times it has been answered.
type State struct {
	Interval    int32
	Efactor     float64
	Repetitions int32
}

// Schedule is the state after an answer, and the moment the Review comes due.
type Schedule struct {
	Interval     int32
	Efactor      float64
	Repetitions  int32
	NextReviewAt time.Time
}

// Next returns the Review schedule after grade, answered at now. grade must be
// one of the four Grades; ParseGrade is the only way a request's value becomes
// one. The rules are the ones this backend has always applied:
//
//   - Again relearns in a minute, whatever the state.
//   - Hard relearns in six minutes, or grows a graduated Interval by 1.2.
//   - Good takes a first graduation to six days, otherwise grows the Interval by
//     the Efactor, and always by at least one day.
//   - Easy skips relearning to at least four days, otherwise grows the Interval
//     by the Efactor and 1.3.
//
// The Efactor follows the Anki remap and never falls below 1.3. Every answer
// counts one more repetition.
func Next(state State, grade Grade, now time.Time) Schedule {
	// Anki formula: EF' = EF + (0.1 - (3-q)*(0.08 + (3-q)*0.02)), with the four
	// Grades remapped to Anki's 0-4 scale.
	ankiQ := int32(grade) - 1
	efactor := state.Efactor + (0.1 - float64(3-ankiQ)*(0.08+float64(3-ankiQ)*0.02))
	if efactor < 1.3 {
		efactor = 1.3
	}

	var (
		interval     int32
		nextReviewAt time.Time
	)

	// The four Grades are exhaustive: ParseGrade admits no other value.
	switch grade {
	case Again:
		interval = 0
		nextReviewAt = now.Add(time.Minute)

	case Hard:
		if state.Interval == 0 {
			interval = 0
			nextReviewAt = now.Add(6 * time.Minute)
		} else {
			interval = int32(float64(state.Interval) * 1.2)
			if interval < 1 {
				interval = 1
			}
			nextReviewAt = now.AddDate(0, 0, int(interval))
		}

	case Good:
		if state.Interval == 0 {
			interval = 0
			nextReviewAt = now.Add(10 * time.Minute)
		} else if state.Interval == 1 {
			interval = 6
			nextReviewAt = now.AddDate(0, 0, 6)
		} else {
			interval = int32(float64(state.Interval) * efactor)
			if interval <= state.Interval {
				interval = state.Interval + 1
			}
			nextReviewAt = now.AddDate(0, 0, int(interval))
		}

	case Easy:
		if state.Interval < 4 {
			interval = 4
		} else {
			interval = int32(float64(state.Interval) * efactor * 1.3)
		}
		if interval < 4 {
			interval = 4
		}
		nextReviewAt = now.AddDate(0, 0, int(interval))
	}

	return Schedule{
		Interval:     interval,
		Efactor:      efactor,
		Repetitions:  state.Repetitions + 1,
		NextReviewAt: nextReviewAt,
	}
}
