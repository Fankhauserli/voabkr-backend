package decks_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/cache"
	"github.com/Fankhauserli/voabkr-backend/decks"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
)

// TestMain provisions this package's Postgres with the migrations applied once,
// per ADR-0003. The harness records a missing container runtime and skips the
// database-backed tests locally; CI fails instead.
func TestMain(m *testing.M) {
	dbtest.Main(m)
}

// newReaderWithCache returns a Reader over the test database and a local-only cache, so a
// test can watch what an entry answers without a Valkey cluster.
func newReaderWithCache(t *testing.T, queries *db.Queries) (*decks.Reader, cache.Cache) {
	t.Helper()

	c, err := cache.NewTwoLevelCache(cache.Config{EnableLocalCache: true})
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	t.Cleanup(c.Close)

	return decks.NewReader(queries, c), c
}

// publicNames returns the names Public answers with.
func publicNames(t *testing.T, reader *decks.Reader) []string {
	t.Helper()

	listed, err := reader.Public(context.Background())
	if err != nil {
		t.Fatalf("failed to read the public listing: %v", err)
	}
	return deckNames(listed)
}

// deckNames returns the names a listing carries.
func deckNames(decks []*types.Deck) []string {
	names := make([]string, 0, len(decks))
	for _, deck := range decks {
		names = append(names, deck.Name)
	}
	return names
}

// renameDeck renames the Deck through the database, leaving the cache alone —
// a write the Reader did not announce.
func renameDeck(t *testing.T, queries *db.Queries, deckID int64, name string) {
	t.Helper()

	deck, err := queries.GetDeck(context.Background(), deckID)
	if err != nil {
		t.Fatalf("failed to read the Deck to rename: %v", err)
	}
	if err := queries.UpdateDeck(context.Background(), db.UpdateDeckParams{
		ID:       deck.ID,
		Name:     name,
		Type:     deck.Type,
		IsPublic: deck.IsPublic,
	}); err != nil {
		t.Fatalf("failed to rename the Deck: %v", err)
	}
}

// shareToken returns the token the Deck's share link is keyed by.
func shareToken(t *testing.T, queries *db.Queries, deckID int64) string {
	t.Helper()

	deck, err := queries.GetDeck(context.Background(), deckID)
	if err != nil {
		t.Fatalf("failed to read the Deck's share token: %v", err)
	}
	return deck.ShareToken.String
}

// A public listing is answered from the entry the first read wrote: a rename
// that no write through the Reader announced is not served until the entry is
// cleared, which is what makes the cache a cache.
func TestPublicServesAWarmEntry(t *testing.T) {
	queries := dbtest.Setup(t)
	reader, _ := newReaderWithCache(t, queries)

	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, true)

	if got := publicNames(t, reader); len(got) != 1 || got[0] != "Seeded Deck" {
		t.Fatalf("expected the seeded Deck in the listing, got %v", got)
	}

	renameDeck(t, queries, deckID, "Renamed")

	if got := publicNames(t, reader); len(got) != 1 || got[0] != "Seeded Deck" {
		t.Fatalf("expected the warm entry to answer, got %v", got)
	}
}

// Invalidate clears the public listing: the renamed Deck answers on the next
// read rather than at the entry's expiry.
func TestInvalidateClearsThePublicListing(t *testing.T) {
	queries := dbtest.Setup(t)
	reader, _ := newReaderWithCache(t, queries)
	ctx := context.Background()

	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, true)

	if got := publicNames(t, reader); len(got) != 1 || got[0] != "Seeded Deck" {
		t.Fatalf("expected the seeded Deck in the listing, got %v", got)
	}

	renameDeck(t, queries, deckID, "Renamed")
	reader.Invalidate(ctx, deckID)

	if got := publicNames(t, reader); len(got) != 1 || got[0] != "Renamed" {
		t.Fatalf("expected the renamed Deck after invalidation, got %v", got)
	}
}

// An erasure hands Decks to no one, so every entry holding a Deck goes: the
// public listing answers from the rows again on the next read.
func TestInvalidateEveryDeckClearsThePublicListing(t *testing.T) {
	queries := dbtest.Setup(t)
	reader, _ := newReaderWithCache(t, queries)
	ctx := context.Background()

	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, true)

	if got := publicNames(t, reader); len(got) != 1 || got[0] != "Seeded Deck" {
		t.Fatalf("expected the seeded Deck in the listing, got %v", got)
	}

	renameDeck(t, queries, deckID, "Renamed")
	reader.InvalidateEveryDeck(ctx)

	if got := publicNames(t, reader); len(got) != 1 || got[0] != "Renamed" {
		t.Fatalf("expected the renamed Deck after invalidation, got %v", got)
	}
}

// A share link is answered from the entry the first read wrote, and the entry
// says so: FromEntry distinguishes it from a row read that also names an owner.
func TestSharedServesAWarmEntry(t *testing.T) {
	queries := dbtest.Setup(t)
	reader, _ := newReaderWithCache(t, queries)
	ctx := context.Background()

	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	token := shareToken(t, queries, deckID)

	first, err := reader.Shared(ctx, token)
	if err != nil {
		t.Fatalf("failed to read the shared Deck: %v", err)
	}
	if first.FromEntry {
		t.Fatal("expected the first read to come from the row")
	}

	renameDeck(t, queries, deckID, "Renamed")

	second, err := reader.Shared(ctx, token)
	if err != nil {
		t.Fatalf("failed to read the warm entry: %v", err)
	}
	if !second.FromEntry {
		t.Fatal("expected the second read to come from the entry")
	}
	if second.Deck.Name != "Seeded Deck" {
		t.Fatalf("expected the warm entry's name, got %q", second.Deck.Name)
	}
}

// Invalidate clears the share link too: the link serves the Deck's current row
// on the next read.
func TestInvalidateClearsTheShareLink(t *testing.T) {
	queries := dbtest.Setup(t)
	reader, _ := newReaderWithCache(t, queries)
	ctx := context.Background()

	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	token := shareToken(t, queries, deckID)

	if _, err := reader.Shared(ctx, token); err != nil {
		t.Fatalf("failed to warm the link: %v", err)
	}

	renameDeck(t, queries, deckID, "Renamed")
	reader.Invalidate(ctx, deckID)

	fresh, err := reader.Shared(ctx, token)
	if err != nil {
		t.Fatalf("failed to read the link after invalidation: %v", err)
	}
	if fresh.FromEntry {
		t.Fatal("expected the read after invalidation to come from the row")
	}
	if fresh.Deck.Name != "Renamed" {
		t.Fatalf("expected the renamed Deck through the link, got %q", fresh.Deck.Name)
	}
}

// InvalidateEveryDeck clears every share link without reading a row, so a link
// warmed before an erasure serves the rows again after it.
func TestInvalidateEveryDeckClearsTheShareLink(t *testing.T) {
	queries := dbtest.Setup(t)
	reader, _ := newReaderWithCache(t, queries)
	ctx := context.Background()

	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	token := shareToken(t, queries, deckID)

	if _, err := reader.Shared(ctx, token); err != nil {
		t.Fatalf("failed to warm the link: %v", err)
	}

	renameDeck(t, queries, deckID, "Renamed")
	reader.InvalidateEveryDeck(ctx)

	fresh, err := reader.Shared(ctx, token)
	if err != nil {
		t.Fatalf("failed to read the link after invalidation: %v", err)
	}
	if fresh.Deck.Name != "Renamed" {
		t.Fatalf("expected the renamed Deck through the link, got %q", fresh.Deck.Name)
	}
}

// Invalidate reads the Deck to find the token its link is keyed by. When that
// row cannot be read — a Deck deleted before the write could invalidate it — the
// fallback clears every link, so a warm entry cannot outlive the row it names.
func TestInvalidateClearsEveryShareLinkWhenTheDeckCannotBeRead(t *testing.T) {
	queries := dbtest.Setup(t)
	reader, _ := newReaderWithCache(t, queries)
	ctx := context.Background()

	owner := dbtest.SeedUser(t, "owner@example.com")
	deletedDeck := dbtest.SeedDeck(t, owner, false)
	survivingDeck := dbtest.SeedDeck(t, owner, false)
	survivingToken := shareToken(t, queries, survivingDeck)

	if _, err := reader.Shared(ctx, shareToken(t, queries, deletedDeck)); err != nil {
		t.Fatalf("failed to warm the deleted Deck's link: %v", err)
	}
	if _, err := reader.Shared(ctx, survivingToken); err != nil {
		t.Fatalf("failed to warm the surviving Deck's link: %v", err)
	}

	// The Deck is gone from every read, so Invalidate cannot name the token its
	// entry was keyed by.
	if err := queries.DeleteDeck(ctx, deletedDeck); err != nil {
		t.Fatalf("failed to delete the Deck: %v", err)
	}
	reader.Invalidate(ctx, deletedDeck)

	renameDeck(t, queries, survivingDeck, "Renamed")

	fresh, err := reader.Shared(ctx, survivingToken)
	if err != nil {
		t.Fatalf("failed to read the surviving Deck's link: %v", err)
	}
	if fresh.FromEntry {
		t.Fatal("expected the link's entry to have been cleared with the prefix")
	}
	if fresh.Deck.Name != "Renamed" {
		t.Fatalf("expected the renamed Deck through the link, got %q", fresh.Deck.Name)
	}
}

// A legacy listing entry is not a listing: Public answers from the readable-Deck
// rule, so a warm decks:all holding another caller's private Deck neither
// answers nor empties the listing.
func TestPublicIgnoresALegacyListingEntry(t *testing.T) {
	queries := dbtest.Setup(t)
	reader, cached := newReaderWithCache(t, queries)
	ctx := context.Background()

	owner := dbtest.SeedUser(t, "owner@example.com")
	publicDeck := dbtest.SeedDeck(t, owner, true)
	privateDeck := dbtest.SeedDeck(t, owner, false)

	_ = cached.SetProto(ctx, "decks:all", &types.DeckList{Decks: []*types.Deck{{
		Id:             uint32(privateDeck),
		Name:           "Warm Private Deck",
		Type:           "vocabulary",
		OwnerId:        uint32(owner),
		IsPublic:       false,
		UserAccessRole: access.RoleOwner,
	}}}, time.Minute)

	got, err := reader.Public(ctx)
	if err != nil {
		t.Fatalf("failed to read the public listing: %v", err)
	}
	if len(got) != 1 || int64(got[0].Id) != publicDeck {
		t.Fatalf("expected the public Deck and nothing else, got %v", got)
	}
}

// A legacy per-Deck entry is not a Deck read: the row decides the caller's
// standing, so a warm deck:<id> body carrying the owner's role does not make a
// private Deck readable by an anonymous caller.
func TestStandingOnIgnoresALegacyPerDeckEntry(t *testing.T) {
	queries := dbtest.Setup(t)
	reader, cached := newReaderWithCache(t, queries)
	ctx := context.Background()

	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)

	_ = cached.SetProto(ctx, fmt.Sprintf("deck:%d", deckID), &types.Deck{
		Id:             uint32(deckID),
		Name:           "Seeded Deck",
		Type:           "vocabulary",
		OwnerId:        uint32(owner),
		IsPublic:       false,
		ShareToken:     "sec_seed_1",
		UserAccessRole: access.RoleOwner,
	}, time.Minute)

	standing, err := reader.StandingOn(ctx, deckID, access.Anonymous)
	if err != nil {
		t.Fatalf("failed to read the Deck's standing: %v", err)
	}
	if standing.Role != access.RolePublic {
		t.Fatalf("expected the anonymous caller's own role %q, got %q", access.RolePublic, standing.Role)
	}
	if access.CanRead(standing.Deck, standing.Role) {
		t.Fatal("expected the private Deck to stay unreadable for an anonymous caller")
	}
}
