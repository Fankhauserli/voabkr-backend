package handlers_test

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/access"
)

const reviewsRoute = "/api/v1/reviews/"

func reviewsPath(deckID int64) string {
	return fmt.Sprintf("/api/v1/reviews/?deck_id=%d", deckID)
}

// A study session filtered to a private deck is refused for a learner with no
// access to it, even though the queue is theirs.
func TestGetReviewsForPrivateDeckForbiddenForOutsider(t *testing.T) {
	h := newTestHandler(t)
	owner := seedUser(t, "owner@example.com")
	outsider := seedUser(t, "outsider@example.com")
	deckID := seedDeck(t, owner, false)
	seedCard(t, deckID, "비공개", "private")

	w := get(t, h.GetReviews, outsider, reviewsRoute, reviewsPath(deckID))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for an outsider, got %d: %s", w.Code, w.Body.String())
	}
}

// The owner's own session on the private deck still serves its Cards.
func TestGetReviewsForPrivateDeckReadableByOwner(t *testing.T) {
	h := newTestHandler(t)
	owner := seedUser(t, "owner@example.com")
	deckID := seedDeck(t, owner, false)
	cardID := seedCard(t, deckID, "비공개", "private")
	seedDueReview(t, owner, cardID)

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
	learner := seedUser(t, "learner@example.com")
	stranger := seedUser(t, "stranger@example.com")

	publicDeck := seedDeck(t, stranger, true)
	publicCard := seedCard(t, publicDeck, "공개", "public")
	learnerDeck := seedDeck(t, learner, false)
	learnerCard := seedCard(t, learnerDeck, "내것", "mine")
	strangerDeck := seedDeck(t, stranger, false)
	strangerCard := seedCard(t, strangerDeck, "남의", "someone else's")

	seedDueReview(t, learner, publicCard)
	seedDueReview(t, learner, learnerCard)
	seedDueReview(t, learner, strangerCard)

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
	learner := seedUser(t, "learner@example.com")
	stranger := seedUser(t, "stranger@example.com")
	strangerDeck := seedDeck(t, stranger, false)
	cardID := seedCard(t, strangerDeck, "남의", "someone else's")

	// The learner studied it while they had access, so the Review has come round.
	seedDueReview(t, learner, cardID)

	w := get(t, h.GetReviews, learner, reviewsRoute, "/api/v1/reviews/")

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := cardWords(t, w.Body.Bytes()); len(got) != 0 {
		t.Fatalf("expected the private deck's card to stay out of the queue, got %v", got)
	}
}

// The since-time listing carries the same deck filter, and the same refusal.
func TestGetReviewsSinceForPrivateDeckForbiddenForOutsider(t *testing.T) {
	h := newTestHandler(t)
	owner := seedUser(t, "owner@example.com")
	outsider := seedUser(t, "outsider@example.com")
	deckID := seedDeck(t, owner, false)
	seedCard(t, deckID, "비공개", "private")

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
