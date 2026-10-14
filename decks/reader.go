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
	var ownerID uint32
	if deck.OwnerID.Valid {
		ownerID = uint32(deck.OwnerID.Int64)
		if owner, err := r.db.GetUser(ctx, deck.OwnerID.Int64); err == nil {
			ownerName = owner.Name
		}
	}

	shared := &types.Deck{
		Id:         uint32(deck.ID),
		Name:       deck.Name,
		Type:       string(deck.Type),
		OwnerId:    ownerID,
		IsPublic:   deck.IsPublic,
		ShareToken: deck.ShareToken.String,
	}

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
