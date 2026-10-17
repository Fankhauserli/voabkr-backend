package access_test

import (
	"context"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestMain provisions this package's Postgres with the migrations applied once,
// per ADR-0003. The harness records a missing container runtime and skips the
// database-backed tests locally; CI fails instead.
func TestMain(m *testing.M) {
	dbtest.Main(m)
}

// readableDeckCase is one Deck in the Access matrix, with the shares it carries.
// The Go predicates read these inputs independently of the SQL function.
type readableDeckCase struct {
	deck   db.Deck
	shares map[int64]string
}

func contractDeck(id, owner int64, isPublic, deleted bool) db.Deck {
	d := deck(owner, isPublic)
	d.ID = id
	d.Name = fmt.Sprintf("Deck %d", id)
	if deleted {
		d.DeletedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	}
	return d
}

func shareFor(c readableDeckCase, caller int64) *db.DeckShare {
	permission, ok := c.shares[caller]
	if !ok {
		return nil
	}
	return share(c.deck.ID, caller, permission)
}

// The SQL function and the Go predicates are two implementations of one rule:
// which Decks a caller may read, and under which Access role. They are compared
// across the full matrix — public, private, owner, view, edit, System and
// deleted Decks — so a drifted copy cannot pass unnoticed.
func TestReadableDecksAgreesWithTheAccessPredicates(t *testing.T) {
	q := dbtest.Setup(t)
	ctx := context.Background()

	owner := dbtest.SeedUser(t, "owner@example.com")
	viewer := dbtest.SeedUser(t, "viewer@example.com")
	editor := dbtest.SeedUser(t, "editor@example.com")
	outsider := dbtest.SeedUser(t, "outsider@example.com")

	cases := make([]readableDeckCase, 0, 8)

	add := func(deckID int64, ownerID int64, isPublic bool, shares map[int64]string) {
		cases = append(cases, readableDeckCase{
			deck:   contractDeck(deckID, ownerID, isPublic, false),
			shares: shares,
		})
	}

	// A public Deck owned by someone else: every caller reads it, and the owner
	// reads it as owner.
	publicOwned := dbtest.SeedDeck(t, owner, true)
	add(publicOwned, owner, true, nil)

	// A private Deck owned by someone else: only its owner and share holders
	// read it.
	privateOwned := dbtest.SeedDeck(t, owner, false)
	add(privateOwned, owner, false, nil)

	// A private Deck shared at each Permission: the share holder reads it under
	// that Permission.
	sharedPrivate := dbtest.SeedDeck(t, outsider, false)
	dbtest.SeedShare(t, sharedPrivate, viewer, access.RoleView)
	dbtest.SeedShare(t, sharedPrivate, editor, access.RoleEdit)
	add(sharedPrivate, outsider, false, map[int64]string{
		viewer: access.RoleView,
		editor: access.RoleEdit,
	})

	// A public Deck shared with a learner: the share's Permission is their role,
	// not the public one.
	sharedPublic := dbtest.SeedDeck(t, outsider, true)
	dbtest.SeedShare(t, sharedPublic, viewer, access.RoleView)
	add(sharedPublic, outsider, true, map[int64]string{viewer: access.RoleView})

	// System decks: public to everyone, and private to everyone but a share
	// holder, with no owner for anyone to be.
	systemPublic := dbtest.SeedDeck(t, access.Anonymous, true)
	add(systemPublic, access.Anonymous, true, nil)
	systemPrivate := dbtest.SeedDeck(t, access.Anonymous, false)
	dbtest.SeedShare(t, systemPrivate, viewer, access.RoleView)
	add(systemPrivate, access.Anonymous, false, map[int64]string{viewer: access.RoleView})

	// A deleted Deck is unreadable by everyone, whatever standing it carried. The
	// function carries the liveness rule, so the expected map skips these rather
	// than asking the Go predicates, which decide only access.
	deletedPublic := dbtest.SeedDeck(t, owner, true)
	deletedPrivate := dbtest.SeedDeck(t, owner, false)
	for _, deckID := range []int64{deletedPublic, deletedPrivate} {
		if err := q.DeleteDeck(ctx, deckID); err != nil {
			t.Fatalf("failed to delete Deck %d: %v", deckID, err)
		}
		cases = append(cases, readableDeckCase{
			deck: contractDeck(deckID, owner, deckID == deletedPublic, true),
		})
	}

	callers := []struct {
		name  string
		id    int64
		valid bool
	}{
		{name: "anonymous caller, no id", id: access.Anonymous},
		{name: "anonymous caller, zero id", id: access.Anonymous, valid: true},
		{name: "owner", id: owner, valid: true},
		{name: "view holder", id: viewer, valid: true},
		{name: "edit holder", id: editor, valid: true},
		{name: "outsider", id: outsider, valid: true},
	}

	for _, caller := range callers {
		t.Run(caller.name, func(t *testing.T) {
			// The function's rows are compared with a separate read of the decks
			// table, so a wrong projection is caught as well as a wrong role.
			liveRows, err := q.ListDecks(ctx)
			if err != nil {
				t.Fatalf("failed to read the live Decks: %v", err)
			}
			liveByID := make(map[int64]db.Deck, len(liveRows))
			for _, live := range liveRows {
				liveByID[live.ID] = live
			}

			rows := dbtest.ReadableDecks(t, pgtype.Int8{Int64: caller.id, Valid: caller.valid})

			want := make(map[int64]string, len(cases))
			for _, c := range cases {
				if c.deck.DeletedAt.Valid {
					continue
				}
				role := access.Role(c.deck, shareFor(c, caller.id), caller.id)
				if access.CanRead(c.deck, role) {
					want[c.deck.ID] = role
				}
			}

			got := make(map[int64]string, len(rows))
			for _, row := range rows {
				live, ok := liveByID[row.Deck.ID]
				if !ok {
					t.Fatalf("function returned Deck %d, which is not live", row.Deck.ID)
				}
				if row.Deck.Name != live.Name ||
					row.Deck.Type != live.Type ||
					row.Deck.OwnerID != live.OwnerID ||
					row.Deck.IsPublic != live.IsPublic ||
					row.Deck.ShareToken != live.ShareToken ||
					row.Deck.DeletedAt.Valid {
					t.Fatalf("function returned Deck %d with the wrong columns:\n got %+v\nwant %+v",
						row.Deck.ID, row.Deck, live)
				}
				got[row.Deck.ID] = row.AccessRole
			}

			if !maps.Equal(got, want) {
				t.Fatalf("readable Decks disagree with the Go predicates:\n got %v\nwant %v", got, want)
			}
		})
	}
}
