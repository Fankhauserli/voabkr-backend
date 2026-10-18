package reviews_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/decks"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/reviews"
	"github.com/Fankhauserli/voabkr-backend/scheduling"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
)

// newWriter returns a Review writer over the test database, settling access
// through the Deck reader the handlers use.
func newWriter(t *testing.T) (*reviews.Writer, *db.Queries) {
	t.Helper()

	queries := dbtest.Setup(t)
	return reviews.NewWriter(queries, decks.NewReader(queries, nil)), queries
}

// deviceEntry is one Grade as a device sends it, carrying the schedule it
// computed.
func deviceEntry(cardID int64, grade int32, interval int32, efactor float64, repetitions int32, answeredAt, nextReviewAt time.Time) *types.GradeEntry {
	return &types.GradeEntry{
		CardId:     uint32(cardID),
		Grade:      grade,
		AnsweredAt: answeredAt.UTC().Format(time.RFC3339),
		Schedule: &types.ReviewSchedule{
			Interval:     interval,
			Efactor:      efactor,
			Repetitions:  repetitions,
			NextReviewAt: nextReviewAt.UTC().Format(time.RFC3339),
		},
	}
}

// applyBatchOne applies one Grade and returns its outcome.
func applyBatchOne(t *testing.T, writer *reviews.Writer, caller int64, entry *types.GradeEntry) reviews.Outcome {
	t.Helper()

	outcomes := writer.ApplyBatch(context.Background(), caller, []*types.GradeEntry{entry})
	if len(outcomes) != 1 {
		t.Fatalf("expected one outcome, got %d", len(outcomes))
	}
	return outcomes[0].Outcome
}

// storedReview returns the learner's Review of cardID.
func storedReview(t *testing.T, queries *db.Queries, learner, cardID int64) db.UserCard {
	t.Helper()

	review, err := queries.GetUserCard(context.Background(), db.GetUserCardParams{UserID: learner, CardID: cardID})
	if err != nil {
		t.Fatalf("expected a stored Review, got %v", err)
	}
	return review
}

// Enrolling a Card stores a Review for a Deck the learner may read.
func TestEnrollStoresAReview(t *testing.T) {
	writer, queries := newWriter(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")

	if err := writer.Enroll(context.Background(), learner, cardID); err != nil {
		t.Fatalf("failed to enroll the Card: %v", err)
	}

	storedReview(t, queries, learner, cardID)
}

// Enrolling applies the same Deck-access gate as every other Grade write.
func TestEnrollRefusesAnUnreadableDeck(t *testing.T) {
	writer, queries := newWriter(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	stranger := dbtest.SeedUser(t, "stranger@example.com")
	deckID := dbtest.SeedDeck(t, stranger, false)
	cardID := dbtest.SeedCard(t, deckID, "남의", "someone else's")

	err := writer.Enroll(context.Background(), learner, cardID)
	if !errors.Is(err, reviews.ErrNoAccess) {
		t.Fatalf("expected ErrNoAccess, got %v", err)
	}

	if _, err := queries.GetUserCard(context.Background(), db.GetUserCardParams{UserID: learner, CardID: cardID}); err == nil {
		t.Fatal("expected no Review for an unreadable Deck")
	}
}

// A Deck that is gone is not readable, on either single-Card path.
func TestEnrollRefusesADeletedDeck(t *testing.T) {
	writer, queries := newWriter(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "지운", "deleted")
	if err := queries.DeleteDeck(context.Background(), deckID); err != nil {
		t.Fatalf("failed to delete the Deck: %v", err)
	}

	err := writer.Enroll(context.Background(), learner, cardID)
	if !errors.Is(err, decks.ErrNoSuchDeck) {
		t.Fatalf("expected decks.ErrNoSuchDeck, got %v", err)
	}
}

// A Card that is not there cannot be enrolled.
func TestEnrollRefusesAnUnknownCard(t *testing.T) {
	writer, _ := newWriter(t)
	learner := dbtest.SeedUser(t, "learner@example.com")

	err := writer.Enroll(context.Background(), learner, 9999)
	if !errors.Is(err, reviews.ErrUnknownCard) {
		t.Fatalf("expected ErrUnknownCard, got %v", err)
	}
}

// A device-computed schedule is stored whole.
func TestApplyBatchStoresTheDeviceSchedule(t *testing.T) {
	writer, queries := newWriter(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")

	answeredAt := time.Now().Add(-time.Hour).Truncate(time.Second)
	nextReviewAt := answeredAt.Add(6 * 24 * time.Hour)

	if got := applyBatchOne(t, writer, learner, deviceEntry(cardID, 3, 6, 2.36, 3, answeredAt, nextReviewAt)); got != reviews.Recorded {
		t.Fatalf("expected Recorded, got %v", got)
	}

	review := storedReview(t, queries, learner, cardID)
	if review.Interval != 6 || review.Efactor != 2.36 || review.Repetitions != 3 {
		t.Fatalf("expected the device's schedule 6/2.36/3, got %d/%g/%d", review.Interval, review.Efactor, review.Repetitions)
	}
	if !review.LastReviewedAt.Time.Equal(answeredAt) || !review.NextReviewAt.Time.Equal(nextReviewAt) {
		t.Fatalf("expected the device's moments, got answered %v due %v", review.LastReviewedAt, review.NextReviewAt)
	}
}

// Each entry is settled on its own: one refusal does not fail the entries
// around it.
func TestApplyBatchRefusesOneEntryWithoutFailingTheBatch(t *testing.T) {
	writer, queries := newWriter(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	stranger := dbtest.SeedUser(t, "stranger@example.com")
	ownDeck := dbtest.SeedDeck(t, learner, false)
	ownCard := dbtest.SeedCard(t, ownDeck, "내것", "mine")
	strangerDeck := dbtest.SeedDeck(t, stranger, false)
	strangerCard := dbtest.SeedCard(t, strangerDeck, "남의", "someone else's")

	answeredAt := time.Now().Add(-time.Hour).Truncate(time.Second)
	nextReviewAt := answeredAt.Add(24 * time.Hour)

	outcomes := writer.ApplyBatch(context.Background(), learner, []*types.GradeEntry{
		deviceEntry(ownCard, 3, 6, 2.5, 1, answeredAt, nextReviewAt),
		deviceEntry(9999, 3, 6, 2.5, 1, answeredAt, nextReviewAt),
		deviceEntry(strangerCard, 3, 6, 2.5, 1, answeredAt, nextReviewAt),
	})

	if len(outcomes) != 3 {
		t.Fatalf("expected an outcome per entry, got %d", len(outcomes))
	}
	if outcomes[0].Outcome != reviews.Recorded || outcomes[0].CardID != uint32(ownCard) {
		t.Fatalf("expected the learner's own Card recorded, got %+v", outcomes[0])
	}
	if outcomes[1].Outcome != reviews.UnknownCard {
		t.Fatalf("expected an unknown Card, got %+v", outcomes[1])
	}
	if outcomes[2].Outcome != reviews.NoAccess {
		t.Fatalf("expected an unreadable Deck, got %+v", outcomes[2])
	}

	storedReview(t, queries, learner, ownCard)
	if _, err := queries.GetUserCard(context.Background(), db.GetUserCardParams{UserID: learner, CardID: strangerCard}); err == nil {
		t.Fatal("expected no Review for an unreadable Deck")
	}
}

// A replayed Grade is accepted and changes nothing; between two answers the
// later one wins.
func TestApplyBatchReplayIsAcceptedAndChangesNothing(t *testing.T) {
	writer, queries := newWriter(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")

	first := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	firstDue := first.Add(6 * 24 * time.Hour)

	entry := deviceEntry(cardID, 3, 6, 2.36, 1, first, firstDue)
	if got := applyBatchOne(t, writer, learner, entry); got != reviews.Recorded {
		t.Fatalf("expected Recorded, got %v", got)
	}

	if got := applyBatchOne(t, writer, learner, entry); got != reviews.Replay {
		t.Fatalf("expected a replay, got %v", got)
	}
	if review := storedReview(t, queries, learner, cardID); review.Interval != 6 || review.Efactor != 2.36 {
		t.Fatalf("expected the replay to change nothing, got %d/%g", review.Interval, review.Efactor)
	}

	earlier := deviceEntry(cardID, 1, 0, 1.3, 1, first.Add(-time.Minute), first.Add(time.Minute))
	if got := applyBatchOne(t, writer, learner, earlier); got != reviews.Replay {
		t.Fatalf("expected a superseded Grade to be a replay, got %v", got)
	}
	if review := storedReview(t, queries, learner, cardID); review.Interval != 6 || !review.LastReviewedAt.Time.Equal(first) {
		t.Fatalf("expected the later answer to stand, got interval %d answered %v", review.Interval, review.LastReviewedAt)
	}

	later := deviceEntry(cardID, 2, 3, 2.2, 2, first.Add(time.Hour), first.Add(25*time.Hour))
	if got := applyBatchOne(t, writer, learner, later); got != reviews.Recorded {
		t.Fatalf("expected the later Grade to be recorded, got %v", got)
	}
	if review := storedReview(t, queries, learner, cardID); review.Interval != 3 || review.Efactor != 2.2 {
		t.Fatalf("expected the later answer stored, got %d/%g", review.Interval, review.Efactor)
	}
}

// A Grade the backend could not store exactly as given is refused with the
// outcome naming what was wrong.
func TestApplyBatchRefusesInvalidEntries(t *testing.T) {
	valid := func() *types.GradeEntry {
		at := time.Now().Add(-time.Hour).Truncate(time.Second)
		return deviceEntry(1, 3, 6, 2.5, 1, at, at.Add(24*time.Hour))
	}

	tests := []struct {
		name  string
		entry func() *types.GradeEntry
		want  reviews.Outcome
	}{
		{
			name: "no schedule",
			entry: func() *types.GradeEntry {
				entry := valid()
				entry.Schedule = nil
				return entry
			},
			want: reviews.NoSchedule,
		},
		{
			name: "invalid Grade",
			entry: func() *types.GradeEntry {
				entry := valid()
				entry.Grade = 5
				return entry
			},
			want: reviews.InvalidGrade,
		},
		{
			name: "invalid Interval",
			entry: func() *types.GradeEntry {
				entry := valid()
				entry.Schedule.Interval = -1
				return entry
			},
			want: reviews.InvalidInterval,
		},
		{
			name: "invalid Efactor",
			entry: func() *types.GradeEntry {
				entry := valid()
				entry.Schedule.Efactor = 0
				return entry
			},
			want: reviews.InvalidEfactor,
		},
		{
			name: "invalid repetitions",
			entry: func() *types.GradeEntry {
				entry := valid()
				entry.Schedule.Repetitions = -1
				return entry
			},
			want: reviews.InvalidRepetitions,
		},
		{
			name: "invalid answeredAt",
			entry: func() *types.GradeEntry {
				entry := valid()
				entry.AnsweredAt = "not a time"
				return entry
			},
			want: reviews.InvalidAnsweredAt,
		},
		{
			name: "invalid nextReviewAt",
			entry: func() *types.GradeEntry {
				entry := valid()
				entry.Schedule.NextReviewAt = "not a time"
				return entry
			},
			want: reviews.InvalidNextReviewAt,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer, _ := newWriter(t)
			learner := dbtest.SeedUser(t, "learner@example.com")
			cardID := dbtest.SeedCard(t, dbtest.SeedDeck(t, learner, false), "나무", "tree")
			entry := tt.entry()
			entry.CardId = uint32(cardID)

			if got := applyBatchOne(t, writer, learner, entry); got != tt.want {
				t.Fatalf("expected outcome %v, got %v", tt.want, got)
			}
		})
	}
}

// The legacy rating schedules from the stored state, and creates a Review only
// when none is there.
func TestApplyLegacySchedulesFromTheStoredState(t *testing.T) {
	writer, queries := newWriter(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")
	dbtest.SeedScheduledReview(t, learner, cardID, time.Now().Add(24*time.Hour))

	if err := writer.ApplyLegacy(context.Background(), learner, cardID, scheduling.Good); err != nil {
		t.Fatalf("failed to rate the Card: %v", err)
	}

	review := storedReview(t, queries, learner, cardID)
	if review.Interval != 15 || review.Efactor != 2.5 || review.Repetitions != 2 {
		t.Fatalf("expected the stored schedule 15/2.5/2, got %d/%g/%d", review.Interval, review.Efactor, review.Repetitions)
	}
}

// A first Good on a Card with no Review graduates it from the row's defaults.
func TestApplyLegacyCreatesAMissingReview(t *testing.T) {
	writer, queries := newWriter(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")

	if err := writer.ApplyLegacy(context.Background(), learner, cardID, scheduling.Good); err != nil {
		t.Fatalf("failed to rate the Card: %v", err)
	}

	review := storedReview(t, queries, learner, cardID)
	if review.Interval != 6 || review.Efactor != 2.5 || review.Repetitions != 2 {
		t.Fatalf("expected the first graduation 6/2.5/2, got %d/%g/%d", review.Interval, review.Efactor, review.Repetitions)
	}
}

// The legacy path applies the same Deck-access gate: an unreadable Deck is
// refused, and stores no Review.
func TestApplyLegacyRefusesAnUnreadableDeck(t *testing.T) {
	writer, queries := newWriter(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	stranger := dbtest.SeedUser(t, "stranger@example.com")
	deckID := dbtest.SeedDeck(t, stranger, false)
	cardID := dbtest.SeedCard(t, deckID, "남의", "someone else's")

	err := writer.ApplyLegacy(context.Background(), learner, cardID, scheduling.Good)
	if !errors.Is(err, reviews.ErrNoAccess) {
		t.Fatalf("expected ErrNoAccess, got %v", err)
	}

	if _, err := queries.GetUserCard(context.Background(), db.GetUserCardParams{UserID: learner, CardID: cardID}); err == nil {
		t.Fatal("expected no Review for an unreadable Deck")
	}
}

// A Deck that is gone is not readable: the legacy path refuses it too.
func TestApplyLegacyRefusesADeletedDeck(t *testing.T) {
	writer, queries := newWriter(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "지운", "deleted")
	if err := queries.DeleteDeck(context.Background(), deckID); err != nil {
		t.Fatalf("failed to delete the Deck: %v", err)
	}

	err := writer.ApplyLegacy(context.Background(), learner, cardID, scheduling.Good)
	if !errors.Is(err, decks.ErrNoSuchDeck) {
		t.Fatalf("expected decks.ErrNoSuchDeck, got %v", err)
	}
}

// A Card that is not there is refused before any Review is touched.
func TestApplyLegacyRefusesAnUnknownCard(t *testing.T) {
	writer, _ := newWriter(t)
	learner := dbtest.SeedUser(t, "learner@example.com")

	err := writer.ApplyLegacy(context.Background(), learner, 9999, scheduling.Good)
	if !errors.Is(err, reviews.ErrUnknownCard) {
		t.Fatalf("expected ErrUnknownCard, got %v", err)
	}
}

// A public Deck and a Deck shared for viewing are both studiable: the gate
// refuses only a Deck the caller may not read.
func TestApplyLegacyAcceptsReadableDecks(t *testing.T) {
	tests := []struct {
		name      string
		public    bool
		share     bool
		wantAllow bool
	}{
		{name: "public Deck", public: true, wantAllow: true},
		{name: "Deck shared for viewing", share: true, wantAllow: true},
		{name: "someone else's private Deck", wantAllow: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer, _ := newWriter(t)
			owner := dbtest.SeedUser(t, "owner@example.com")
			learner := dbtest.SeedUser(t, "learner@example.com")
			deckID := dbtest.SeedDeck(t, owner, tt.public)
			cardID := dbtest.SeedCard(t, deckID, "카드", "card")
			if tt.share {
				dbtest.SeedShare(t, deckID, learner, access.RoleView)
			}

			err := writer.ApplyLegacy(context.Background(), learner, cardID, scheduling.Good)
			if tt.wantAllow && err != nil {
				t.Fatalf("expected the Grade to be accepted, got %v", err)
			}
			if !tt.wantAllow && !errors.Is(err, reviews.ErrNoAccess) {
				t.Fatalf("expected ErrNoAccess, got %v", err)
			}
		})
	}
}

// Every Grade write asks the same gate: an unreadable Deck is refused by each
// of the three, and none stores a Review.
func TestEveryGradeWriteRefusesAnUnreadableDeck(t *testing.T) {
	answeredAt := time.Now().Add(-time.Hour).Truncate(time.Second)
	nextReviewAt := answeredAt.Add(24 * time.Hour)

	tests := []struct {
		name  string
		write func(t *testing.T, writer *reviews.Writer, caller, cardID int64) error
	}{
		{
			name: "Enroll",
			write: func(_ *testing.T, writer *reviews.Writer, caller, cardID int64) error {
				return writer.Enroll(context.Background(), caller, cardID)
			},
		},
		{
			name: "ApplyBatch",
			write: func(t *testing.T, writer *reviews.Writer, caller, cardID int64) error {
				outcomes := writer.ApplyBatch(context.Background(), caller, []*types.GradeEntry{
					deviceEntry(cardID, 3, 6, 2.5, 1, answeredAt, nextReviewAt),
				})
				if len(outcomes) != 1 || outcomes[0].Outcome != reviews.NoAccess {
					t.Fatalf("expected NoAccess, got %+v", outcomes)
				}
				return nil
			},
		},
		{
			name: "ApplyLegacy",
			write: func(_ *testing.T, writer *reviews.Writer, caller, cardID int64) error {
				return writer.ApplyLegacy(context.Background(), caller, cardID, scheduling.Good)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer, queries := newWriter(t)
			learner := dbtest.SeedUser(t, "learner@example.com")
			stranger := dbtest.SeedUser(t, "stranger@example.com")
			deckID := dbtest.SeedDeck(t, stranger, false)
			cardID := dbtest.SeedCard(t, deckID, "남의", "someone else's")

			if err := tt.write(t, writer, learner, cardID); err != nil && !errors.Is(err, reviews.ErrNoAccess) {
				t.Fatalf("expected ErrNoAccess, got %v", err)
			}

			if _, err := queries.GetUserCard(context.Background(), db.GetUserCardParams{
				UserID: learner, CardID: cardID,
			}); err == nil {
				t.Fatal("expected no Review for an unreadable Deck")
			}
		})
	}
}
