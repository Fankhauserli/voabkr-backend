package handlers_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/jackc/pgx/v5"
)

const legacyReviewRoute = "/api/v1/reviews/:id"

func legacyReviewPath(cardID int64) string {
	return fmt.Sprintf("/api/v1/reviews/%d", cardID)
}

// storedReview returns the learner's Review of cardID, failing the test when
// there is none.
func storedReview(t *testing.T, learner, cardID int64) db.UserCard {
	t.Helper()

	review, err := dbtest.Queries(t).GetUserCard(context.Background(), db.GetUserCardParams{
		UserID: learner,
		CardID: cardID,
	})
	if err != nil {
		t.Fatalf("expected a stored Review for Card %d: %v", cardID, err)
	}
	return review
}

// The legacy route schedules from the Review already stored, and answers with
// the message it has always answered with.
func TestUpdateReviewSchedulesFromTheStoredState(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")
	dbtest.SeedScheduledReview(t, learner, cardID, time.Now().Add(24*time.Hour))

	w := put(t, h.UpdateReview, learner, legacyReviewRoute, legacyReviewPath(cardID), `{"ease":3}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if got, want := w.Body.String(), `{"message":"Review updated"}`; got != want {
		t.Fatalf("expected the legacy response %s, got %s", want, got)
	}

	// Good grows a graduated Interval of 6 by the Efactor 2.5, and counts the
	// answer.
	review := storedReview(t, learner, cardID)
	if review.Interval != 15 || review.Efactor != 2.5 || review.Repetitions != 2 {
		t.Fatalf("expected the stored schedule 15/2.5/2, got %d/%g/%d",
			review.Interval, review.Efactor, review.Repetitions)
	}
	if !review.NextReviewAt.Valid || !review.NextReviewAt.Time.Equal(review.LastReviewedAt.Time.AddDate(0, 0, 15)) {
		t.Fatalf("expected the next Review 15 days after the answer, got %v answered %v",
			review.NextReviewAt, review.LastReviewedAt)
	}
}

// A learner meeting a Card for the first time gets a Review only because none
// was stored: the first Good graduates it to six days from the row's defaults.
func TestUpdateReviewCreatesAMissingReview(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")

	w := put(t, h.UpdateReview, learner, legacyReviewRoute, legacyReviewPath(cardID), `{"ease":3}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	review := storedReview(t, learner, cardID)
	if review.Interval != 6 || review.Efactor != 2.5 || review.Repetitions != 2 {
		t.Fatalf("expected the stored schedule 6/2.5/2, got %d/%g/%d",
			review.Interval, review.Efactor, review.Repetitions)
	}
}

// A Grade is only accepted on a Deck the learner may read: an answer for
// someone else's private Deck is refused, and stores no Review.
func TestUpdateReviewRefusesAnUnreadableDeck(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	stranger := dbtest.SeedUser(t, "stranger@example.com")
	deckID := dbtest.SeedDeck(t, stranger, false)
	cardID := dbtest.SeedCard(t, deckID, "남의", "someone else's")

	w := put(t, h.UpdateReview, learner, legacyReviewRoute, legacyReviewPath(cardID), `{"ease":3}`)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", w.Code, w.Body.String())
	}

	if _, err := dbtest.Queries(t).GetUserCard(context.Background(), db.GetUserCardParams{
		UserID: learner, CardID: cardID,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected no Review to be stored for an unreadable Deck, got %v", err)
	}
}

// A Deck that is gone is unreadable too: the legacy path refuses it with the
// same 403, and stores nothing.
func TestUpdateReviewRefusesADeletedDeck(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "지운", "deleted")
	if err := dbtest.Queries(t).DeleteDeck(context.Background(), deckID); err != nil {
		t.Fatalf("failed to delete the Deck: %v", err)
	}

	w := put(t, h.UpdateReview, learner, legacyReviewRoute, legacyReviewPath(cardID), `{"ease":3}`)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := dbtest.Queries(t).GetUserCard(context.Background(), db.GetUserCardParams{
		UserID: learner, CardID: cardID,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected no Review for a deleted Deck, got %v", err)
	}
}

// A public Deck and a Deck shared for viewing are both studiable: the access
// gate refuses only a Deck the caller may not read.
func TestUpdateReviewAcceptsReadableDecks(t *testing.T) {
	tests := []struct {
		name  string
		grant func(t *testing.T, deckID, learner int64)
	}{
		{
			"public Deck",
			func(t *testing.T, deckID, learner int64) {},
		},
		{
			"Deck shared for viewing",
			func(t *testing.T, deckID, learner int64) {
				dbtest.SeedShare(t, deckID, learner, access.RoleView)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t)
			owner := dbtest.SeedUser(t, "owner@example.com")
			learner := dbtest.SeedUser(t, "learner@example.com")
			deckID := dbtest.SeedDeck(t, owner, true)
			cardID := dbtest.SeedCard(t, deckID, "공개", "public")
			tt.grant(t, deckID, learner)

			w := put(t, h.UpdateReview, learner, legacyReviewRoute, legacyReviewPath(cardID), `{"ease":3}`)

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
			}
			storedReview(t, learner, cardID)
		})
	}
}

// A Grade outside the four the scheduler knows is refused, rather than falling
// through to a default schedule and being stored as an answer.
func TestUpdateReviewRefusesAGradeOutsideTheFour(t *testing.T) {
	for _, ease := range []int{-1, 0, 5} {
		t.Run(fmt.Sprintf("ease %d", ease), func(t *testing.T) {
			h := newTestHandler(t)
			learner := dbtest.SeedUser(t, "learner@example.com")
			deckID := dbtest.SeedDeck(t, learner, false)
			cardID := dbtest.SeedCard(t, deckID, "나무", "tree")

			w := put(t, h.UpdateReview, learner, legacyReviewRoute, legacyReviewPath(cardID),
				fmt.Sprintf(`{"ease":%d}`, ease))

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
			}

			if _, err := dbtest.Queries(t).GetUserCard(context.Background(), db.GetUserCardParams{
				UserID: learner, CardID: cardID,
			}); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("expected no Review to be stored for an invalid Grade, got %v", err)
			}
		})
	}
}

// A Card that is not there is read as gone, as every Card read path reads it.
func TestUpdateReviewAnswersNotFoundForAnUnknownCard(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")

	w := put(t, h.UpdateReview, learner, legacyReviewRoute, legacyReviewPath(9999), `{"ease":3}`)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}
