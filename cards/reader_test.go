package cards_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/cards"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"google.golang.org/protobuf/proto"
)

// TestMain provisions this package's Postgres with the migrations applied once,
// per ADR-0003. The harness records a missing container runtime and skips the
// database-backed tests locally; CI fails instead.
func TestMain(m *testing.M) {
	dbtest.Main(m)
}

// A Deck's Cards stop answering at the module's own interface once the Deck is
// deleted, not only through the handler that gates the route.
func TestByDeckOmitsTheCardsOfADeletedDeck(t *testing.T) {
	queries := dbtest.Setup(t)
	reader := cards.NewReader(queries, nil)
	ctx := context.Background()

	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	dbtest.SeedCard(t, deckID, "지운", "deleted")

	got, err := reader.ByDeck(ctx, owner, deckID, cards.Page{})
	if err != nil {
		t.Fatalf("failed to read the Deck's Cards: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected the Deck's Card to begin with, got %d", len(got))
	}

	if err := queries.DeleteDeck(ctx, deckID); err != nil {
		t.Fatalf("failed to delete the Deck: %v", err)
	}

	got, err = reader.ByDeck(ctx, owner, deckID, cards.Page{})
	if err != nil {
		t.Fatalf("failed to read the deleted Deck's Cards: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected the deleted Deck's Cards to leave the listing, got %d", len(got))
	}
}

// A Deck's Cards are read for the caller asking: each carries that caller's own
// Review schedule, and a Card they have not studied carries none.
func TestByDeckCarriesTheCallersSchedules(t *testing.T) {
	queries := dbtest.Setup(t)
	reader := cards.NewReader(queries, nil)
	ctx := context.Background()

	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	studied := dbtest.SeedCard(t, deckID, "배운", "studied")
	unstudied := dbtest.SeedCard(t, deckID, "안 배운", "unstudied")

	nextReviewAt := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	dbtest.SeedScheduledReview(t, learner, studied, nextReviewAt)

	got, err := reader.ByDeck(ctx, learner, deckID, cards.Page{})
	if err != nil {
		t.Fatalf("failed to read the Deck's Cards: %v", err)
	}

	schedules := schedulesByCard(got)
	if schedule := schedules[studied]; schedule == nil {
		t.Fatal("expected the studied Card to carry the learner's Review schedule")
	} else if schedule.NextReviewAt != nextReviewAt.UTC().Format(time.RFC3339) {
		t.Fatalf("expected nextReviewAt %s, got %q", nextReviewAt.UTC().Format(time.RFC3339), schedule.NextReviewAt)
	}
	if schedule := schedules[unstudied]; schedule != nil {
		t.Fatalf("expected the unstudied Card to carry no Review schedule, got %+v", schedule)
	}
}

// The entry a Deck's Cards are held in carries the Deck's contents alone: the
// caller's Review schedule is attached to the read, not written with the Cards.
func TestByDeckStoresTheCardsWithoutTheCallersSchedule(t *testing.T) {
	queries := dbtest.Setup(t)
	stored := newStoredCache()
	reader := cards.NewReader(queries, stored)
	ctx := context.Background()

	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "배운", "studied")
	dbtest.SeedScheduledReview(t, learner, cardID, time.Now().Add(24*time.Hour))

	got, err := reader.ByDeck(ctx, learner, deckID, cards.Page{})
	if err != nil {
		t.Fatalf("failed to read the Deck's Cards: %v", err)
	}
	if len(got) != 1 || got[0].ReviewSchedule == nil {
		t.Fatalf("expected the read to carry the learner's Review schedule, got %+v", got)
	}

	entry := stored.onlyEntry(t)
	if len(entry) != 1 {
		t.Fatalf("expected the entry to hold the Deck's one Card, got %d", len(entry))
	}
	if entry[0].ReviewSchedule != nil {
		t.Fatalf("expected the entry to hold no Review schedule, got %+v", entry[0].ReviewSchedule)
	}
}

// A warm entry is the Deck's contents, not a learner's view of it: the schedule
// a read carries is the asking caller's, never the one the entry was warmed by.
func TestByDeckServesAWarmEntryWithTheAskingCallersSchedule(t *testing.T) {
	queries := dbtest.Setup(t)
	stored := newStoredCache()
	reader := cards.NewReader(queries, stored)
	ctx := context.Background()

	owner := dbtest.SeedUser(t, "owner@example.com")
	other := dbtest.SeedUser(t, "other@example.com")
	deckID := dbtest.SeedDeck(t, owner, true)
	cardID := dbtest.SeedCard(t, deckID, "공개", "public")

	ownerReviewAt := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	otherReviewAt := time.Now().Add(72 * time.Hour).Truncate(time.Second)
	dbtest.SeedScheduledReview(t, owner, cardID, ownerReviewAt)
	dbtest.SeedScheduledReview(t, other, cardID, otherReviewAt)

	if _, err := reader.ByDeck(ctx, owner, deckID, cards.Page{}); err != nil {
		t.Fatalf("failed to warm the entry: %v", err)
	}

	got, err := reader.ByDeck(ctx, other, deckID, cards.Page{})
	if err != nil {
		t.Fatalf("failed to read the warm entry: %v", err)
	}
	if len(got) != 1 || got[0].ReviewSchedule == nil {
		t.Fatalf("expected the other learner's Review schedule, got %+v", got)
	}
	if want := otherReviewAt.UTC().Format(time.RFC3339); got[0].ReviewSchedule.NextReviewAt != want {
		t.Fatalf("expected the asking learner's nextReviewAt %s, got %q", want, got[0].ReviewSchedule.NextReviewAt)
	}
}

// A guest reading a public Deck carries no Review schedule, even when another
// learner has studied its Cards and the entry is warm.
func TestByDeckGivesAGuestNoSchedules(t *testing.T) {
	queries := dbtest.Setup(t)
	reader := cards.NewReader(queries, newStoredCache())
	ctx := context.Background()

	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, true)
	cardID := dbtest.SeedCard(t, deckID, "공개", "public")
	dbtest.SeedScheduledReview(t, owner, cardID, time.Now().Add(24*time.Hour))

	if _, err := reader.ByDeck(ctx, owner, deckID, cards.Page{}); err != nil {
		t.Fatalf("failed to warm the entry: %v", err)
	}

	got, err := reader.ByDeck(ctx, access.Anonymous, deckID, cards.Page{})
	if err != nil {
		t.Fatalf("failed to read the Deck's Cards as a guest: %v", err)
	}
	if schedule := schedulesByCard(got)[cardID]; schedule != nil {
		t.Fatalf("expected a guest to carry no Review schedule, got %+v", schedule)
	}
}

// The single read carries the caller's own Review schedule of the Card, and a
// guest carries none.
func TestByIDCarriesTheCallersSchedule(t *testing.T) {
	queries := dbtest.Setup(t)
	reader := cards.NewReader(queries, nil)
	ctx := context.Background()

	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, true)
	cardID := dbtest.SeedCard(t, deckID, "배운", "studied")

	nextReviewAt := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	dbtest.SeedScheduledReview(t, owner, cardID, nextReviewAt)

	got, err := reader.ByID(ctx, owner, cardID)
	if err != nil {
		t.Fatalf("failed to read the Card: %v", err)
	}
	if got.Body.ReviewSchedule == nil {
		t.Fatal("expected the caller's Review schedule")
	}
	if want := nextReviewAt.UTC().Format(time.RFC3339); got.Body.ReviewSchedule.NextReviewAt != want {
		t.Fatalf("expected nextReviewAt %s, got %q", want, got.Body.ReviewSchedule.NextReviewAt)
	}

	guest, err := reader.ByID(ctx, access.Anonymous, cardID)
	if err != nil {
		t.Fatalf("failed to read the Card as a guest: %v", err)
	}
	if guest.Body.ReviewSchedule != nil {
		t.Fatalf("expected a guest to carry no Review schedule, got %+v", guest.Body.ReviewSchedule)
	}
}

// The cross-Deck listing carries the caller's own schedules too.
func TestForCallerCarriesTheCallersSchedules(t *testing.T) {
	queries := dbtest.Setup(t)
	reader := cards.NewReader(queries, nil)
	ctx := context.Background()

	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	studied := dbtest.SeedCard(t, deckID, "배운", "studied")
	unstudied := dbtest.SeedCard(t, deckID, "안 배운", "unstudied")
	dbtest.SeedScheduledReview(t, learner, studied, time.Now().Add(24*time.Hour))

	got, err := reader.ForCaller(ctx, learner, cards.Page{})
	if err != nil {
		t.Fatalf("failed to read the caller's Cards: %v", err)
	}

	schedules := schedulesByCard(got)
	if schedule := schedules[studied]; schedule == nil {
		t.Fatal("expected the studied Card to carry the learner's Review schedule")
	}
	if schedule := schedules[unstudied]; schedule != nil {
		t.Fatalf("expected the unstudied Card to carry no Review schedule, got %+v", schedule)
	}
}

// Rows a caller's own query read are rendered with the caller's schedules, so
// the Review queue does not have to attach them itself.
func TestFromRowsCarriesTheCallersSchedules(t *testing.T) {
	queries := dbtest.Setup(t)
	reader := cards.NewReader(queries, nil)
	ctx := context.Background()

	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "배운", "studied")
	dbtest.SeedScheduledReview(t, learner, cardID, time.Now().Add(24*time.Hour))

	row, err := queries.GetCard(ctx, cardID)
	if err != nil {
		t.Fatalf("failed to read the Card's row: %v", err)
	}

	got, err := reader.FromRows(ctx, learner, []db.Card{row})
	if err != nil {
		t.Fatalf("failed to render the rows: %v", err)
	}
	if len(got) != 1 || got[0].ReviewSchedule == nil {
		t.Fatalf("expected the row to carry the learner's Review schedule, got %+v", got)
	}

	guest, err := reader.FromRows(ctx, access.Anonymous, []db.Card{row})
	if err != nil {
		t.Fatalf("failed to render the rows for a guest: %v", err)
	}
	if len(guest) != 1 || guest[0].ReviewSchedule != nil {
		t.Fatalf("expected a guest's row to carry no Review schedule, got %+v", guest)
	}
}

// schedulesByCard indexes a listing's Review schedules by Card id.
func schedulesByCard(cards []*types.Card) map[int64]*types.ReviewSchedule {
	schedules := make(map[int64]*types.ReviewSchedule, len(cards))
	for _, card := range cards {
		schedules[int64(card.Id)] = card.ReviewSchedule
	}
	return schedules
}

// storedCache is an in-memory stand-in for the cache boundary. It serialises
// what a reader stores, as the real cache does, so a test can read an entry as
// it was written rather than the pointers the reader returned.
type storedCache struct {
	entries map[string][]byte
}

func newStoredCache() *storedCache {
	return &storedCache{entries: make(map[string][]byte)}
}

func (c *storedCache) GetProto(_ context.Context, key string, msg proto.Message) (bool, error) {
	data, found := c.entries[key]
	if !found {
		return false, nil
	}
	return true, proto.Unmarshal(data, msg)
}

func (c *storedCache) SetProto(_ context.Context, key string, msg proto.Message, _ time.Duration) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	c.entries[key] = data
	return nil
}

func (c *storedCache) Delete(_ context.Context, keys ...string) error {
	for _, key := range keys {
		delete(c.entries, key)
	}
	return nil
}

func (c *storedCache) DeletePrefix(_ context.Context, prefix string) error {
	for key := range c.entries {
		if strings.HasPrefix(key, prefix) {
			delete(c.entries, key)
		}
	}
	return nil
}

func (c *storedCache) Close() {}

// onlyEntry returns the one entry c holds, as the Cards that were written to
// it. A test that expects a particular number of entries says so itself.
func (c *storedCache) onlyEntry(t *testing.T) []*types.Card {
	t.Helper()

	if len(c.entries) != 1 {
		t.Fatalf("expected one cache entry, got %d", len(c.entries))
	}
	for _, data := range c.entries {
		var list types.CardList
		if err := proto.Unmarshal(data, &list); err != nil {
			t.Fatalf("failed to read the stored entry: %v", err)
		}
		return list.Cards
	}
	return nil
}
