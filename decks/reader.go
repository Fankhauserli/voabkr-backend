// Package decks owns reading a Deck end to end: which cache entry answers it,
// the fetch on miss, the shape it takes on the wire, and which entries a write
// invalidates. Callers ask for a Deck and never see a database row or a key.
//
// Entries hold data every caller reads alike — a Deck's name and its Cards — and
// never the Access role of whoever warmed them, which is resolved per request
// (ADR-0001).
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

// NewReader returns a Reader over the database and cache it is given. Either may
// be nil, and then it answers as an instance holding neither.
func NewReader(q *db.Queries, c cache.Cache) *Reader {
	return &Reader{db: q, cache: c}
}

// The keys a Deck is held under, built here and nowhere else. Note that no
// prefix matches the share-link entry: a Deck and its link are keyed differently
// — by id, and by token — so a write clears the link through the Deck the token
// belongs to.
func deckKey(deckID int64) string   { return fmt.Sprintf("deck:%d", deckID) }
func sharedKey(token string) string { return fmt.Sprintf("deck:shared:%s", token) }

// publicListingKey holds the public Decks alone, which every sessionless caller
// reads alike. An entry that could hold a private Deck must not answer there:
// which Decks a caller may list is a decision about the caller, and an entry
// holds data, never decisions (ADR-0001).
const publicListingKey = "decks:public"

// listingKeyFor is where one learner's listing is held — the public Decks, their
// own, and the ones shared with them.
func listingKeyFor(caller int64) string { return fmt.Sprintf("decks:user:%d", caller) }

// cardsOfKey is the prefix a Deck's Card entries share, held under the Deck.
func cardsOfKey(deckID int64) string { return fmt.Sprintf("cards:deck:%d", deckID) }

// listingPrefix is what holds every listing a Deck appears in — a learner's own
// Decks, and the public ones — where its name and its Access role are read.
// There is no key naming one learner's listing, so a write clears them all.
const listingPrefix = "decks:"

// allCardsPrefix is what holds every Deck's Cards. Only erasing a learner takes
// the Cards of Decks nobody wrote through, so it is the one write that clears
// them wholesale.
const allCardsPrefix = "cards:"

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
	if r.db == nil {
		return Standing{}, ErrNoSuchDeck
	}

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

	if r.db == nil {
		return []*types.Deck{}, nil
	}

	rows, err := r.db.ListPublicDecks(ctx)
	if err != nil {
		return nil, err
	}

	decks := make([]*types.Deck, 0, len(rows))
	for _, row := range rows {
		decks = append(decks, bodyOf(row, access.RolePublic))
	}
	return r.hold(ctx, publicListingKey, decks), nil
}

// ForCaller returns the Decks caller reads — the public ones, their own, and the
// ones shared with them — each carrying the Access role they read it under. The
// list is the caller's own, so it is held under their key.
func (r *Reader) ForCaller(ctx context.Context, caller int64) ([]*types.Deck, error) {
	key := listingKeyFor(caller)
	if r.cache != nil {
		var cached types.DeckList
		if found, err := r.cache.GetProto(ctx, key, &cached); err == nil && found {
			return cached.Decks, nil
		}
	}

	if r.db == nil {
		return []*types.Deck{}, nil
	}

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
	return r.hold(ctx, key, decks), nil
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

// hold keeps a listing under the key it belongs to for as long as an entry lives.
func (r *Reader) hold(ctx context.Context, key string, decks []*types.Deck) []*types.Deck {
	if r.cache != nil {
		_ = r.cache.SetProto(ctx, key, &types.DeckList{Decks: decks}, entryTTL)
	}
	return decks
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

	if r.db == nil {
		return SharedDeck{}, ErrNoSuchDeck
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

// Invalidate drops every entry holding the Deck, or made from it: the Deck's own
// entry, its Cards, every listing it appears in, and the entry its share link
// answers under.
//
// It reads the Deck to find the token that link is keyed by, so a caller who is
// deleting the Deck calls it while the row is still there.
func (r *Reader) Invalidate(ctx context.Context, deckID int64) error {
	if r.cache == nil {
		return nil
	}

	_ = r.cache.DeletePrefix(ctx, listingPrefix)
	_ = r.cache.Delete(ctx, deckKey(deckID))
	_ = r.cache.DeletePrefix(ctx, cardsOfKey(deckID))

	if r.db == nil {
		return nil
	}

	deck, err := r.db.GetDeck(ctx, deckID)
	if err != nil {
		return nil
	}
	if deck.ShareToken.Valid && deck.ShareToken.String != "" {
		_ = r.cache.Delete(ctx, sharedKey(deck.ShareToken.String))
	}
	return nil
}

// InvalidateEveryDeck drops what any Deck is held under, for the one caller that
// takes Deck rows with it rather than writing through them: erasing a learner's
// account erases the Decks it owned, and the Cards of those with it.
func (r *Reader) InvalidateEveryDeck(ctx context.Context) {
	if r.cache == nil {
		return
	}
	_ = r.cache.DeletePrefix(ctx, listingPrefix)
	_ = r.cache.DeletePrefix(ctx, allCardsPrefix)
}
