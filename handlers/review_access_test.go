package handlers_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
)

const reviewsRoute = "/api/v1/reviews/"

func reviewsPath(deckID int64) string {
	return fmt.Sprintf("/api/v1/reviews/?deck_id=%d", deckID)
}

// A study session filtered to a private deck is refused for a learner with no
// access to it, even though the queue is theirs.
func TestGetReviewsForPrivateDeckForbiddenForOutsider(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	outsider := dbtest.SeedUser(t, "outsider@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	dbtest.SeedCard(t, deckID, "비공개", "private")

	w := get(t, h.GetReviews, outsider, reviewsRoute, reviewsPath(deckID))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for an outsider, got %d: %s", w.Code, w.Body.String())
	}
}

// The owner's own session on the private deck still serves its Cards.
func TestGetReviewsForPrivateDeckReadableByOwner(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	cardID := dbtest.SeedCard(t, deckID, "비공개", "private")
	dbtest.SeedDueReview(t, owner, cardID)

	w := get(t, h.GetReviews, owner, reviewsRoute, reviewsPath(deckID))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for the owner, got %d: %s", w.Code, w.Body.String())
	}
	if got := cardWords(t, w.Body.Bytes()); !slices.Equal(got, []string{"비공개"}) {
		t.Fatalf("expected the private deck's own card, got %v", got)
	}
}

// One learner's queue carries the Cards due in every Deck they may read — and no
// others.
func TestGetReviewsQueueSkipsUnreadableDecks(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	stranger := dbtest.SeedUser(t, "stranger@example.com")

	publicDeck := dbtest.SeedDeck(t, stranger, true)
	publicCard := dbtest.SeedCard(t, publicDeck, "공개", "public")
	learnerDeck := dbtest.SeedDeck(t, learner, false)
	learnerCard := dbtest.SeedCard(t, learnerDeck, "내것", "mine")
	strangerDeck := dbtest.SeedDeck(t, stranger, false)
	strangerCard := dbtest.SeedCard(t, strangerDeck, "남의", "someone else's")

	dbtest.SeedDueReview(t, learner, publicCard)
	dbtest.SeedDueReview(t, learner, learnerCard)
	dbtest.SeedDueReview(t, learner, strangerCard)

	w := get(t, h.GetReviews, learner, reviewsRoute, "/api/v1/reviews/")

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// The queue's order is the device's to decide, so compare it as a set.
	got := cardWords(t, w.Body.Bytes())
	slices.Sort(got)
	if !slices.Equal(got, []string{"공개", "내것"}) {
		t.Fatalf("expected only the learner's own and the public Deck's Cards, got %v", got)
	}
}

// A Card whose Deck the learner may no longer read leaves their queue, even
// though the Review of it is theirs.
func TestGetReviewsQueueDropsCardsOfUnreadableDecks(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	stranger := dbtest.SeedUser(t, "stranger@example.com")
	strangerDeck := dbtest.SeedDeck(t, stranger, false)
	cardID := dbtest.SeedCard(t, strangerDeck, "남의", "someone else's")

	// The learner studied it while they had access, so the Review has come round.
	dbtest.SeedDueReview(t, learner, cardID)

	w := get(t, h.GetReviews, learner, reviewsRoute, "/api/v1/reviews/")

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := cardWords(t, w.Body.Bytes()); len(got) != 0 {
		t.Fatalf("expected the private deck's card to stay out of the queue, got %v", got)
	}
}

// Enrolling a Card applies the same Deck-access gate as every Grade write: an
// unreadable Deck is refused, and stores no Review.
func TestCreateReviewForPrivateDeckForbiddenForOutsider(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	outsider := dbtest.SeedUser(t, "outsider@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	cardID := dbtest.SeedCard(t, deckID, "비공개", "private")

	w := post(t, h.CreateReview, outsider, reviewsRoute, reviewsRoute, fmt.Sprintf(`{"cardID":%d}`, cardID))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for an outsider, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := dbtest.Queries(t).GetUserCard(context.Background(), db.GetUserCardParams{
		UserID: outsider, CardID: cardID,
	}); err == nil {
		t.Fatal("expected no Review for an unreadable Deck")
	}
}

// Enrolling a Card on a Deck the learner may read stores the Review.
func TestCreateReviewStoresAReviewForAReadableDeck(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")

	w := post(t, h.CreateReview, learner, reviewsRoute, reviewsRoute, fmt.Sprintf(`{"cardID":%d}`, cardID))

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}
	storedReview(t, learner, cardID)
}

// A study session filtered to one Deck offers only that Deck's Cards, even when
// other Decks have due Cards of their own.
func TestGetReviewsForOneDeckListsOnlyThatDecksCards(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	filteredDeck := dbtest.SeedDeck(t, learner, false)
	otherDeck := dbtest.SeedDeck(t, learner, false)

	filteredDue := dbtest.SeedCard(t, filteredDeck, "고른", "filtered")
	dbtest.SeedDueReview(t, learner, filteredDue)
	otherDue := dbtest.SeedCard(t, otherDeck, "다른", "other")
	dbtest.SeedDueReview(t, learner, otherDue)

	w := get(t, h.GetReviews, learner, reviewsRoute, reviewsPath(filteredDeck))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := cardWords(t, w.Body.Bytes()); !slices.Equal(got, []string{"고른"}) {
		t.Fatalf("expected only the named Deck's Card, got %v", got)
	}
}

// The queue keeps every spelling of the Deck filter the wire has carried.
func TestGetReviewsKeepsTheDeckFilterAliases(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "고른", "filtered")
	dbtest.SeedDueReview(t, learner, cardID)

	for _, alias := range []string{"deck_id", "deckId", "deck"} {
		t.Run(alias, func(t *testing.T) {
			path := fmt.Sprintf("/api/v1/reviews/?%s=%d", alias, deckID)
			w := get(t, h.GetReviews, learner, reviewsRoute, path)

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
			}
			if got := cardWords(t, w.Body.Bytes()); !slices.Equal(got, []string{"고른"}) {
				t.Fatalf("expected the named Deck's Card, got %v", got)
			}
		})
	}
}

// The since-time listing carries the same deck filter, and the same refusal.
func TestGetReviewsSinceForPrivateDeckForbiddenForOutsider(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	outsider := dbtest.SeedUser(t, "outsider@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	dbtest.SeedCard(t, deckID, "비공개", "private")

	path := fmt.Sprintf("/api/v1/reviews/since/2026-01-01T00:00:00Z?deck_id=%d", deckID)
	w := get(t, h.GetReviewsSince, outsider, "/api/v1/reviews/since/:time", path)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for an outsider, got %d: %s", w.Code, w.Body.String())
	}
}

// An unauthorized request without a session is answered the same way it always
// was, whether or not a deck_id is present.
func TestGetReviewsWithoutSessionUnauthorized(t *testing.T) {
	h := newTestHandler(t)

	w := get(t, h.GetReviews, access.Anonymous, reviewsRoute, "/api/v1/reviews/")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d: %s", w.Code, w.Body.String())
	}
}
