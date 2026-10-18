package reviews_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/cards"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/reviews"
	"github.com/Fankhauserli/voabkr-backend/types"
)

// TestMain provisions this package's Postgres with the migrations applied once,
// per ADR-0003. The harness records a missing container runtime and skips the
// database-backed tests locally; CI fails instead.
func TestMain(m *testing.M) {
	dbtest.Main(m)
}

// newReader returns a Review reader over the test database, with Card reads
// answering for the caller asking.
func newReader(t *testing.T) *reviews.Reader {
	t.Helper()

	queries := dbtest.Setup(t)
	return reviews.NewReader(queries, cards.NewReader(queries, nil))
}

// queue reads the caller's queue and returns the Korean words it carries.
func queue(t *testing.T, reader *reviews.Reader, query reviews.Query) []string {
	t.Helper()

	got, err := reader.Queue(context.Background(), query)
	if err != nil {
		t.Fatalf("failed to read the queue: %v", err)
	}
	return wordsOf(got)
}

// The queue is the stored schedules' due at or before now: a Card due in the
// future is not offered, and a Card with no Review is not introduced.
func TestQueueReturnsTheCardsDueNow(t *testing.T) {
	reader := newReader(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)

	due := dbtest.SeedCard(t, deckID, "지금", "due now")
	dbtest.SeedDueReview(t, learner, due)
	dbtest.SeedCard(t, deckID, "나중", "later")
	notDue := dbtest.SeedCard(t, deckID, "새것", "new")
	dbtest.SeedScheduledReview(t, learner, notDue, time.Now().Add(24*time.Hour))

	if got := queue(t, reader, reviews.Query{Caller: learner}); !slices.Equal(got, []string{"지금"}) {
		t.Fatalf("expected only the due Card, got %v", got)
	}
}

// A Deck filter is the caller's ask: only the named Deck's due Cards answer,
// even when other readable Decks have due Cards of their own.
func TestQueueAppliesTheDeckFilter(t *testing.T) {
	reader := newReader(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	filteredDeck := dbtest.SeedDeck(t, learner, false)
	otherDeck := dbtest.SeedDeck(t, learner, false)

	filteredDue := dbtest.SeedCard(t, filteredDeck, "고른", "filtered")
	dbtest.SeedDueReview(t, learner, filteredDue)
	otherDue := dbtest.SeedCard(t, otherDeck, "다른", "other")
	dbtest.SeedDueReview(t, learner, otherDue)

	if got := queue(t, reader, reviews.Query{Caller: learner, DeckID: filteredDeck}); !slices.Equal(got, []string{"고른"}) {
		t.Fatalf("expected only the named Deck's Card, got %v", got)
	}
}

// A filter naming a Deck the caller may not read answers nothing, even when
// their Review of one of its Cards has come round.
func TestQueueFilteredToAnUnreadableDeckIsEmpty(t *testing.T) {
	reader := newReader(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	stranger := dbtest.SeedUser(t, "stranger@example.com")
	strangerDeck := dbtest.SeedDeck(t, stranger, false)
	cardID := dbtest.SeedCard(t, strangerDeck, "남의", "someone else's")

	// The learner studied it while they had access, so the Review has come round.
	dbtest.SeedDueReview(t, learner, cardID)

	if got := queue(t, reader, reviews.Query{Caller: learner, DeckID: strangerDeck}); len(got) != 0 {
		t.Fatalf("expected no Cards from an unreadable Deck, got %v", got)
	}
}

// The queue excludes Cards of Decks the caller may not read and of Decks that
// are gone, while still serving their own and the public ones.
func TestQueueExcludesUnreadableAndDeletedDecks(t *testing.T) {
	reader := newReader(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	stranger := dbtest.SeedUser(t, "stranger@example.com")

	ownDeck := dbtest.SeedDeck(t, learner, false)
	ownDue := dbtest.SeedCard(t, ownDeck, "내것", "mine")
	dbtest.SeedDueReview(t, learner, ownDue)

	publicDeck := dbtest.SeedDeck(t, stranger, true)
	publicDue := dbtest.SeedCard(t, publicDeck, "공개", "public")
	dbtest.SeedDueReview(t, learner, publicDue)

	privateDeck := dbtest.SeedDeck(t, stranger, false)
	privateDue := dbtest.SeedCard(t, privateDeck, "비공개", "private")
	dbtest.SeedDueReview(t, learner, privateDue)

	deletedDeck := dbtest.SeedDeck(t, learner, true)
	deletedDue := dbtest.SeedCard(t, deletedDeck, "지운", "deleted")
	dbtest.SeedDueReview(t, learner, deletedDue)
	if err := dbtest.Queries(t).DeleteDeck(context.Background(), deletedDeck); err != nil {
		t.Fatalf("failed to delete the Deck: %v", err)
	}

	got := queue(t, reader, reviews.Query{Caller: learner})
	slices.Sort(got)
	if !slices.Equal(got, []string{"공개", "내것"}) {
		t.Fatalf("expected only the readable, live Decks' Cards, got %v", got)
	}
}

// The since read takes Cards due after that moment, and keeps the Deck filter.
func TestQueueSinceReturnsCardsDueAfterTheMoment(t *testing.T) {
	reader := newReader(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)

	early := dbtest.SeedCard(t, deckID, "이른", "early")
	dbtest.SeedScheduledReview(t, learner, early, time.Now().Add(-3*time.Hour))
	late := dbtest.SeedCard(t, deckID, "늦은", "late")
	dbtest.SeedScheduledReview(t, learner, late, time.Now().Add(-time.Hour))

	query := reviews.Query{Caller: learner, Since: time.Now().Add(-2 * time.Hour)}
	if got := queue(t, reader, query); !slices.Equal(got, []string{"늦은"}) {
		t.Fatalf("expected only the Card due after the moment, got %v", got)
	}
}

// A since read still adds the Deck filter, so one Deck's history is not another
// Deck's answer.
func TestQueueSinceAppliesTheDeckFilter(t *testing.T) {
	reader := newReader(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	filteredDeck := dbtest.SeedDeck(t, learner, false)
	otherDeck := dbtest.SeedDeck(t, learner, false)

	filteredDue := dbtest.SeedCard(t, filteredDeck, "고른", "filtered")
	dbtest.SeedDueReview(t, learner, filteredDue)
	otherDue := dbtest.SeedCard(t, otherDeck, "다른", "other")
	dbtest.SeedDueReview(t, learner, otherDue)

	query := reviews.Query{Caller: learner, DeckID: filteredDeck, Since: time.Now().Add(-2 * time.Hour)}
	if got := queue(t, reader, query); !slices.Equal(got, []string{"고른"}) {
		t.Fatalf("expected only the named Deck's Card, got %v", got)
	}
}

// A queue Card carries the caller's own Review schedule of it: the schedule is
// what the device computes its next answer from.
func TestQueueAttachesTheCallersSchedules(t *testing.T) {
	reader := newReader(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "배운", "studied")

	nextReviewAt := time.Now().Add(-time.Hour).Truncate(time.Second)
	dbtest.SeedScheduledReview(t, learner, cardID, nextReviewAt)

	got, err := reader.Queue(context.Background(), reviews.Query{Caller: learner})
	if err != nil {
		t.Fatalf("failed to read the queue: %v", err)
	}
	if len(got) != 1 || got[0].ReviewSchedule == nil {
		t.Fatalf("expected the Card with its Review schedule, got %+v", got)
	}
	if want := nextReviewAt.UTC().Format(time.RFC3339); got[0].ReviewSchedule.NextReviewAt != want {
		t.Fatalf("expected nextReviewAt %s, got %q", want, got[0].ReviewSchedule.NextReviewAt)
	}
}

// A guest has no queue: the read belongs to a learner whose Reviews are read.
// The handler refuses one before asking; the module reads none for that caller.
func TestQueueForAGuestIsEmpty(t *testing.T) {
	reader := newReader(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, true)
	cardID := dbtest.SeedCard(t, deckID, "공개", "public")
	dbtest.SeedDueReview(t, owner, cardID)

	if got := queue(t, reader, reviews.Query{Caller: access.Anonymous}); len(got) != 0 {
		t.Fatalf("expected a guest's queue to be empty, got %v", got)
	}
}

// wordsOf returns the Korean words a queue carries.
func wordsOf(cards []*types.Card) []string {
	words := make([]string, 0, len(cards))
	for _, card := range cards {
		words = append(words, card.KoreanWord)
	}
	return words
}
