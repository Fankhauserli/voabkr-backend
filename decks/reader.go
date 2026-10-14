// Package decks owns reading a Deck end to end: which cache entry answers it,
// the fetch on miss, the shape it takes on the wire, the Access role the caller
// reads it under, and which entries a write invalidates. Callers ask for a Deck
// rather than assembling one, and hold no key.
//
// An entry holds data every caller reads alike — a Deck's name, the public
// listing — and never anything derived from who is asking: an Access role is
// resolved per request from the Deck and the caller's own DeckShare, and a
// listing of one learner's own standing is no entry at all (ADR-0001).
package decks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/cache"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/jackc/pgx/v5/pgtype"
)

// entryTTL is how long an entry holding a Deck lives. A write clears what it
// holds, so this bounds only what nothing writes through.
const entryTTL = 5 * time.Minute

// ErrNoSuchDeck is returned when no Deck answers — for a missing row, and for a
// reader with no database to read it from.
var ErrNoSuchDeck = errors.New("no such Deck")

// Reader answers every read of a Deck, and clears what a write invalidates.
type Reader struct {
	db    *db.Queries
	cache cache.Cache
}

// NewReader returns a Reader over the database a Deck is read from and the cache
// it is held in. A nil cache means an instance that holds nothing; a Deck is
// read from the database either way.
func NewReader(q *db.Queries, c cache.Cache) *Reader {
	return &Reader{db: q, cache: c}
}

// The Deck keys are built here and nowhere else. Note that no prefix matches the
// share-link entry: a Deck and its link are keyed differently — by token — so a
// write clears the link through the Deck the token belongs to.
func sharedKey(token string) string { return fmt.Sprintf("deck:shared:%s", token) }

// sharedPrefix is what holds every share-link entry. Links are cleared one at a
// time wherever the Deck names its token; the prefix is for the writes that
// cannot — an erasure, or a row that cannot be read.
const sharedPrefix = "deck:shared:"

// publicListingKey holds the public Decks alone, which every sessionless caller
// reads alike. An entry that could hold a private Deck must not answer there:
// which Decks a caller may list is a decision about the caller, and an entry
// holds data, never decisions (ADR-0001).
const publicListingKey = "decks:public"

// listingPrefix is what holds a listing a Deck appears in — the public one, where
// its name is read by every sessionless caller.
const listingPrefix = "decks:"

// SharedDeck is a Deck as its share link serves it. FromEntry tells the shape
// the link answered in: an entry holds the Deck alone, while reading the row
// also names the learner who owns it — a difference the wire contract keeps for
// now.
type SharedDeck struct {
	Deck      *types.Deck
	OwnerName string
	FromEntry bool
}

// Standing is a Deck with the Access role a caller holds on it. What a caller
// may do with it is access/'s question, asked with the row; what the wire
// carries is Body.
type Standing struct {
	// Deck is the row the standing was read from.
	Deck db.Deck
	Role string
}

// Body is the Deck as the wire carries it, under the role the caller reads it —
// resolved per request, and never held in an entry (ADR-0001).
func (s Standing) Body() *types.Deck { return bodyOf(s.Deck, s.Role) }

// StandingOn reads deckID and resolves caller's Access role on it, from the Deck
// and the caller's own DeckShare, judging nothing by the answer. A Deck that is
// not there is ErrNoSuchDeck.
func (r *Reader) StandingOn(ctx context.Context, deckID, caller int64) (Standing, error) {
	deck, err := r.db.GetDeck(ctx, deckID)
	if err != nil {
		return Standing{}, fmt.Errorf("%w: %v", ErrNoSuchDeck, err)
	}

	var share *db.DeckShare
	if caller != access.Anonymous && !access.Owns(deck, caller) {
		if s, err := r.db.GetDeckShare(ctx, db.GetDeckShareParams{
			DeckID: deck.ID,
			UserID: pgtype.Int8{Int64: caller, Valid: true},
		}); err == nil {
			share = &s
		}
	}

	return Standing{Deck: deck, Role: access.Role(deck, share, caller)}, nil
}

// Public returns every public Deck, as a sessionless caller reads them: the same
// list for everyone, so an entry answers it.
func (r *Reader) Public(ctx context.Context) ([]*types.Deck, error) {
	if r.cache != nil {
		var cached types.DeckList
		if found, err := r.cache.GetProto(ctx, publicListingKey, &cached); err == nil && found {
			return cached.Decks, nil
		}
	}

	rows, err := r.db.ListPublicDecks(ctx)
	if err != nil {
		return nil, err
	}

	decks := make([]*types.Deck, 0, len(rows))
	for _, row := range rows {
		decks = append(decks, bodyOf(row, access.RolePublic))
	}

	if r.cache != nil {
		_ = r.cache.SetProto(ctx, publicListingKey, &types.DeckList{Decks: decks}, entryTTL)
	}
	return decks, nil
}

// ForCaller returns the Decks caller reads — the public ones, their own, and the
// ones shared with them — each carrying the Access role they read it under.
//
// Nothing answers this from an entry: the list is a decision about the caller,
// whose Decks and roles change with a DeckShare given or revoked, and a write
// cannot know whose listing to clear (ADR-0001).
func (r *Reader) ForCaller(ctx context.Context, caller int64) ([]*types.Deck, error) {
	rows, err := r.db.ListDecksForUser(ctx, pgtype.Int8{Int64: caller, Valid: true})
	if err != nil {
		return nil, err
	}

	// The query resolves each Deck's Access role for this caller, so the rows
	// carry it; ListPublicDecks rows are read by a guest, which is the public role.
	decks := make([]*types.Deck, 0, len(rows))
	for _, row := range rows {
		decks = append(decks, bodyOf(listedDeck(row), row.UserAccessRole))
	}
	return decks, nil
}

// listedDeck is a listing row as the Deck it carries: the query reads the Deck's
// own columns with the role resolved for this caller beside them.
func listedDeck(row db.ListDecksForUserRow) db.Deck {
	return db.Deck{
		ID:         row.ID,
		Name:       row.Name,
		Type:       row.Type,
		OwnerID:    row.OwnerID,
		IsPublic:   row.IsPublic,
		ShareToken: row.ShareToken,
	}
}

// bodyOf is the one row→wire mapping: a Deck as callers see it, carrying the
// Access role they read it under, which is resolved per request and never held
// in an entry (ADR-0001).
func bodyOf(deck db.Deck, role string) *types.Deck {
	var ownerID uint32
	if deck.OwnerID.Valid {
		ownerID = uint32(deck.OwnerID.Int64)
	}

	return &types.Deck{
		Id:             uint32(deck.ID),
		Name:           deck.Name,
		Type:           string(deck.Type),
		OwnerId:        ownerID,
		IsPublic:       deck.IsPublic,
		ShareToken:     deck.ShareToken.String,
		UserAccessRole: role,
	}
}

// Shared answers the share link a token names. A Deck no link answers for is
// ErrNoSuchDeck.
func (r *Reader) Shared(ctx context.Context, token string) (SharedDeck, error) {
	if r.cache != nil {
		var cached types.Deck
		if found, err := r.cache.GetProto(ctx, sharedKey(token), &cached); err == nil && found {
			return SharedDeck{Deck: &cached, FromEntry: true}, nil
		}
	}

	deck, err := r.db.GetDeckByShareToken(ctx, pgtype.Text{String: token, Valid: true})
	if err != nil {
		return SharedDeck{}, fmt.Errorf("%w: %v", ErrNoSuchDeck, err)
	}

	var ownerName string
	if deck.OwnerID.Valid {
		if owner, err := r.db.GetUser(ctx, deck.OwnerID.Int64); err == nil {
			ownerName = owner.Name
		}
	}

	// A link is read by whoever holds it, never by a caller whose standing it
	// would carry, so the body names no Access role.
	shared := bodyOf(deck, "")
	if r.cache != nil {
		_ = r.cache.SetProto(ctx, sharedKey(token), shared, entryTTL)
	}

	return SharedDeck{Deck: shared, OwnerName: ownerName}, nil
}

// Invalidate drops every entry holding the Deck: the public listing its name
// appears in, and the entry its share link answers under. What holds its Cards
// is the Card reader's to clear, and deleting the Deck asks it to.
//
// It reads the Deck to find the token that link is keyed by, so a caller who is
// deleting the Deck calls it while the row is still there.
func (r *Reader) Invalidate(ctx context.Context, deckID int64) {
	if r.cache == nil {
		return
	}

	_ = r.cache.DeletePrefix(ctx, listingPrefix)

	deck, err := r.db.GetDeck(ctx, deckID)
	if err != nil {
		// The token the link is keyed by cannot be read, and a link left answering
		// from its entry is the thing this exists to prevent, so every link entry
		// goes rather than the one.
		_ = r.cache.DeletePrefix(ctx, sharedPrefix)
		return
	}
	if deck.ShareToken.Valid && deck.ShareToken.String != "" {
		_ = r.cache.Delete(ctx, sharedKey(deck.ShareToken.String))
	}
}

// InvalidateEveryDeck drops what any Deck is held under. One caller needs this
// rather than writing through the Deck: erasing a learner's account hands their
// Decks to no one (the foreign key sets the owner to NULL), so the Decks stay
// and become System decks — and the body every listing served of them, naming
// the erased learner as owner, goes with the erased standing.
//
// Every link entry goes too, not only those of the erased learner's Decks: which
// Decks were theirs is no longer readable from the row that would say.
func (r *Reader) InvalidateEveryDeck(ctx context.Context) {
	if r.cache == nil {
		return
	}
	_ = r.cache.DeletePrefix(ctx, listingPrefix)
	_ = r.cache.DeletePrefix(ctx, sharedPrefix)
}
