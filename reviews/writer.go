package reviews

// Every Grade write — enrolling a Card, storing the schedules a device computed,
// and scheduling one here for the legacy rating route — asks the one Deck-access
// gate. Every refusal is a typed outcome rather than a sentence: the words a
// client reads are the handler's to choose.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/decks"
	"github.com/Fankhauserli/voabkr-backend/scheduling"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrUnknownCard is returned when the Card a write names is not there.
var ErrUnknownCard = errors.New("unknown Card")

// ErrNoAccess is returned when the caller may not read the Card's Deck.
var ErrNoAccess = errors.New("no access to this Deck")

// The legacy rating's store stages, so its route can answer a failure in the
// words it has always used.
var (
	ErrReadReview   = errors.New("failed to read the Review")
	ErrCreateReview = errors.New("failed to create the Review")
	ErrStoreReview  = errors.New("failed to store the Review")
)

// Outcome is how one Grade write ended.
type Outcome int

const (
	// Unspecified is the zero Outcome: no write has ended this way.
	Unspecified Outcome = iota

	// Recorded is a Grade stored as given.
	Recorded

	// Replay is a Grade answered no later than the one already stored: it is
	// accepted and changes nothing.
	Replay

	// NoSchedule is an entry carrying no Review schedule.
	NoSchedule

	// InvalidGrade is a Grade outside the four the scheduler knows.
	InvalidGrade

	// InvalidInterval is a negative Interval.
	InvalidInterval

	// InvalidEfactor is an Efactor that is not positive.
	InvalidEfactor

	// InvalidRepetitions is a negative repetition count.
	InvalidRepetitions

	// InvalidAnsweredAt is an answer moment that is not RFC 3339.
	InvalidAnsweredAt

	// InvalidNextReviewAt is a due moment that is not RFC 3339.
	InvalidNextReviewAt

	// UnknownCard is a Card that is not there.
	UnknownCard

	// NoAccess is a Card whose Deck the caller may not read.
	NoAccess

	// StoreFailed is a Grade the database could not read or store.
	StoreFailed
)

// Accepted reports whether the Grade was taken: recorded now, or already
// recorded.
func (o Outcome) Accepted() bool {
	return o == Recorded || o == Replay
}

// GradeOutcome is one entry's answer: the Card it named and how the write ended.
type GradeOutcome struct {
	CardID  uint32
	Outcome Outcome
}

// Writer stores Grades for the learner asking.
type Writer struct {
	db    *db.Queries
	decks *decks.Reader
}

// NewWriter returns a Writer over the database Grades are stored in, settling
// access through the same Deck reader every Deck-scoped read uses.
func NewWriter(q *db.Queries, d *decks.Reader) *Writer {
	return &Writer{db: q, decks: d}
}

// Enroll adds a Card to the learner's Reviews. The Card must be there and its
// Deck readable, by the same gate every Grade write asks.
func (w *Writer) Enroll(ctx context.Context, caller, cardID int64) error {
	if _, err := w.readableCard(ctx, caller, cardID); err != nil {
		return err
	}

	_, err := w.db.CreateUserCard(ctx, db.CreateUserCardParams{
		UserID: caller,
		CardID: cardID,
	})
	return err
}

// ApplyBatch stores the Grades a device scheduled. Each entry is settled on its own,
// so one refusal is one entry's answer; the outcomes are in the entries' order.
func (w *Writer) ApplyBatch(ctx context.Context, caller int64, entries []*types.GradeEntry) []GradeOutcome {
	outcomes := make([]GradeOutcome, 0, len(entries))
	for _, entry := range entries {
		outcomes = append(outcomes, GradeOutcome{
			CardID:  entry.CardId,
			Outcome: w.apply(ctx, caller, entry),
		})
	}
	return outcomes
}

// apply settles one Grade the device scheduled.
func (w *Writer) apply(ctx context.Context, caller int64, entry *types.GradeEntry) Outcome {
	grade, outcome, ok := parseGrade(entry)
	if !ok {
		return outcome
	}

	if _, err := w.readableCard(ctx, caller, int64(entry.CardId)); err != nil {
		switch {
		case errors.Is(err, ErrUnknownCard):
			return UnknownCard
		case errors.Is(err, ErrNoAccess), errors.Is(err, decks.ErrNoSuchDeck):
			return NoAccess
		default:
			return StoreFailed
		}
	}

	stored, err := w.db.ApplyGrade(ctx, db.ApplyGradeParams{
		UserID:         caller,
		CardID:         int64(entry.CardId),
		Efactor:        grade.efactor,
		Interval:       grade.interval,
		Repetitions:    grade.repetitions,
		LastReviewedAt: pgtype.Timestamptz{Time: grade.answeredAt, Valid: true},
		NextReviewAt:   pgtype.Timestamptz{Time: grade.nextReviewAt, Valid: true},
	})
	if err != nil {
		return StoreFailed
	}

	// A Grade answered no later than the one already stored is a replay: it is
	// accepted, so the device can drop it, and it changes nothing.
	if stored == 0 {
		return Replay
	}
	return Recorded
}

// ApplyLegacy schedules a Grade here, the way this backend did before the device took
// the scheduler over. The stored Review is the state it continues from; only a
// Review that is not there is created.
func (w *Writer) ApplyLegacy(ctx context.Context, caller, cardID int64, grade scheduling.Grade) error {
	if _, err := w.readableCard(ctx, caller, cardID); err != nil {
		return err
	}

	review, err := w.db.GetUserCard(ctx, db.GetUserCardParams{
		UserID: caller,
		CardID: cardID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		review, err = w.db.CreateUserCard(ctx, db.CreateUserCardParams{
			UserID: caller,
			CardID: cardID,
		})
		if err != nil {
			return fmt.Errorf("%w: %v", ErrCreateReview, err)
		}
	case err != nil:
		return fmt.Errorf("%w: %v", ErrReadReview, err)
	}

	now := time.Now()
	next := scheduling.Next(scheduling.State{
		Interval:    review.Interval,
		Efactor:     review.Efactor,
		Repetitions: review.Repetitions,
	}, grade, now)

	err = w.db.UpdateUserCard(ctx, db.UpdateUserCardParams{
		UserID:         caller,
		CardID:         cardID,
		Efactor:        next.Efactor,
		Interval:       next.Interval,
		Repetitions:    next.Repetitions,
		LastReviewedAt: pgtype.Timestamptz{Time: now, Valid: true},
		NextReviewAt:   pgtype.Timestamptz{Time: next.NextReviewAt, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreReview, err)
	}
	return nil
}

// readableCard is the one Deck-access gate every Grade write asks: the Card must
// be there, and the caller must be able to read the Deck that holds it. A Deck
// that is gone is not readable, and arrives as decks.ErrNoSuchDeck.
func (w *Writer) readableCard(ctx context.Context, caller, cardID int64) (db.Card, error) {
	card, err := w.db.GetCard(ctx, cardID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return db.Card{}, ErrUnknownCard
	case err != nil:
		return db.Card{}, err
	}

	standing, err := w.decks.StandingOn(ctx, card.DeckID, caller)
	if err != nil {
		return db.Card{}, err
	}
	if !access.CanRead(standing.Deck, standing.Role) {
		return db.Card{}, ErrNoAccess
	}
	return card, nil
}

// scheduledGrade is a Grade entry read into the shapes the store needs: the
// Review schedule the device computed, and the two moments it carries.
type scheduledGrade struct {
	interval     int32
	efactor      float64
	repetitions  int32
	answeredAt   time.Time
	nextReviewAt time.Time
}

// parseGrade reads a Grade entry, refusing anything the backend could not store
// exactly as given. ok is false when it did, and outcome names the first thing
// wrong with the entry.
func parseGrade(entry *types.GradeEntry) (scheduledGrade, Outcome, bool) {
	schedule := entry.Schedule
	switch {
	case schedule == nil:
		return scheduledGrade{}, NoSchedule, false
	case entry.Grade < 1 || entry.Grade > 4:
		return scheduledGrade{}, InvalidGrade, false
	case schedule.Interval < 0:
		return scheduledGrade{}, InvalidInterval, false
	case schedule.Efactor <= 0:
		return scheduledGrade{}, InvalidEfactor, false
	case schedule.Repetitions < 0:
		return scheduledGrade{}, InvalidRepetitions, false
	}

	answeredAt, err := time.Parse(time.RFC3339, entry.AnsweredAt)
	if err != nil {
		return scheduledGrade{}, InvalidAnsweredAt, false
	}

	nextReviewAt, err := time.Parse(time.RFC3339, schedule.NextReviewAt)
	if err != nil {
		return scheduledGrade{}, InvalidNextReviewAt, false
	}

	return scheduledGrade{
		interval:     schedule.Interval,
		efactor:      schedule.Efactor,
		repetitions:  schedule.Repetitions,
		answeredAt:   answeredAt,
		nextReviewAt: nextReviewAt,
	}, Unspecified, true
}
